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
	Model             string          `json:"model"`
	PreferredProvider string          `json:"-"`
	Surface           string          `json:"-"`
	Input             any             `json:"input,omitempty"`
	Messages          []Message       `json:"messages,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	MaxTokens         int             `json:"max_tokens,omitempty"`
	Tools             []any           `json:"tools,omitempty"`
	OutputFormat      *OutputFormat   `json:"-"`
	Options           *RequestOptions `json:"-"`
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
