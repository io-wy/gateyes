package apicompat

import (
	"encoding/json"
	"strings"

	"github.com/gateyes/gateway/internal/service/provider"
)

type ChatCompletionRequest struct {
	Model          string                 `json:"model"`
	Messages       []provider.ChatMessage `json:"messages"`
	Stream         bool                   `json:"stream,omitempty"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	Tools          []any                  `json:"tools,omitempty"`
	ResponseFormat any                    `json:"response_format,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object,omitempty"`
	Created int64                  `json:"created,omitempty"`
	Model   string                 `json:"model,omitempty"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   provider.Usage         `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int                  `json:"index,omitempty"`
	Message      provider.ChatMessage `json:"message"`
	FinishReason string               `json:"finish_reason,omitempty"`
}

type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Usage   *provider.Usage             `json:"usage,omitempty"`
}

type ChatCompletionChunkChoice struct {
	Index        int                      `json:"index"`
	Delta        ChatCompletionChunkDelta `json:"delta"`
	FinishReason string                   `json:"finish_reason,omitempty"`
}

type ChatCompletionChunkDelta struct {
	Role      string                        `json:"role,omitempty"`
	Content   string                        `json:"content,omitempty"`
	ToolCalls []ChatCompletionChunkToolCall `json:"tool_calls,omitempty"`
}

type ChatCompletionChunkToolCall struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id,omitempty"`
	Type     string                `json:"type,omitempty"`
	Function provider.FunctionCall `json:"function,omitempty"`
}

type AnthropicMessagesRequest struct {
	Model         string                 `json:"model"`
	Messages      []AnthropicMessage     `json:"messages"`
	System        any                    `json:"system,omitempty"`
	MaxTokens     int                    `json:"max_tokens,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
	Tools         []AnthropicTool        `json:"tools,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Temperature   float64                `json:"temperature,omitempty"`
	TopK          int                    `json:"top_k,omitempty"`
	TopP          float64                `json:"top_p,omitempty"`
	Thinking      *AnthropicThinking     `json:"thinking,omitempty"`
	CacheControl  *AnthropicCacheControl `json:"cache_control,omitempty"`
}

type AnthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type AnthropicCacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

func (m *AnthropicMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	if len(raw.Content) == 0 {
		return nil
	}
	if raw.Content[0] == '"' {
		var text string
		if err := json.Unmarshal(raw.Content, &text); err != nil {
			return err
		}
		m.Content = []AnthropicContentBlock{{Type: "text", Text: text}}
		return nil
	}
	return json.Unmarshal(raw.Content, &m.Content)
}

type AnthropicContentBlock struct {
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Input     json.RawMessage  `json:"input,omitempty"`
	ToolUseID string           `json:"tool_use_id,omitempty"`
	Content   string           `json:"content,omitempty"`
	Source    *AnthropicSource `json:"source,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	Signature string           `json:"signature,omitempty"`
}

type AnthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type AnthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type AnthropicMessagesResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicEvent struct {
	Type    string                     `json:"type"`
	Index   int                        `json:"index,omitempty"`
	Delta   any                        `json:"delta,omitempty"`
	Content []AnthropicContentBlock    `json:"content,omitempty"`
	Block   *AnthropicContentBlock     `json:"content_block,omitempty"`
	Message *AnthropicMessagesResponse `json:"message,omitempty"`
}

func cloneMessages(messages []provider.ChatMessage) []provider.ChatMessage {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]provider.ChatMessage, len(messages))
	for i, message := range messages {
		cloned[i] = message
		cloned[i].Content = cloneAny(message.Content)
		if len(message.ToolCalls) > 0 {
			cloned[i].ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
		}
	}
	return cloned
}

func cloneAny(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return value
	}
	return cloned
}

func collectText(value any) string {
	switch current := value.(type) {
	case nil:
		return ""
	case string:
		return current
	case []any:
		parts := make([]string, 0, len(current))
		for _, item := range current {
			if text := collectText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := current["text"].(string); ok && text != "" {
			return text
		}
		if content, ok := current["content"]; ok {
			return collectText(content)
		}
		if content, ok := current["input_text"]; ok {
			return collectText(content)
		}
		return ""
	default:
		raw, err := json.Marshal(current)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
