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

type openAIProvider struct {
	baseProvider
}

func NewOpenAIProvider(cfg config.ProviderConfig) Provider {
	return &openAIProvider{
		baseProvider: newBaseProvider(cfg),
	}
}

func (p *openAIProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	httpReq, err := p.newRequest(ctx, req, false)
	if err != nil {
		return nil, err
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, newProviderTransportError("provider.openai.create_response", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, newUpstreamStatusError(httpResp)
	}

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, newProviderParseError("provider.openai.read_response", err, "read upstream response body")
	}

	switch detectResponseFormat(bodyBytes) {
	case "chat":
		var raw chatCompletionResponse
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			return nil, newProviderParseError("provider.openai.parse_chat_response", err, "decode chat completion response")
		}
		return convertChatCompletionResponse(raw, req.Model), nil
	case "responses":
		var raw openAIResponsePayload
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			return nil, newProviderParseError("provider.openai.parse_response", err, "decode responses payload")
		}
		return convertOpenAIResponse(raw, req.Model), nil
	default:
		return p.parseFallbackResponse(bodyBytes, req.Model)
	}
}

func (p *openAIProvider) parseFallbackResponse(body []byte, requestedModel string) (*Response, error) {
	var raw openAIResponsePayload
	if err := json.Unmarshal(body, &raw); err == nil {
		return convertOpenAIResponse(raw, requestedModel), nil
	}

	var chatRaw chatCompletionResponse
	if err := json.Unmarshal(body, &chatRaw); err != nil {
		return nil, newProviderParseError("provider.openai.parse_fallback_response", err, "unable to parse upstream response")
	}
	return convertChatCompletionResponse(chatRaw, requestedModel), nil
}

func (p *openAIProvider) newRequest(ctx context.Context, req *ResponseRequest, stream bool) (*http.Request, error) {
	path, payload := p.requestPathAndPayload(req, stream)

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIPath(p.cfg.BaseURL, path), bytes.NewReader(body))
	if err != nil {
		return nil, newProviderConfigError("provider.openai.new_request", err.Error())
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	applyProviderProfile(p.cfg, payload, httpReq.Header)

	body, _ = json.Marshal(payload)
	httpReq.Body = io.NopCloser(bytes.NewReader(body))
	httpReq.ContentLength = int64(len(body))
	return httpReq, nil
}

func (p *openAIProvider) requestPathAndPayload(req *ResponseRequest, stream bool) (string, map[string]any) {
	switch endpoint := strings.TrimSpace(p.cfg.Endpoint); endpoint {
	case "responses":
		payload := map[string]any{
			"model":  req.Model,
			"input":  buildOpenAIInput(req.InputMessages()),
			"stream": stream,
		}
		if maxTokens := req.RequestedMaxTokens(); maxTokens > 0 {
			payload["max_output_tokens"] = maxTokens
		}
		if len(req.Tools) > 0 {
			payload["tools"] = req.Tools
		}
		if req.OutputFormat != nil && len(req.OutputFormat.Raw) > 0 {
			payload["response_format"] = req.OutputFormat.Raw
		}
		return "/responses", payload
	case "", "chat":
		return "/v1/chat/completions", buildOpenAIChatPayload(req, stream)
	default:
		return endpoint, buildOpenAIChatPayload(req, stream)
	}
}

func buildOpenAIChatPayload(req *ResponseRequest, stream bool) map[string]any {
	payload := map[string]any{
		"model":    req.Model,
		"messages": buildChatCompletionMessages(req.InputMessages()),
		"stream":   stream,
	}
	if maxTokens := req.RequestedMaxTokens(); maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if req.OutputFormat != nil && len(req.OutputFormat.Raw) > 0 {
		payload["response_format"] = req.OutputFormat.Raw
	}
	return payload
}

func joinOpenAIPath(baseURL, path string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1") && strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	return base + path
}

func (p *openAIProvider) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIPath(p.cfg.BaseURL, "/v1/embeddings"), bytes.NewReader(body))
	if err != nil {
		return nil, newProviderConfigError("provider.openai.new_embedding_request", err.Error())
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	applyProviderProfile(p.cfg, payload, httpReq.Header)

	body, _ = json.Marshal(payload)
	httpReq.Body = io.NopCloser(bytes.NewReader(body))
	httpReq.ContentLength = int64(len(body))

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, newProviderTransportError("provider.openai.create_embedding", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, newUpstreamStatusError(httpResp)
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, newProviderParseError("provider.openai.read_embedding_response", err, "read upstream embedding body")
	}

	var result EmbeddingResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, newProviderParseError("provider.openai.parse_embedding_response", err, "decode embedding response")
	}
	return &result, nil
}
