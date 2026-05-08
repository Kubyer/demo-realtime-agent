package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	groqBaseURL    = "https://api.groq.com/openai/v1"
	sentenceBufCap = 4 // max sentences queued to TTS before backpressure

	// sentenceBoundary characters that trigger a TTS flush.
	sentenceBoundary = ".?!,;:"
)

type streamMode int

const (
	modeText           streamMode = iota
	modeToolAccumulate            // accumulating tool_call JSON fragments
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

func NewGroqClient(apiKey, model string, simulator ToolSimulator, tools []openai.Tool, log *slog.Logger, baseURL string) *GroqClient {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	} else {
		cfg.BaseURL = groqBaseURL
	}
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

// flushText sanitizes and sends a sentence to the TTS pipeline.
// Uses select on ctx.Done() to unblock if the turn is cancelled (barge-in).
func (g *GroqClient) flushText(ctx context.Context, text string, sentenceCh chan<- string) error {
	text = sanitizeTTSChunk(text)
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

// frHours maps 0–23 to French spoken words (heure is feminine: "une").
var frHours = [24]string{
	"zéro", "une", "deux", "trois", "quatre", "cinq", "six", "sept",
	"huit", "neuf", "dix", "onze", "douze", "treize", "quatorze", "quinze",
	"seize", "dix-sept", "dix-huit", "dix-neuf", "vingt", "vingt et une",
	"vingt-deux", "vingt-trois",
}

// frMinutes maps exact minute values to spoken French suffixes.
var frMinutes = map[int]string{
	0: "", 15: "et quart", 30: "trente", 45: "quarante-cinq",
}

var (
	// Matches "14h30", "14:30", "14 h 30" — hour + separator + two-digit minutes.
	reTimeHhMm = regexp.MustCompile(`\b(\d{1,2})\s*[h:]\s*(\d{2})\b`)
	// Matches bare "14h" or "14 h" — only when no digit follows the h.
	reTimeHhOnly = regexp.MustCompile(`\b(\d{1,2})\s*h\b`)
)

// normalizeFrenchTimes converts digit-based time patterns to spoken French before
// the text reaches TTS, so ElevenLabs never sees raw digit times.
// "09:00" → "neuf heures", "14h30" → "quatorze heures trente", "09 h" → "neuf heures".
func normalizeFrenchTimes(s string) string {
	// Pass 1: patterns with explicit two-digit minutes (14h30, 14:30, 14 h 30).
	s = reTimeHhMm.ReplaceAllStringFunc(s, func(m string) string {
		parts := reTimeHhMm.FindStringSubmatch(m)
		if len(parts) < 3 {
			return m
		}
		h, err1 := strconv.Atoi(parts[1])
		min, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || h > 23 {
			return m
		}
		suffix := " heures"
		if h == 1 {
			suffix = " heure"
		}
		base := frHours[h] + suffix
		if label, ok := frMinutes[min]; ok {
			if label != "" {
				base += " " + label
			}
		} else {
			base += fmt.Sprintf(" %d", min)
		}
		return base
	})
	// Pass 2: bare hour (09h, 09 h) — only runs after pass 1 removed all HHhMM forms.
	s = reTimeHhOnly.ReplaceAllStringFunc(s, func(m string) string {
		parts := reTimeHhOnly.FindStringSubmatch(m)
		if len(parts) < 2 {
			return m
		}
		h, err := strconv.Atoi(parts[1])
		if err != nil || h > 23 {
			return m
		}
		if h == 1 {
			return frHours[h] + " heure"
		}
		return frHours[h] + " heures"
	})
	return s
}

// punctSanitizer fixes tokenizer spacing artifacts before sending text to TTS.
// e.g. "Hello , world ." → "Hello, world."
var punctSanitizer = strings.NewReplacer(
	" .", ".",
	" ,", ",",
	" !", "!",
	" ?", "?",
	" ;", ";",
	" :", ":",
)

// sanitizeTTSChunk cleans a flushed text chunk before it reaches the TTS:
//  1. Removes spaces before punctuation (tokenizer artifact).
//  2. Ensures a space follows punctuation when missing ("word,word" → "word, word").
//  3. Collapses multiple consecutive spaces to one.
//  4. Trims leading/trailing whitespace.
func sanitizeTTSChunk(text string) string {
	text = normalizeFrenchTimes(text)
	text = punctSanitizer.Replace(text)
	text = ensureSpaceAfterPunct(text)
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	return strings.TrimSpace(text)
}

// ensureSpaceAfterPunct inserts a space after ,;: when immediately followed by
// a non-space character. This prevents TTS from running words together when the
// LLM omits the space (e.g. "neuf heures,dix heures" → "neuf heures, dix heures").
func ensureSpaceAfterPunct(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i, ch := range runes {
		b.WriteRune(ch)
		if (ch == ',' || ch == ';' || ch == ':') && i+1 < len(runes) && runes[i+1] != ' ' {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

// lastBoundary returns the index of the last sentence-boundary character in s,
// or -1 if none is found.
// Comma, semicolon, and colon require a trailing space to avoid false positives
// in numeric literals (e.g. "14:30", "3,14").
func lastBoundary(s string) int {
	last := -1
	for i, ch := range s {
		if strings.ContainsRune(sentenceBoundary, ch) {
			switch ch {
			case ',', ';', ':':
				if i+1 < len(s) && s[i+1] == ' ' {
					last = i
				}
			default:
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
				Description: "Returns available appointment slots for one or more dates. Pass all dates in a single call — never call this function multiple times for dates from the same user request.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"dates": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "ISO 8601 dates to check, e.g. [\"2026-05-12\",\"2026-05-13\"]. Always batch multiple dates into one call.",
							"minItems":    1,
						},
					},
					"required": []string{"dates"},
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
				Description: "Books a meeting slot in the calendar. Call this only after the user has confirmed both the datetime AND their name and email.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"datetime": map[string]any{
							"type":        "string",
							"description": "ISO 8601 datetime of the slot to book, e.g. 2026-05-08T14:30:00. Must be one of the slots returned by check_availability.",
						},
						"name": map[string]any{
							"type":        "string",
							"description": "Full name of the person to book the meeting for.",
						},
						"email": map[string]any{
							"type":        "string",
							"description": "Email address of the person to book the meeting for.",
						},
					},
					"required": []string{"datetime", "name", "email"},
				},
			},
		},
	}
}

// FillerDelay is the minimum time to wait before sending filler audio.
// The LLM TTFT is ~120ms; the filler should start at t=0 and be cancelled
// by tool result audio if the LLM responds in time.
const FillerDelay = 50 * time.Millisecond
