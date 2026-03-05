package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gateyes/internal/concurrency"
	"gateyes/internal/pkg/apperror"
	"gateyes/internal/scheduler"
)

type GatewayService struct {
	selector scheduler.Selector
	limiter  concurrency.Manager
	models   []string
	now      func() time.Time
}

func NewGatewayService(selector scheduler.Selector, limiter concurrency.Manager, models []string) *GatewayService {
	return &GatewayService{
		selector: selector,
		limiter:  limiter,
		models:   models,
		now:      time.Now,
	}
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (s *GatewayService) ListModels(_ context.Context) []Model {
	created := s.now().Unix()
	items := make([]Model, 0, len(s.models))
	for _, model := range s.models {
		items = append(items, Model{
			ID:      model,
			Object:  "model",
			Created: created,
			OwnedBy: "gateway",
		})
	}
	return items
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionInput struct {
	Model     string
	Messages  []ChatMessage
	Stream    bool
	TokenID   string
	SessionID string
}

type GatewayDebug struct {
	ChannelID     string `json:"channel_id"`
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionResult struct {
	ID           string       `json:"id"`
	Object       string       `json:"object"`
	Created      int64        `json:"created"`
	Model        string       `json:"model"`
	Choices      []ChatChoice `json:"choices"`
	GatewayDebug GatewayDebug `json:"gateway_debug"`
	Stream       bool         `json:"-"`
}

func (s *GatewayService) ChatCompletions(ctx context.Context, input ChatCompletionInput) (ChatCompletionResult, concurrency.ReleaseFunc, error) {
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return ChatCompletionResult{}, nil, apperror.New(apperror.CodeInvalidArgument, "model is required", nil)
	}
	if len(input.Messages) == 0 {
		return ChatCompletionResult{}, nil, apperror.New(apperror.CodeInvalidArgument, "messages is required", nil)
	}

	decision, err := s.selector.Select(ctx, scheduler.Request{
		Model:     model,
		TokenID:   input.TokenID,
		SessionID: input.SessionID,
	})
	if err != nil {
		if errors.Is(err, scheduler.ErrNoAvailableChannel) {
			return ChatCompletionResult{}, nil, apperror.New(apperror.CodeNoAvailableChannel, "no available channel", err)
		}
		return ChatCompletionResult{}, nil, apperror.Wrap(apperror.CodeInternal, err)
	}

	release, err := s.limiter.Acquire(ctx, concurrency.AcquireKeys{
		ChannelID: decision.ChannelID,
		TokenID:   input.TokenID,
	})
	if err != nil {
		return ChatCompletionResult{}, nil, apperror.New(apperror.CodeRateLimited, err.Error(), err)
	}

	now := s.now()
	result := ChatCompletionResult{
		ID:      fmt.Sprintf("chatcmpl-%d", now.UnixNano()),
		Object:  "chat.completion",
		Created: now.Unix(),
		Model:   model,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: "TODO: upstream forwarding not implemented yet",
				},
				FinishReason: "stop",
			},
		},
		GatewayDebug: GatewayDebug{
			ChannelID:     decision.ChannelID,
			Provider:      decision.Provider,
			UpstreamModel: decision.UpstreamModel,
		},
		Stream: input.Stream,
	}
	return result, release, nil
}

type EmbeddingsInput struct {
	Model     string
	Input     any
	TokenID   string
	SessionID string
}

type EmbeddingItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type EmbeddingsResult struct {
	Object string          `json:"object"`
	Data   []EmbeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingsUsage `json:"usage"`
}

func (s *GatewayService) Embeddings(ctx context.Context, input EmbeddingsInput) (EmbeddingsResult, concurrency.ReleaseFunc, error) {
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return EmbeddingsResult{}, nil, apperror.New(apperror.CodeInvalidArgument, "model is required", nil)
	}
	if input.Input == nil {
		return EmbeddingsResult{}, nil, apperror.New(apperror.CodeInvalidArgument, "input is required", nil)
	}

	decision, err := s.selector.Select(ctx, scheduler.Request{
		Model:     model,
		TokenID:   input.TokenID,
		SessionID: input.SessionID,
	})
	if err != nil {
		if errors.Is(err, scheduler.ErrNoAvailableChannel) {
			return EmbeddingsResult{}, nil, apperror.New(apperror.CodeNoAvailableChannel, "no available channel", err)
		}
		return EmbeddingsResult{}, nil, apperror.Wrap(apperror.CodeInternal, err)
	}

	release, err := s.limiter.Acquire(ctx, concurrency.AcquireKeys{
		ChannelID: decision.ChannelID,
		TokenID:   input.TokenID,
	})
	if err != nil {
		return EmbeddingsResult{}, nil, apperror.New(apperror.CodeRateLimited, err.Error(), err)
	}

	result := EmbeddingsResult{
		Object: "list",
		Data: []EmbeddingItem{
			{
				Object:    "embedding",
				Index:     0,
				Embedding: []float64{0, 0, 0, 0, 0, 0, 0, 0},
			},
		},
		Model: model,
		Usage: EmbeddingsUsage{
			PromptTokens: 0,
			TotalTokens:  0,
		},
	}
	return result, release, nil
}
