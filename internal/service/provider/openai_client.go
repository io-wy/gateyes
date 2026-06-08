package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	oairesponses "github.com/openai/openai-go/responses"

	"github.com/gateyes/gateway/internal/app/config"
)

type openAIProvider struct {
	baseProvider
	client openai.Client
}

func NewOpenAIProvider(cfg config.ProviderConfig) Provider {
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

	if vendor := strings.TrimSpace(cfg.Vendor); vendor != "" {
		opts = append(opts, option.WithHeader("X-Gateyes-Vendor", vendor))
	}

	client := openai.NewClient(opts...)

	return &openAIProvider{
		baseProvider: bp,
		client:       client,
	}
}

func (p *openAIProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	switch endpoint := strings.TrimSpace(p.cfg.Endpoint); endpoint {
	case "responses":
		return p.createResponses(ctx, req)
	case "", "chat":
		return p.createChatCompletion(ctx, req)
	default:
		return p.createChatCompletion(ctx, req)
	}
}

func (p *openAIProvider) createChatCompletion(ctx context.Context, req *ResponseRequest) (*Response, error) {
	params, err := p.toChatCompletionParams(req)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, p.mapError(err)
	}

	return convertSDKChatCompletion(*resp, req.Model), nil
}

func (p *openAIProvider) createResponses(ctx context.Context, req *ResponseRequest) (*Response, error) {
	params, err := p.toResponseParams(req)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Responses.New(ctx, params)
	if err != nil {
		return nil, p.mapError(err)
	}

	return convertSDKResponse(*resp, req.Model), nil
}

func filterFunctionTools(tools []any) []any {
	filtered := make([]any, 0, len(tools))
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := m["type"].(string); typ == "function" {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (p *openAIProvider) toChatCompletionParams(req *ResponseRequest) (openai.ChatCompletionNewParams, error) {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(req.Model),
		Messages: buildChatCompletionMessages(req.InputMessages()),
	}
	if maxTokens := req.RequestedMaxTokens(); maxTokens > 0 {
		params.MaxTokens = openai.Int(int64(maxTokens))
	}
	if tools := filterFunctionTools(req.Tools); len(tools) > 0 {
		body, err := json.Marshal(tools)
		if err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("marshal tools: %w", err)
		}
		if err := json.Unmarshal(body, &params.Tools); err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("unmarshal tools: %w", err)
		}
	}
	if req.OutputFormat != nil && len(req.OutputFormat.Raw) > 0 {
		body, err := json.Marshal(req.OutputFormat.Raw)
		if err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("marshal response_format: %w", err)
		}
		if err := json.Unmarshal(body, &params.ResponseFormat); err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("unmarshal response_format: %w", err)
		}
	}

	extraBody := buildExtraBody(p.cfg)
	if err := mergeExtraBody(&params, extraBody); err != nil {
		return openai.ChatCompletionNewParams{}, fmt.Errorf("merge extra body: %w", err)
	}

	return params, nil
}

func (p *openAIProvider) toResponseParams(req *ResponseRequest) (oairesponses.ResponseNewParams, error) {
	params := oairesponses.ResponseNewParams{
		Model: oairesponses.ResponsesModel(req.Model),
		Input: oairesponses.ResponseNewParamsInputUnion{
			OfInputItemList: buildOpenAIInput(req.InputMessages()),
		},
	}
	if maxTokens := req.RequestedMaxTokens(); maxTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(maxTokens))
	}
	if tools := filterFunctionTools(req.Tools); len(tools) > 0 {
		body, err := json.Marshal(tools)
		if err != nil {
			return oairesponses.ResponseNewParams{}, fmt.Errorf("marshal tools: %w", err)
		}
		if err := json.Unmarshal(body, &params.Tools); err != nil {
			return oairesponses.ResponseNewParams{}, fmt.Errorf("unmarshal tools: %w", err)
		}
	}

	extraBody := buildExtraBody(p.cfg)
	if err := mergeExtraBody(&params, extraBody); err != nil {
		return oairesponses.ResponseNewParams{}, fmt.Errorf("merge extra body: %w", err)
	}

	return params, nil
}

func (p *openAIProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		switch endpoint := strings.TrimSpace(p.cfg.Endpoint); endpoint {
		case "responses":
			p.streamResponses(ctx, req, result, errCh)
		default:
			p.streamChatCompletion(ctx, req, result, errCh)
		}
	}()

	return result, errCh
}

func (p *openAIProvider) streamChatCompletion(ctx context.Context, req *ResponseRequest, result chan<- ResponseEvent, errCh chan<- error) {
	params, err := p.toChatCompletionParams(req)
	if err != nil {
		errCh <- err
		return
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	var responseID string
	var finalUsage Usage

	for stream.Next() {
		chunk := stream.Current()
		if responseID == "" && chunk.ID != "" {
			responseID = chunk.ID
		}
		event := parseSDKChatCompletionChunk(chunk, req.Model)
		if event != nil {
			if event.Usage != nil {
				finalUsage = *event.Usage
			}
			result <- *event
		}
	}

	if err := stream.Err(); err != nil {
		errCh <- p.mapError(err)
		return
	}

	if responseID == "" {
		responseID = "stream-" + uuid()
	}
	result <- ResponseEvent{
		Type: EventResponseCompleted,
		Response: &Response{
			ID:     responseID,
			Object: "response",
			Model:  req.Model,
			Status: "completed",
			Usage:  finalUsage,
		},
	}
}

func (p *openAIProvider) streamResponses(ctx context.Context, req *ResponseRequest, result chan<- ResponseEvent, errCh chan<- error) {
	params, err := p.toResponseParams(req)
	if err != nil {
		errCh <- err
		return
	}

	stream := p.client.Responses.NewStreaming(ctx, params)
	defer stream.Close()

	var completed bool
	for stream.Next() {
		event := stream.Current()
		parsed, parseErr := parseSDKResponseStreamEvent(event, req.Model)
		if parseErr != nil {
			errCh <- parseErr
			return
		}
		if parsed != nil {
			if parsed.Type == EventResponseCompleted {
				completed = true
			}
			result <- *parsed
		}
	}

	if err := stream.Err(); err != nil {
		errCh <- p.mapError(err)
		return
	}

	if !completed {
		result <- ResponseEvent{Type: EventResponseCompleted}
	}
}

func (p *openAIProvider) CreateImageGeneration(ctx context.Context, req *ImageGenerationRequest) (*ImageGenerationResponse, error) {
	params := openai.ImageGenerateParams{
		Prompt: req.Prompt,
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.cfg.Model
	}
	if model != "" {
		params.Model = openai.ImageModel(model)
	}
	if req.N > 0 {
		params.N = openai.Int(int64(req.N))
	}
	if req.OutputCompression > 0 {
		params.OutputCompression = openai.Int(int64(req.OutputCompression))
	}
	if req.PartialImages > 0 {
		params.PartialImages = openai.Int(int64(req.PartialImages))
	}
	if req.User != "" {
		params.User = openai.String(req.User)
	}
	if req.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(req.Background)
	}
	if req.Moderation != "" {
		params.Moderation = openai.ImageGenerateParamsModeration(req.Moderation)
	}
	if req.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(req.OutputFormat)
	}
	if req.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(req.Quality)
	}
	if req.ResponseFormat != "" {
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormat(req.ResponseFormat)
	}
	if req.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(req.Size)
	}
	if req.Style != "" {
		params.Style = openai.ImageGenerateParamsStyle(req.Style)
	}

	if len(p.cfg.ExtraBody) > 0 {
		params.SetExtraFields(p.cfg.ExtraBody)
	}

	resp, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, p.mapError(err)
	}

	return convertSDKImagesResponse(*resp), nil
}

func convertSDKImagesResponse(resp openai.ImagesResponse) *ImageGenerationResponse {
	result := &ImageGenerationResponse{
		Created: resp.Created,
		Data:    make([]ImageGenerationData, 0, len(resp.Data)),
	}
	for _, item := range resp.Data {
		result.Data = append(result.Data, ImageGenerationData{
			B64JSON:       item.B64JSON,
			RevisedPrompt: item.RevisedPrompt,
			URL:           item.URL,
		})
	}
	if resp.Usage.TotalTokens > 0 || resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		result.Usage = &ImageGenerationUsage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
			TotalTokens:  int(resp.Usage.TotalTokens),
			InputTokensDetails: &ImageGenerationUsageInputTokensDetails{
				ImageTokens: int(resp.Usage.InputTokensDetails.ImageTokens),
				TextTokens:  int(resp.Usage.InputTokensDetails.TextTokens),
			},
		}
	}
	return result
}

func (p *openAIProvider) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	params := openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(req.Model),
	}

	switch v := req.Input.(type) {
	case string:
		params.Input = openai.EmbeddingNewParamsInputUnion{OfString: openai.String(v)}
	case []string:
		params.Input = openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: v}
	}

	if len(p.cfg.ExtraBody) > 0 {
		params.SetExtraFields(p.cfg.ExtraBody)
	}

	resp, err := p.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, p.mapError(err)
	}

	result := &EmbeddingResponse{
		Object: "list",
		Model:  resp.Model,
		Usage: Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: 0,
			TotalTokens:      int(resp.Usage.TotalTokens),
		},
	}

	for _, item := range resp.Data {
		result.Data = append(result.Data, EmbeddingData{
			Object:    "embedding",
			Index:     int(item.Index),
			Embedding: item.Embedding,
		})
	}

	return result, nil
}

func (p *openAIProvider) mapError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return newUpstreamStatusErrorFromCode(apiErr.StatusCode, err)
	}

	return newProviderTransportError("provider.openai", err)
}
