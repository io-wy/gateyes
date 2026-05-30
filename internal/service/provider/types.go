package provider

import (
	"context"
	"encoding/json"
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
	CachedTokens     int `json:"cached_tokens"`
}

type Provider interface {
	Name() string
	Type() string
	BaseURL() string
	Model() string
	Weight() int
	UnitCost() float64
	Cost(promptTokens, completionTokens int) float64
	CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error)
	StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error)
	CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}

type EmbeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}
