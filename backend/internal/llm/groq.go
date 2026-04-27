package llm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	groqBaseURL    = "https://api.groq.com/openai/v1"
	sentenceBufCap = 4 // max sentences queued to TTS before backpressure

	// sentenceBoundary characters that trigger a TTS flush.
	sentenceBoundary = ".?!,"
)

type streamMode int

const (
	modeText            streamMode = iota
	modeToolAccumulate             // accumulating tool_call JSON fragments
)

// ToolSimulator is called when the LLM requests a tool. Returns the tool result
// as a string.
type ToolSimulator interface {
	Execute(ctx context.Context, name, arguments string) (string, error)
}

// GroqClient streams LLM completions from Groq and routes text to TTS while
// intercepting tool calls for local simulation.
type GroqClient struct {
	client    *openai.Client
	model     string
	tools     []openai.Tool
	simulator ToolSimulator
	log       *slog.Logger
}

func NewGroqClient(apiKey, model string, simulator ToolSimulator, tools []openai.Tool, log *slog.Logger) *GroqClient {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = groqBaseURL
	return &GroqClient{
		client:    openai.NewClientWithConfig(cfg),
		model:     model,
		tools:     tools,
		simulator: simulator,
		log:       log,
	}
}

// StreamLoop runs one (or more, when tool calls occur) LLM turns.
// sentenceCh receives flushed sentence strings for the TTS pipeline.
// The function blocks until the turn is complete or ctx is cancelled.
func (g *GroqClient) StreamLoop(ctx context.Context, history *History, sentenceCh chan<- string) error {
	return g.streamLoop(ctx, history, sentenceCh, 0)
}

const maxToolRounds = 5

func (g *GroqClient) streamLoop(ctx context.Context, history *History, sentenceCh chan<- string, depth int) error {
	if depth >= maxToolRounds {
		return errors.New("llm: max tool call rounds exceeded")
	}

	req := openai.ChatCompletionRequest{
		Model:    g.model,
		Messages: history.Snapshot(),
		Stream:   true,
	}
	if len(g.tools) > 0 {
		req.Tools = g.tools
	}

	stream, err := g.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	var (
		mode        = modeText
		textBuf     strings.Builder // accumulates text until sentence boundary
		fullTextBuf strings.Builder // accumulates complete assistant response for history
		toolName    string
		toolID      string
		toolArgsBuf strings.Builder // accumulates tool_call JSON arguments
	)

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}

		if len(resp.Choices) == 0 {
			continue
		}
		delta := resp.Choices[0].Delta

		// Check for tool_call activation.
		if len(delta.ToolCalls) > 0 {
			tc := delta.ToolCalls[0]
			if mode == modeText {
				// Transition: flush any pending text, then switch modes.
				if textBuf.Len() > 0 {
					if err := g.flushText(ctx, textBuf.String(), sentenceCh); err != nil {
						return err
					}
					textBuf.Reset()
				}
				mode = modeToolAccumulate
				toolID = tc.ID
				if tc.Function.Name != "" {
					toolName = tc.Function.Name
				}
			}
			// Always accumulate arguments (they arrive incrementally).
			toolArgsBuf.WriteString(tc.Function.Arguments)
			continue
		}

		if mode == modeToolAccumulate {
			// Keep accumulating until EOF.
			continue
		}

		// modeText: forward content tokens to sentence buffer.
		if delta.Content != "" {
			textBuf.WriteString(delta.Content)
			fullTextBuf.WriteString(delta.Content)
			// Flush on sentence boundary.
			text := textBuf.String()
			if idx := lastBoundary(text); idx >= 0 {
				sentence := text[:idx+1]
				rest := text[idx+1:]
				textBuf.Reset()
				textBuf.WriteString(rest)
				if err := g.flushText(ctx, sentence, sentenceCh); err != nil {
					return err
				}
			}
		}
	}

	// Stream ended — flush remaining text buffer.
	if mode == modeText && textBuf.Len() > 0 {
		if err := g.flushText(ctx, textBuf.String(), sentenceCh); err != nil {
			return err
		}
	}

	// Record the complete assistant text response in history for multi-turn context.
	if mode == modeText {
		if full := strings.TrimSpace(fullTextBuf.String()); full != "" {
			history.Append(openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: full,
			})
		}
	}

	// Handle accumulated tool call.
	if mode == modeToolAccumulate {
		args := toolArgsBuf.String()
		g.log.Info("llm: tool call", "tool", toolName, "id", toolID, "args", args)

		// Record the assistant's tool_use message.
		history.Append(openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleAssistant,
			ToolCalls: []openai.ToolCall{
				{
					ID:   toolID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      toolName,
						Arguments: args,
					},
				},
			},
		})

		// Execute the tool.
		result, err := g.simulator.Execute(ctx, toolName, args)
		if err != nil {
			return err
		}

		// Inject tool result and run another LLM turn.
		history.Append(openai.ChatCompletionMessage{
			Role:       openai.ChatMessageRoleTool,
			ToolCallID: toolID,
			Content:    result,
		})

		return g.streamLoop(ctx, history, sentenceCh, depth+1)
	}

	return nil
}

// flushText sends a sentence to the TTS pipeline.
// Uses select on ctx.Done() to unblock if the turn is cancelled (barge-in).
func (g *GroqClient) flushText(ctx context.Context, text string, sentenceCh chan<- string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	g.log.Debug("llm: sentence flush", "text", text)
	select {
	case sentenceCh <- text:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// lastBoundary returns the index of the last sentence-boundary character in s,
// or -1 if none is found.
func lastBoundary(s string) int {
	last := -1
	for i, ch := range s {
		if strings.ContainsRune(sentenceBoundary, ch) {
			// Only treat comma as a boundary if followed by a space or end.
			if ch == ',' {
				if i+1 < len(s) && s[i+1] == ' ' {
					last = i
				}
			} else {
				last = i
			}
		}
	}
	return last
}

// DefaultTools returns the tool definitions exposed to the LLM.
func DefaultTools() []openai.Tool {
	return []openai.Tool{
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "check_availability",
				Description: "Returns the next available appointment slots from the calendar.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"date": map[string]any{
							"type":        "string",
							"description": "ISO 8601 date, e.g. 2026-04-23",
						},
					},
					"required": []string{"date"},
				},
			},
		},
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "fetch_prospect",
				Description: "Fetches a prospect's information from the CRM using their email address.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"email": map[string]any{
							"type":        "string",
							"description": "The prospect's email address",
						},
					},
					"required": []string{"email"},
				},
			},
		},
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "book_meeting",
				Description: "Books a meeting in the calendar.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{},
				},
			},
		},
	}
}

// FillerDelay is the minimum time to wait before sending filler audio.
// The LLM TTFT is ~120ms; the filler should start at t=0 and be cancelled
// by tool result audio if the LLM responds in time.
const FillerDelay = 50 * time.Millisecond
