package llm

import (
	"sync"

	openai "github.com/sashabaranov/go-openai"
)

// History manages the conversation message list for a single session.
// All methods are safe for concurrent use.
type History struct {
	mu   sync.RWMutex
	msgs []openai.ChatCompletionMessage
}

func NewHistory(systemPrompt string) *History {
	h := &History{}
	if systemPrompt != "" {
		h.msgs = []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		}
	}
	return h
}

func (h *History) Append(msg openai.ChatCompletionMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, msg)
}

// Snapshot returns a shallow copy of the message slice for use in an LLM call.
func (h *History) Snapshot() []openai.ChatCompletionMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	cp := make([]openai.ChatCompletionMessage, len(h.msgs))
	copy(cp, h.msgs)
	return cp
}
