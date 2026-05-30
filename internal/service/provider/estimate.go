package provider

import (
	"strings"
	"time"
)

// RoughTokenCount estimates token count from string length.
func RoughTokenCount(content string) int {
	if content == "" {
		return 0
	}
	return len(content) / 4
}

// DefaultMaxOutputTokens is the conservative fallback when no max_tokens is specified.
const DefaultMaxOutputTokens = 4096

func (r *ResponseRequest) InputMessages() []Message {
	if len(r.Messages) > 0 {
		return cloneMessages(r.Messages)
	}
	return normalizeMessages(r.Input)
}

func (r *ResponseRequest) RequestedMaxTokens() int {
	if r.MaxOutputTokens > 0 {
		return r.MaxOutputTokens
	}
	return r.MaxTokens
}

func (r *ResponseRequest) Normalize() {
	if len(r.Messages) == 0 {
		r.Messages = r.InputMessages()
	}
	if r.Input == nil && len(r.Messages) > 0 {
		r.Input = cloneMessages(r.Messages)
	}
}

// EstimatePromptTokens estimates the prompt token count, used for admission control and cache key derivation.
func (r *ResponseRequest) EstimatePromptTokens() int {
	total := 0
	for _, message := range r.InputMessages() {
		total += RoughTokenCount(message.Signature())
	}
	if total == 0 {
		return 1
	}
	return total
}

func (r *ResponseRequest) InputText() string {
	if r == nil {
		return ""
	}

	var parts []string
	for _, message := range r.InputMessages() {
		if text := collectText(message.Content); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func (r *ResponseRequest) HasToolsRequested() bool {
	return r != nil && len(r.Tools) > 0
}

func (r *ResponseRequest) HasImageInput() bool {
	if r == nil {
		return false
	}
	for _, message := range r.InputMessages() {
		for _, block := range message.Content {
			if block.Type == "image" {
				return true
			}
		}
	}
	return false
}

func (r *ResponseRequest) HasStructuredOutputRequest() bool {
	return r != nil && r.OutputFormat != nil
}

// EstimateAdmissionTokens estimates the total tokens for admission control.
// It includes both prompt estimation and output budget.
func (r *ResponseRequest) EstimateAdmissionTokens() int {
	promptTokens := r.EstimatePromptTokens()

	maxTokens := r.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = r.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = DefaultMaxOutputTokens
	}

	return promptTokens + maxTokens
}

func (r *Response) OutputText() string {
	if r == nil {
		return ""
	}

	var b strings.Builder
	for _, item := range r.Output {
		for _, content := range item.Content {
			if content.Text != "" {
				b.WriteString(content.Text)
			} else if content.Refusal != "" {
				b.WriteString(content.Refusal)
			}
		}
	}
	return b.String()
}

func (r *Response) Signature() string {
	if r == nil {
		return ""
	}

	var b strings.Builder
	for _, item := range r.Output {
		if item.Type == "function_call" {
			b.WriteString(item.Name)
			b.WriteString(item.Args)
			continue
		}
		for _, content := range item.Content {
			if content.Text != "" {
				b.WriteString(content.Text)
			} else if content.Refusal != "" {
				b.WriteString(content.Refusal)
			}
		}
	}
	return b.String()
}

func (r *Response) OutputToolCalls() []ToolCall {
	if r == nil {
		return nil
	}

	var calls []ToolCall
	for _, item := range r.Output {
		if item.Type != "function_call" {
			continue
		}
		calls = append(calls, ToolCall{
			ID:   item.ID,
			Type: "function",
			Function: FunctionCall{
				Name:      item.Name,
				Arguments: item.Args,
			},
		})
	}
	return calls
}

func NewTextResponse(id, model, text string, usage Usage) *Response {
	return &Response{
		ID:      id,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output: []ResponseOutput{{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []ResponseContent{{
				Type: "output_text",
				Text: text,
			}},
		}},
		Usage: usage,
	}
}

func NewToolCallResponse(id, model, text string, calls []ToolCall, usage Usage) *Response {
	output := []ResponseOutput{{
		Type:   "message",
		Role:   "assistant",
		Status: "completed",
		Content: []ResponseContent{{
			Type: "output_text",
			Text: text,
		}},
	}}
	for _, call := range calls {
		output = append(output, ResponseOutput{
			Type:   "function_call",
			CallID: call.ID,
			Name:   call.Function.Name,
			Args:   call.Function.Arguments,
		})
	}
	return &Response{
		ID:      id,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  output,
		Usage:   usage,
	}
}
