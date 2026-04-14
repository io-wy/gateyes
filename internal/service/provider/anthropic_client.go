package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gateyes/gateway/internal/config"
)

const anthropicVersion = "2023-06-01"

type anthropicProvider struct {
	baseProvider
}

func NewAnthropicProvider(cfg config.ProviderConfig) Provider {
	return &anthropicProvider{
		baseProvider: newBaseProvider(cfg),
	}
}

func (p *anthropicProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	params, err := p.buildParams(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := p.newRequest(ctx, params, false)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, newProviderTransportError("provider.anthropic.create_response", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, newUpstreamStatusError(resp)
	}

	var anthropicResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, newProviderParseError("provider.anthropic.parse_response", err, "decode anthropic response")
	}

	return convertAnthropicResponse(anthropicResp, req.Model), nil
}

func (p *anthropicProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		params, err := p.buildParams(req)
		if err != nil {
			errCh <- err
			return
		}

		httpReq, err := p.newRequest(ctx, params, true)
		if err != nil {
			errCh <- err
			return
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- newProviderTransportError("provider.anthropic.stream_response", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errCh <- newUpstreamStatusError(resp)
			return
		}

		p.parseStream(resp.Body, result, errCh, req.Model)
	}()

	return result, errCh
}

func (p *anthropicProvider) newRequest(ctx context.Context, params map[string]any, stream bool) (*http.Request, error) {
	body, _ := json.Marshal(params)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, newProviderConfigError("provider.anthropic.new_request", err.Error())
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	applyProviderProfile(p.cfg, params, httpReq.Header)

	body, _ = json.Marshal(params)
	httpReq.Body = io.NopCloser(bytes.NewReader(body))
	httpReq.ContentLength = int64(len(body))
	return httpReq, nil
}
