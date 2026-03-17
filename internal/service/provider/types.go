package provider

import "context"

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Type    string `json:"type,omitempty"`
}

type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Stream    bool          `json:"stream,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object,omitempty"`
	Created int64    `json:"created,omitempty"`
	Model   string   `json:"model,omitempty"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int         `json:"index,omitempty"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ResponsesRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type ResponsesResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ResponsesChoice `json:"choices"`
	Usage   Usage             `json:"usage"`
}

type ResponsesChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type Provider interface {
	Name() string
	Type() string
	BaseURL() string
	Model() string
	UnitCost() float64
	Cost(promptTokens, completionTokens int) float64
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan string, <-chan error)
}

func ConvertToResponses(chatResp *ChatResponse) *ResponsesResponse {
	choices := make([]ResponsesChoice, 0, len(chatResp.Choices))
	for _, choice := range chatResp.Choices {
		choices = append(choices, ResponsesChoice{
			Index:        choice.Index,
			Message:      choice.Message,
			FinishReason: choice.FinishReason,
		})
	}

	return &ResponsesResponse{
		ID:      chatResp.ID,
		Object:  "response",
		Created: chatResp.Created,
		Model:   chatResp.Model,
		Choices: choices,
		Usage:   chatResp.Usage,
	}
}
