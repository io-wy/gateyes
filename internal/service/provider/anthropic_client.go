package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/gateyes/gateway/internal/config"
)

type anthropicProvider struct {
	baseProvider
	client anthropic.Client
}

func NewAnthropicProvider(cfg config.ProviderConfig) Provider {
	bp := newBaseProvider(cfg)

	opts := []option.RequestOption{
		option.WithBaseURL(cfg.BaseURL),
		option.WithAPIKey(cfg.APIKey),
		option.WithMaxRetries(0),
	}

	for k, v := range cfg.Headers {
		if strings.TrimSpace(k) != "" && v != "" {
			opts = append(opts, option.WithHeader(k, v))
		}
	}

	client := anthropic.NewClient(opts...)

	return &anthropicProvider{
		baseProvider: bp,
		client:       client,
	}
}

func (p *anthropicProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	params, err := buildAnthropicParams(req, p.cfg)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, p.mapError(err)
	}

	return convertSDKAnthropicMessage(*resp, req.Model), nil
}

func (p *anthropicProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		streamReq := *req
		streamReq.Stream = true
		params, err := buildAnthropicParams(&streamReq, p.cfg)
		if err != nil {
			errCh <- err
			return
		}

		stream := p.client.Messages.NewStreaming(ctx, params)
		defer stream.Close()

		state := &anthropicStreamState{
			responseID: "stream-" + uuid(),
			model:      req.Model,
		}

		for stream.Next() {
			event := stream.Current()
			if evt := handleAnthropicStreamEvent(event, state); evt != nil {
				result <- *evt
			}
		}

		if err := stream.Err(); err != nil {
			errCh <- p.mapError(err)
			return
		}

		if !state.completed {
			result <- ResponseEvent{Type: EventResponseCompleted, Response: state.response()}
		}
	}()

	return result, errCh
}

func (p *anthropicProvider) mapError(err error) error {
	if err == nil {
		return nil
	}

	var httpErr interface{ StatusCode() int }
	if errors.As(err, &httpErr) {
		return newUpstreamStatusErrorFromCode(httpErr.StatusCode(), err)
	}

	return newProviderTransportError("provider.anthropic", err)
}
