package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ContentBlock struct {
	Type       string             `json:"type"`
	Text       string             `json:"text,omitempty"`
	Thinking   string             `json:"thinking,omitempty"`
	Signature  string             `json:"signature,omitempty"`
	Refusal    string             `json:"refusal,omitempty"`
	Image      *ContentImage      `json:"image,omitempty"`
	Structured *StructuredContent `json:"structured,omitempty"`
}

type ContentImage struct {
	SourceType string `json:"source_type,omitempty"`
	URL        string `json:"url,omitempty"`
	MediaType  string `json:"media_type,omitempty"`
	Data       string `json:"data,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type StructuredContent struct {
	Format string          `json:"format,omitempty"`
	Data   map[string]any  `json:"data,omitempty"`
	Raw    json.RawMessage `json:"raw,omitempty"`
}

type Message struct {
	Role       string         `json:"role,omitempty"`
	Content    []ContentBlock `json:"content,omitempty"`
	Type       string         `json:"type,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Type       string          `json:"type"`
		Name       string          `json:"name"`
		ToolCallID string          `json:"tool_call_id"`
		ToolCalls  []ToolCall      `json:"tool_calls"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	m.Type = raw.Type
	m.Name = raw.Name
	m.ToolCallID = raw.ToolCallID
	m.ToolCalls = raw.ToolCalls
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		m.Content = nil
		return nil
	}
	var contentValue any
	if err := json.Unmarshal(raw.Content, &contentValue); err != nil {
		return err
	}
	m.Content = NormalizeMessageContent(contentValue)
	return nil
}

type ChatMessage struct {
	Role       string     `json:"role,omitempty"`
	Content    any        `json:"content,omitempty"`
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
	Model           string          `json:"model"`
	Input           any             `json:"input,omitempty"`
	Messages        []Message       `json:"messages,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Tools           []any           `json:"tools,omitempty"`
	OutputFormat    *OutputFormat   `json:"-"`
	Options         *RequestOptions `json:"-"`
}

type OutputFormat struct {
	Type   string         `json:"type,omitempty"`
	Name   string         `json:"name,omitempty"`
	Strict bool           `json:"strict,omitempty"`
	Schema map[string]any `json:"schema,omitempty"`
	Raw    map[string]any `json:"raw,omitempty"`
}

type RequestOptions struct {
	System       string                 `json:"-"`
	Thinking     *AnthropicThinking     `json:"-"`
	CacheControl *AnthropicCacheControl `json:"-"`
	Raw          map[string]any         `json:"-"`
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
	Type       string             `json:"type"`
	Text       string             `json:"text,omitempty"`
	Thinking   string             `json:"thinking,omitempty"`
	Signature  string             `json:"signature,omitempty"`
	Refusal    string             `json:"refusal,omitempty"`
	Image      *ContentImage      `json:"image,omitempty"`
	Structured *StructuredContent `json:"structured,omitempty"`
}

const (
	EventResponseStarted   = "response_started"
	EventContentDelta      = "content_delta"
	EventToolCallDone      = "tool_call_done"
	EventResponseCompleted = "response_completed"
	EventThinkingDelta     = "thinking_delta"
)

type ResponseEvent struct {
	Type          string          `json:"type"`
	Delta         string          `json:"delta,omitempty"`
	TextDelta     string          `json:"-"`
	ThinkingDelta string          `json:"-"`
	Response      *Response       `json:"response,omitempty"`
	Output        *ResponseOutput `json:"output,omitempty"`
	ToolCalls     []ToolCall      `json:"tool_calls,omitempty"`
	FinishReason  string          `json:"finish_reason,omitempty"`
	Usage         *Usage          `json:"usage,omitempty"`
}

func (e ResponseEvent) Text() string {
	if e.TextDelta != "" {
		return e.TextDelta
	}
	return e.Delta
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"` // 上游返回的缓存命中 token 数（OpenAI cached_tokens / Anthropic cache_hit_input_tokens）
}

type ChatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []ChatMessage `json:"messages"`
	Stream         bool          `json:"stream,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	Tools          []any         `json:"tools,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
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
	Index        int         `json:"index,omitempty"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
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

// Anthropic Messages API types

type AnthropicMessagesRequest struct {
	Model         string                 `json:"model"`
	Messages      []AnthropicMessage     `json:"messages"`
	System        any                    `json:"system,omitempty"` // string or []AnthropicSystemBlock
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
	// content can be a string or an array
	if raw.Content[0] == '"' {
		var s string
		if err := json.Unmarshal(raw.Content, &s); err != nil {
			return err
		}
		m.Content = []AnthropicContentBlock{{Type: "text", Text: s}}
	} else {
		if err := json.Unmarshal(raw.Content, &m.Content); err != nil {
			return err
		}
	}
	return nil
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

// Anthropic streaming types

type AnthropicEvent struct {
	Type    string                     `json:"type"`
	Index   int                        `json:"index,omitempty"`
	Delta   any                        `json:"delta,omitempty"`
	Content []AnthropicContentBlock    `json:"content,omitempty"`
	Block   *AnthropicContentBlock     `json:"block,omitempty"`
	Message *AnthropicMessagesResponse `json:"message,omitempty"`
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

// TODO: 这步没有意义，可以直接读 api 返回的 input tokens 数，后续优化
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

// TODO: 这步没有意义，可以直接读 api 返回的 input tokens 数，后续优化
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

func ConvertChatRequest(req *ChatCompletionRequest) *ResponseRequest {
	if req == nil {
		return nil
	}

	messages := make([]Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, Message{
			Role:       msg.Role,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  append([]ToolCall(nil), msg.ToolCalls...),
			Content:    NormalizeMessageContent(msg.Content),
		})
	}
	return &ResponseRequest{
		Model:        req.Model,
		Input:        messages,
		Messages:     messages,
		Stream:       req.Stream,
		MaxTokens:    req.MaxTokens,
		Tools:        req.Tools,
		OutputFormat: normalizeOutputFormatValue(req.ResponseFormat),
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
			Message: ChatMessage{
				Role:      "assistant",
				Content:   resp.OutputText(),
				ToolCalls: resp.OutputToolCalls(),
			},
			FinishReason: responseFinishReason(resp),
		}},
		Usage: resp.Usage,
	}
}

func normalizeOutputFormatValue(value any) *OutputFormat {
	current, ok := value.(map[string]any)
	if !ok || len(current) == 0 {
		return nil
	}
	format := &OutputFormat{
		Raw: current,
	}
	format.Type, _ = current["type"].(string)
	if jsonSchema, ok := current["json_schema"].(map[string]any); ok {
		format.Type = "json_schema"
		format.Name, _ = jsonSchema["name"].(string)
		format.Strict, _ = jsonSchema["strict"].(bool)
		if schema, ok := jsonSchema["schema"].(map[string]any); ok {
			format.Schema = schema
		}
	}
	if format.Type == "" {
		format.Type = "text"
	}
	return format
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
	case EventResponseStarted:
		// 流开始事件，返回一个初始 chunk
		return chunk
	case EventContentDelta:
		chunk.Choices[0].Delta = ChatCompletionChunkDelta{Content: event.Text()}
		chunk.Choices[0].FinishReason = event.FinishReason
		if len(event.ToolCalls) > 0 {
			toolCalls := make([]ChatCompletionChunkToolCall, 0, len(event.ToolCalls))
			for index, call := range event.ToolCalls {
				toolCalls = append(toolCalls, ChatCompletionChunkToolCall{
					Index: index,
					ID:    call.ID,
					Type:  firstNonEmpty(call.Type, "function"),
					Function: FunctionCall{
						Name:      call.Function.Name,
						Arguments: call.Function.Arguments,
					},
				})
			}
			chunk.Choices[0].Delta.ToolCalls = toolCalls
		}
		if event.Usage != nil {
			chunk.Usage = event.Usage
		}
	case EventToolCallDone:
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
	case EventResponseCompleted:
		chunk.Choices[0].FinishReason = "stop"
		if event.Response != nil {
			chunk.Choices[0].FinishReason = responseFinishReason(event.Response)
			usage := event.Response.Usage
			chunk.Usage = &usage
		}
	default:
		// 对于未知事件类型，返回空 chunk 而非 nil
		return chunk
	}

	return chunk
}

func CloneRequestOptions(value *RequestOptions) *RequestOptions {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Thinking != nil {
		thinking := *value.Thinking
		cloned.Thinking = &thinking
	}
	if value.CacheControl != nil {
		cacheControl := *value.CacheControl
		cloned.CacheControl = &cacheControl
	}
	cloned.Raw = cloneStringAnyMapLocal(value.Raw)
	return &cloned
}

func cloneStringAnyMapLocal(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		cloned := make(map[string]any, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		fallback := make(map[string]any, len(value))
		for key, item := range value {
			fallback[key] = item
		}
		return fallback
	}
	return cloned
}

// === Anthropic Messages API conversion ===

func ConvertAnthropicRequest(req *AnthropicMessagesRequest) *ResponseRequest {
	if req == nil {
		return nil
	}

	messages := convertAnthropicMessages(req.Messages)
	// Convert Anthropic tools to OpenAI format
	var tools []any
	for _, tool := range req.Tools {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}

	// Handle system field (can be string or array)
	systemText := convertAnthropicSystem(req.System)

	return &ResponseRequest{
		Model:     req.Model,
		Input:     messages,
		Messages:  messages,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
		Tools:     tools,
		Options: &RequestOptions{
			System:       systemText,
			Thinking:     req.Thinking,
			CacheControl: req.CacheControl,
		},
	}
}

func convertAnthropicSystem(system any) string {
	switch s := system.(type) {
	case string:
		return s
	case []any:
		// Array of system blocks
		var parts []string
		for _, item := range s {
			if block, ok := item.(map[string]any); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func convertAnthropicMessages(msgs []AnthropicMessage) []Message {
	result := make([]Message, 0, len(msgs))
	for _, msg := range msgs {
		content := make([]ContentBlock, 0, len(msg.Content))
		toolCalls := make([]ToolCall, 0)
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_use":
				inputMap := make(map[string]any)
				if len(block.Input) > 0 {
					_ = json.Unmarshal(block.Input, &inputMap)
				}
				raw, _ := json.Marshal(inputMap)
				toolCalls = append(toolCalls, ToolCall{
					ID:   block.ID,
					Type: "function",
					Function: FunctionCall{
						Name:      block.Name,
						Arguments: string(raw),
					},
				})
			case "tool_result":
				result = append(result, Message{
					Role:       "tool",
					ToolCallID: block.ToolUseID,
					Content:    TextBlocks(firstNonEmpty(block.Content, block.Text)),
				})
			default:
				content = append(content, convertAnthropicBlock(block)...)
			}
		}
		if len(content) == 0 && len(toolCalls) == 0 {
			continue
		}
		result = append(result, Message{Role: msg.Role, Content: content, ToolCalls: toolCalls})
	}
	return result
}

func convertAnthropicMessage(msg AnthropicMessage) Message {
	content := make([]ContentBlock, 0, len(msg.Content))
	toolCalls := make([]ToolCall, 0)
	toolResults := make([]Message, 0)
	for _, block := range msg.Content {
		switch block.Type {
		case "tool_use":
			inputMap := make(map[string]any)
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &inputMap)
			}
			raw, _ := json.Marshal(inputMap)
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(raw),
				},
			})
		case "tool_result":
			toolResults = append(toolResults, Message{
				Role:       "tool",
				ToolCallID: block.ToolUseID,
				Content:    TextBlocks(firstNonEmpty(block.Content, block.Text)),
			})
		default:
			content = append(content, convertAnthropicBlock(block)...)
		}
	}
	if len(toolResults) > 0 && len(content) == 0 && len(toolCalls) == 0 {
		return toolResults[0]
	}
	return Message{Role: msg.Role, Content: content, ToolCalls: toolCalls}
}

func convertAnthropicContent(blocks []AnthropicContentBlock) []ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, convertAnthropicBlock(block)...)
	}
	return result
}

func convertAnthropicBlock(block AnthropicContentBlock) []ContentBlock {
	switch block.Type {
	case "text":
		return TextBlocks(block.Text)
	case "thinking":
		return []ContentBlock{{
			Type:      "thinking",
			Thinking:  block.Thinking,
			Signature: block.Signature,
		}}
	case "image":
		if block.Source == nil {
			return nil
		}
		return []ContentBlock{{
			Type: "image",
			Image: &ContentImage{
				SourceType: block.Source.Type,
				MediaType:  block.Source.MediaType,
				Data:       block.Source.Data,
			},
		}}
	default:
		return nil
	}
}

func ConvertResponseToAnthropic(resp *Response) *AnthropicMessagesResponse {
	if resp == nil {
		return nil
	}

	stopReason := "end_turn"
	if len(resp.OutputToolCalls()) > 0 {
		stopReason = "tool_use"
	}

	return &AnthropicMessagesResponse{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    convertResponseToAnthropicContent(resp.Output),
		Model:      resp.Model,
		StopReason: stopReason,
		Usage: AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

func convertResponseToAnthropicContent(outputs []ResponseOutput) []AnthropicContentBlock {
	blocks := make([]AnthropicContentBlock, 0)
	for _, output := range outputs {
		switch output.Type {
		case "message":
			for _, content := range output.Content {
				switch content.Type {
				case "thinking":
					blocks = append(blocks, AnthropicContentBlock{
						Type:      "thinking",
						Thinking:  content.Thinking,
						Signature: content.Signature,
					})
				case "output_text":
					if content.Text != "" {
						blocks = append(blocks, AnthropicContentBlock{
							Type: "text",
							Text: content.Text,
						})
					}
				}
			}
		case "function_call":
			inputMap := make(map[string]any)
			if len(output.Args) > 0 {
				_ = json.Unmarshal([]byte(output.Args), &inputMap)
			}
			inputBytes, _ := json.Marshal(inputMap)
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    firstNonEmpty(output.ID, output.CallID),
				Name:  output.Name,
				Input: inputBytes,
			})
		}
	}
	return blocks
}

// ConvertEventToAnthropicEvent converts internal event to Anthropic streaming format
func ConvertEventToAnthropicEvent(responseID, model string, event ResponseEvent) *AnthropicEvent {
	switch event.Type {
	case EventResponseStarted:
		// Initial message start
		return &AnthropicEvent{
			Type: "message_start",
			Message: &AnthropicMessagesResponse{
				ID:      responseID,
				Type:    "message",
				Model:   model,
				Content: []AnthropicContentBlock{},
				Usage:   AnthropicUsage{},
			},
		}
	case EventContentDelta:
		if event.Delta == "" {
			if len(event.ToolCalls) > 0 {
				blocks := make([]AnthropicContentBlock, 0, len(event.ToolCalls))
				for _, call := range event.ToolCalls {
					blocks = append(blocks, AnthropicContentBlock{
						Type:  "tool_use",
						ID:    call.ID,
						Name:  call.Function.Name,
						Input: marshalRawJSON(call.Function.Arguments),
					})
				}
				if len(blocks) > 0 {
					return &AnthropicEvent{Type: "content", Content: blocks}
				}
			}
			return nil
		}
		return &AnthropicEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: map[string]any{
				"type": "text_delta",
				"text": event.Delta,
			},
		}
	case EventToolCallDone:
		if event.Output == nil {
			return nil
		}
		if event.Output.Type == "function_call" {
			inputMap := make(map[string]any)
			if len(event.Output.Args) > 0 {
				_ = json.Unmarshal([]byte(event.Output.Args), &inputMap)
			}
			return &AnthropicEvent{
				Type:  "content_block_stop",
				Index: 0,
			}
		}
		return &AnthropicEvent{
			Type: "content_block_stop",
		}
	case EventResponseCompleted:
		stopReason := "end_turn"
		if event.Response != nil && len(event.Response.OutputToolCalls()) > 0 {
			stopReason = "tool_use"
		}
		var usage AnthropicUsage
		if event.Response != nil {
			usage = AnthropicUsage{
				InputTokens:  event.Response.Usage.PromptTokens,
				OutputTokens: event.Response.Usage.CompletionTokens,
			}
		}
		return &AnthropicEvent{
			Type:  "message_delta",
			Delta: stopReason,
			Message: &AnthropicMessagesResponse{
				StopReason: stopReason,
				Usage:      usage,
			},
		}
	}
	return nil
}

func RoughTokenCount(content string) int {
	if content == "" {
		return 0
	}
	return len(content) / 4
}

// DefaultMaxOutputTokens 是未指定 max_output_tokens 时的保守估计值
const DefaultMaxOutputTokens = 4096

// EstimateAdmissionTokens 估算准入 token 数，用于限流
// 计算逻辑：prompt estimation + output budget
// P3 fix: 之前只算 prompt，不算 max_tokens，导致长输出请求白嫖 limiter
func (r *ResponseRequest) EstimateAdmissionTokens() int {
	promptTokens := r.EstimatePromptTokens()

	// output budget: 优先用用户指定的 max_tokens/max_output_tokens
	maxTokens := r.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = r.MaxTokens
	}
	// 没传则用保守默认值
	if maxTokens <= 0 {
		maxTokens = DefaultMaxOutputTokens
	}

	return promptTokens + maxTokens
}

func normalizeMessages(input any) []Message {
	switch value := input.(type) {
	case nil:
		return nil
	case string:
		return []Message{{Role: "user", Content: TextBlocks(value)}}
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

func TextBlocks(text string) []ContentBlock {
	if text == "" {
		return nil
	}
	return []ContentBlock{{Type: "text", Text: text}}
}

func NormalizeMessageContent(value any) []ContentBlock {
	return normalizeContentBlocks(value)
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
			Content:    normalizeContentBlocks(content),
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
		Content:    normalizeContentBlocks(value["content"]),
		ToolCalls:  normalizeToolCalls(value["tool_calls"]),
	}
	if len(message.Content) == 0 {
		if text := firstNonEmpty(collectText(value["text"]), collectText(value["input_text"])); text != "" {
			message.Content = TextBlocks(text)
		}
	}
	if len(message.Content) == 0 && len(message.ToolCalls) == 0 && message.ToolCallID == "" {
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
	case []ContentBlock:
		parts := make([]string, 0, len(current))
		for _, block := range current {
			switch block.Type {
			case "text", "output_text":
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			case "thinking":
				if block.Thinking != "" {
					parts = append(parts, block.Thinking)
				}
			case "refusal":
				if block.Refusal != "" {
					parts = append(parts, block.Refusal)
				}
			}
		}
		return strings.Join(parts, "")
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
	message.Content = cloneContentBlocks(message.Content)
	if len(message.ToolCalls) > 0 {
		message.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
	}
	return message
}

func cloneContentBlocks(content []ContentBlock) []ContentBlock {
	if len(content) == 0 {
		return nil
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return content
	}
	var cloned []ContentBlock
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return content
	}
	return cloned
}

func normalizeContent(value any) any {
	return normalizeContentBlocks(value)
}

func normalizeContentBlocks(value any) []ContentBlock {
	switch current := value.(type) {
	case nil:
		return nil
	case string:
		return TextBlocks(current)
	case []ContentBlock:
		if len(current) == 0 {
			return nil
		}
		return append([]ContentBlock(nil), current...)
	case []ResponseContent:
		result := make([]ContentBlock, 0, len(current))
		for _, item := range current {
			result = append(result, responseContentToBlocks(item)...)
		}
		return result
	case []any:
		result := make([]ContentBlock, 0, len(current))
		for _, item := range current {
			result = append(result, normalizeContentBlocks(item)...)
		}
		return result
	case map[string]any:
		return normalizeContentBlockMap(current)
	default:
		text := collectText(current)
		return TextBlocks(text)
	}
}

func normalizeContentBlockMap(current map[string]any) []ContentBlock {
	typeName := firstNonEmpty(stringValue(current["type"]), "text")
	switch typeName {
	case "text", "input_text", "output_text":
		text := firstNonEmpty(stringValue(current["text"]), collectText(current["content"]), collectText(current["input_text"]))
		return TextBlocks(text)
	case "thinking":
		thinking := firstNonEmpty(stringValue(current["thinking"]), stringValue(current["text"]))
		if thinking == "" {
			return nil
		}
		return []ContentBlock{{
			Type:      "thinking",
			Thinking:  thinking,
			Signature: stringValue(current["signature"]),
		}}
	case "refusal":
		refusal := firstNonEmpty(stringValue(current["refusal"]), stringValue(current["text"]))
		if refusal == "" {
			return nil
		}
		return []ContentBlock{{Type: "refusal", Refusal: refusal}}
	case "image", "image_url":
		image := normalizeImageBlock(current)
		if image == nil {
			return nil
		}
		return []ContentBlock{{Type: "image", Image: image}}
	case "structured_output", "json":
		structured := normalizeStructuredContent(current)
		if structured == nil {
			return nil
		}
		return []ContentBlock{{Type: "structured_output", Structured: structured}}
	default:
		text := collectText(current)
		return TextBlocks(text)
	}
}

func responseContentToBlocks(item ResponseContent) []ContentBlock {
	switch item.Type {
	case "thinking":
		if item.Thinking == "" {
			return nil
		}
		return []ContentBlock{{
			Type:      "thinking",
			Thinking:  item.Thinking,
			Signature: item.Signature,
		}}
	case "output_text", "text":
		return TextBlocks(item.Text)
	case "refusal":
		if item.Text == "" {
			return nil
		}
		return []ContentBlock{{Type: "refusal", Refusal: item.Text}}
	default:
		return TextBlocks(item.Text)
	}
}

func normalizeImageBlock(current map[string]any) *ContentImage {
	if imageURL, ok := current["image_url"].(map[string]any); ok {
		return &ContentImage{
			SourceType: "url",
			URL:        stringValue(imageURL["url"]),
			Detail:     stringValue(imageURL["detail"]),
		}
	}
	if source, ok := current["source"].(map[string]any); ok {
		return &ContentImage{
			SourceType: stringValue(source["type"]),
			URL:        stringValue(source["url"]),
			MediaType:  stringValue(source["media_type"]),
			Data:       stringValue(source["data"]),
		}
	}
	return nil
}

func normalizeStructuredContent(current map[string]any) *StructuredContent {
	structured := &StructuredContent{
		Format: stringValue(current["format"]),
	}
	if structured.Format == "" {
		structured.Format = "json"
	}
	if data, ok := current["data"].(map[string]any); ok {
		structured.Data = data
	}
	if raw, ok := current["raw"].(string); ok && raw != "" {
		structured.Raw = json.RawMessage(raw)
	}
	return structured
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
