package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role,omitempty"`
	Content    any        `json:"content,omitempty"`
	Type       string     `json:"type,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ResponseRequest struct {
	Model           string    `json:"model"`
	Input           any       `json:"input,omitempty"`
	Messages        []Message `json:"messages,omitempty"`
	Stream          bool      `json:"stream,omitempty"`
	MaxOutputTokens int       `json:"max_output_tokens,omitempty"`
	MaxTokens       int       `json:"max_tokens,omitempty"`
}

type Response struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Status  string           `json:"status,omitempty"`
	Output  []ResponseOutput `json:"output"`
	Usage   Usage            `json:"usage"`
}

type ResponseOutput struct {
	ID      string            `json:"id,omitempty"`
	Type    string            `json:"type"`
	Role    string            `json:"role,omitempty"`
	Status  string            `json:"status,omitempty"`
	Content []ResponseContent `json:"content,omitempty"`
	CallID  string            `json:"call_id,omitempty"`
	Name    string            `json:"name,omitempty"`
	Args    string            `json:"arguments,omitempty"`
}

type ResponseContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ResponseEvent struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta,omitempty"`
	Response *Response       `json:"response,omitempty"`
	Output   *ResponseOutput `json:"output,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatCompletionRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object,omitempty"`
	Created int64                  `json:"created,omitempty"`
	Model   string                 `json:"model,omitempty"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int     `json:"index,omitempty"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Usage   *Usage                      `json:"usage,omitempty"`
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
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function,omitempty"`
}

type Provider interface {
	Name() string
	Type() string
	BaseURL() string
	Model() string
	UnitCost() float64
	Cost(promptTokens, completionTokens int) float64
	CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error)
	StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error)
}

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

func (r *ResponseRequest) CacheKey() string {
	var b strings.Builder
	b.WriteString(r.Model)
	b.WriteString("\n")
	for _, message := range r.InputMessages() {
		b.WriteString(message.Role)
		b.WriteString(":")
		b.WriteString(message.Signature())
		b.WriteString("\n")
	}
	return b.String()
}

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

func (r *Response) OutputText() string {
	if r == nil {
		return ""
	}

	var b strings.Builder
	for _, item := range r.Output {
		for _, content := range item.Content {
			if content.Text != "" {
				b.WriteString(content.Text)
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

func ConvertChatRequest(req *ChatCompletionRequest) *ResponseRequest {
	if req == nil {
		return nil
	}

	messages := cloneMessages(req.Messages)
	return &ResponseRequest{
		Model:     req.Model,
		Input:     messages,
		Messages:  messages,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
	}
}

func ConvertResponseToChat(resp *Response) *ChatCompletionResponse {
	if resp == nil {
		return nil
	}

	return &ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.Created,
		Model:   resp.Model,
		Choices: []ChatCompletionChoice{{
			Index: 0,
			Message: Message{
				Role:      "assistant",
				Content:   resp.OutputText(),
				ToolCalls: resp.OutputToolCalls(),
			},
			FinishReason: responseFinishReason(resp),
		}},
		Usage: resp.Usage,
	}
}

func ConvertEventToChatChunk(responseID, model string, event ResponseEvent) *ChatCompletionChunk {
	chunk := &ChatCompletionChunk{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChunkChoice{{
			Index: 0,
			Delta: ChatCompletionChunkDelta{},
		}},
	}

	switch event.Type {
	case "response.output_text.delta":
		chunk.Choices[0].Delta = ChatCompletionChunkDelta{Content: event.Delta}
	case "response.output_item.done":
		if event.Output == nil || event.Output.Type != "function_call" {
			return nil
		}
		chunk.Choices[0].Delta.ToolCalls = []ChatCompletionChunkToolCall{{
			Index: 0,
			ID:    event.Output.ID,
			Type:  "function",
			Function: FunctionCall{
				Name:      event.Output.Name,
				Arguments: event.Output.Args,
			},
		}}
	case "response.completed":
		chunk.Choices[0].FinishReason = "stop"
		if event.Response != nil {
			chunk.Choices[0].FinishReason = responseFinishReason(event.Response)
			usage := event.Response.Usage
			chunk.Usage = &usage
		}
	default:
		return nil
	}

	return chunk
}

func RoughTokenCount(content string) int {
	if content == "" {
		return 0
	}
	return len(content) / 4
}

func normalizeMessages(input any) []Message {
	switch value := input.(type) {
	case nil:
		return nil
	case string:
		return []Message{{Role: "user", Content: value}}
	case []Message:
		return cloneMessages(value)
	case Message:
		return []Message{cloneMessage(value)}
	case []any:
		messages := make([]Message, 0, len(value))
		for _, item := range value {
			messages = append(messages, normalizeMessages(item)...)
		}
		return messages
	case map[string]any:
		msg, ok := normalizeMessageMap(value)
		if !ok {
			return nil
		}
		return []Message{msg}
	default:
		return nil
	}
}

func normalizeMessageMap(value map[string]any) (Message, bool) {
	messageType, _ := value["type"].(string)
	switch messageType {
	case "function_call_output":
		callID, _ := value["call_id"].(string)
		content := value["output"]
		if content == nil {
			content = value["content"]
		}
		return Message{
			Role:       "tool",
			Type:       messageType,
			ToolCallID: callID,
			Content:    normalizeContent(content),
		}, callID != "" || content != nil
	case "function_call":
		return Message{
			Role: "assistant",
			Type: messageType,
			ToolCalls: []ToolCall{{
				ID:   stringValue(value["id"]),
				Type: "function",
				Function: FunctionCall{
					Name:      stringValue(value["name"]),
					Arguments: stringValue(value["arguments"]),
				},
			}},
		}, true
	}

	role, _ := value["role"].(string)
	if role == "" {
		role = "user"
	}

	message := Message{
		Role:       role,
		Type:       messageType,
		Name:       stringValue(value["name"]),
		ToolCallID: stringValue(value["tool_call_id"]),
		Content:    normalizeContent(value["content"]),
		ToolCalls:  normalizeToolCalls(value["tool_calls"]),
	}
	if message.Content == nil {
		if text := firstNonEmpty(collectText(value["text"]), collectText(value["input_text"])); text != "" {
			message.Content = text
		}
	}
	if message.Content == nil && len(message.ToolCalls) == 0 && message.ToolCallID == "" {
		return Message{}, false
	}
	return message, true
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
			text := collectText(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if typeName, _ := current["type"].(string); isToolLikeType(typeName) {
			return ""
		}
		if text, ok := current["text"].(string); ok && text != "" {
			return text
		}
		if value, ok := current["content"]; ok {
			return collectText(value)
		}
		if value, ok := current["input_text"]; ok {
			return collectText(value)
		}
		return ""
	default:
		return fmt.Sprint(current)
	}
}

func cloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}

	result := make([]Message, len(messages))
	for i := range messages {
		result[i] = cloneMessage(messages[i])
	}
	return result
}

func cloneMessage(message Message) Message {
	message.Content = cloneContent(message.Content)
	if len(message.ToolCalls) > 0 {
		message.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
	}
	return message
}

func cloneContent(content any) any {
	if content == nil {
		return nil
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return content
	}
	var cloned any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return content
	}
	return cloned
}

func normalizeContent(value any) any {
	switch current := value.(type) {
	case nil:
		return nil
	case string:
		if current == "" {
			return nil
		}
		return current
	case []ResponseContent:
		if len(current) == 0 {
			return nil
		}
		return current
	case []any:
		if len(current) == 0 {
			return nil
		}
		return cloneContent(current)
	case map[string]any:
		return cloneContent(current)
	default:
		text := collectText(current)
		if text == "" {
			return nil
		}
		return text
	}
}

func normalizeToolCalls(value any) []ToolCall {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]ToolCall, 0, len(list))
	for _, item := range list {
		current, ok := item.(map[string]any)
		if !ok {
			continue
		}
		call := ToolCall{
			ID:   stringValue(current["id"]),
			Type: firstNonEmpty(stringValue(current["type"]), "function"),
		}
		if fn, ok := current["function"].(map[string]any); ok {
			call.Function = FunctionCall{
				Name:      stringValue(fn["name"]),
				Arguments: stringValue(fn["arguments"]),
			}
		}
		if call.Function.Name == "" && call.Function.Arguments == "" && call.ID == "" {
			continue
		}
		result = append(result, call)
	}
	return result
}

func (m Message) Signature() string {
	var b strings.Builder
	if text := collectText(m.Content); text != "" {
		b.WriteString(text)
	}
	if m.ToolCallID != "" {
		b.WriteString("|tool_result:")
		b.WriteString(m.ToolCallID)
	}
	for _, call := range m.ToolCalls {
		b.WriteString("|tool_call:")
		b.WriteString(call.ID)
		b.WriteString(":")
		b.WriteString(call.Function.Name)
		b.WriteString(call.Function.Arguments)
	}
	return b.String()
}

func responseFinishReason(resp *Response) string {
	if len(resp.OutputToolCalls()) > 0 {
		return "tool_calls"
	}
	return "stop"
}

func isToolLikeType(typeName string) bool {
	switch typeName {
	case "function_call", "function_call_output", "tool_use", "tool_result":
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
