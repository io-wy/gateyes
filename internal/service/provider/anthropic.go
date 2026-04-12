package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

const anthropicVersion = "2023-06-01"

type anthropicProvider struct {
	cfg    config.ProviderConfig
	client *http.Client
}

func NewAnthropicProvider(cfg config.ProviderConfig) Provider {
	return &anthropicProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}
}

func (p *anthropicProvider) Name() string      { return p.cfg.Name }
func (p *anthropicProvider) Type() string      { return p.cfg.Type }
func (p *anthropicProvider) BaseURL() string   { return p.cfg.BaseURL }
func (p *anthropicProvider) Model() string     { return p.cfg.Model }
func (p *anthropicProvider) UnitCost() float64 { return p.cfg.PriceInput + p.cfg.PriceOutput }
func (p *anthropicProvider) Cost(prompt, completion int) float64 {
	return float64(prompt)*p.cfg.PriceInput + float64(completion)*p.cfg.PriceOutput
}

func (p *anthropicProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	params, err := p.buildParams(req)
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(params)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Message: string(data)}
	}

	var anthropicResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, err
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

		body, _ := json.Marshal(params)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(p.cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", p.cfg.APIKey)
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			errCh <- &UpstreamError{StatusCode: resp.StatusCode, Message: string(data)}
			return
		}

		// 流式解析
		p.parseStream(resp.Body, result, errCh, req.Model)
	}()

	return result, errCh
}

func (p *anthropicProvider) parseStream(body io.Reader, result chan<- ResponseEvent, errCh chan<- error, model string) {
	reader := bufio.NewReader(body)
	responseID := "stream-" + uuid()
	state := &anthropicStreamState{
		responseID: responseID,
		model:      model,
	}
	var eventName string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if event := parseAnthropicStreamEvent(eventName, strings.Join(dataLines, "\n"), state); event != nil {
					result <- *event
				}
				if !state.completed {
					result <- ResponseEvent{Type: EventResponseCompleted, Response: state.response()}
				}
				break
			}
			errCh <- err
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			data := strings.Join(dataLines, "\n")
			if data == "[DONE]" {
				return
			}
			if strings.Contains(data, "\"type\":\"error\"") || strings.Contains(data, "\"error\":") {
				var errResp struct {
					Type  string `json:"type"`
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal([]byte(data), &errResp) == nil && errResp.Error.Message != "" {
					errCh <- &UpstreamError{StatusCode: 0, Message: errResp.Error.Message}
					return
				}
			}
			if event := parseAnthropicStreamEvent(eventName, data, state); event != nil {
				result <- *event
			}
			eventName = ""
			dataLines = nil
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

// uuid generates a simple UUID-like string
func uuid() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func parseAnthropicStreamEvent(eventName, data string, state *anthropicStreamState) *ResponseEvent {
	if state == nil || data == "" {
		return nil
	}
	var event struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(data), &event) != nil && eventName == "" {
		return nil
	}
	if eventName == "" {
		eventName = event.Type
	}

	switch eventName {
	case "message_start":
		var payload struct {
			Message struct {
				ID      string                  `json:"id"`
				Content []AnthropicContentBlock `json:"content"`
				Usage   struct {
					InputTokens         int `json:"input_tokens"`
					OutputTokens        int `json:"output_tokens"`
					CacheHitInputTokens int `json:"cache_hit_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		if payload.Message.ID != "" {
			state.responseID = payload.Message.ID
		}
		state.promptTokens = payload.Message.Usage.InputTokens
		state.cachedTokens = payload.Message.Usage.CacheHitInputTokens
		for _, block := range payload.Message.Content {
			state.applyContentBlock(block)
		}
		return nil

	case "content_block_start":
		var payload struct {
			Index        int                   `json:"index"`
			ContentBlock AnthropicContentBlock `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		switch payload.ContentBlock.Type {
		case "text":
			if payload.ContentBlock.Text != "" {
				state.appendText(payload.ContentBlock.Text)
				return &ResponseEvent{Type: EventContentDelta, Delta: payload.ContentBlock.Text}
			}
		case "thinking":
			if payload.ContentBlock.Thinking != "" {
				state.appendThinking(payload.ContentBlock.Thinking, payload.ContentBlock.Signature)
			}
		case "tool_use":
			args := strings.TrimSpace(string(payload.ContentBlock.Input))
			if args == "" {
				args = "{}"
			}
			state.activeTool = &ResponseOutput{
				ID:     payload.ContentBlock.ID,
				Type:   "function_call",
				Status: "completed",
				CallID: payload.ContentBlock.ID,
				Name:   payload.ContentBlock.Name,
				Args:   args,
			}
		}
		return nil

	case "content_block_delta":
		var payload struct {
			Index int `json:"index"`
			Delta any `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}

		// Handle different delta formats
		if deltaMap, ok := payload.Delta.(map[string]any); ok {
			if deltaType, _ := deltaMap["type"].(string); deltaType == "text_delta" {
				if text, _ := deltaMap["text"].(string); text != "" {
					state.appendText(text)
					return &ResponseEvent{Type: EventContentDelta, Delta: text}
				}
			}
			if text, _ := deltaMap["text"].(string); text != "" {
				state.appendText(text)
				return &ResponseEvent{Type: EventContentDelta, Delta: text}
			}
			if thinking, _ := deltaMap["thinking"].(string); thinking != "" {
				state.appendThinking(thinking, "")
			}
			if partial, _ := deltaMap["partial_json"].(string); partial != "" && state.activeTool != nil {
				state.activeTool.Args += partial
			}
		}
		return nil

	case "content_block_stop":
		if state.activeTool == nil {
			return nil
		}
		output := *state.activeTool
		state.outputs = append(state.outputs, output)
		state.activeTool = nil
		return &ResponseEvent{Type: EventToolCallDone, Output: &output}

	case "message_delta":
		// 扩展：支持更多第三方 provider 的 message_delta 变体
		var payload struct {
			Delta any `json:"delta"` // 可能是 {"type":"text","text":"..."} 或 {"type":"text_delta","text":"..."}
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Content string `json:"content"` // 某些 provider 直接在 message_delta 里发文本
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		// 尝试从 delta 对象中提取 text
		if deltaMap, ok := payload.Delta.(map[string]any); ok {
			if text, _ := deltaMap["text"].(string); text != "" {
				state.appendText(text)
				return &ResponseEvent{Type: EventContentDelta, Delta: text}
			}
		}
		// 如果有 content 字段（某些 provider 变体）
		if payload.Content != "" {
			state.appendText(payload.Content)
			return &ResponseEvent{Type: EventContentDelta, Delta: payload.Content}
		}
		if payload.Usage.OutputTokens > 0 {
			state.completionTokens = payload.Usage.OutputTokens
		}
		return nil

	case "message_stop":
		state.completed = true
		return &ResponseEvent{Type: EventResponseCompleted, Response: state.response()}

	case "ping":
		return nil

	// 扩展：支持更多第三方 provider 的事件变体
	case "text_block", "content_block": // 某些 provider 使用不同的事件名
		var payload struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(data), &payload) == nil && payload.Text != "" {
			state.appendText(payload.Text)
			return &ResponseEvent{Type: EventContentDelta, Delta: payload.Text}
		}
		return nil
	}

	return nil
}

type anthropicStreamState struct {
	responseID       string
	model            string
	promptTokens     int
	completionTokens int
	cachedTokens     int
	outputs          []ResponseOutput
	activeTool       *ResponseOutput
	completed        bool
}

func (s *anthropicStreamState) appendText(text string) {
	if text == "" {
		return
	}
	if len(s.outputs) == 0 || s.outputs[len(s.outputs)-1].Type != "message" {
		s.outputs = append(s.outputs, ResponseOutput{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
		})
	}
	index := len(s.outputs) - 1
	s.outputs[index].Content = append(s.outputs[index].Content, ResponseContent{
		Type: "output_text",
		Text: text,
	})
}

func (s *anthropicStreamState) appendThinking(thinking, signature string) {
	if thinking == "" {
		return
	}
	if len(s.outputs) == 0 || s.outputs[len(s.outputs)-1].Type != "message" {
		s.outputs = append(s.outputs, ResponseOutput{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
		})
	}
	index := len(s.outputs) - 1
	s.outputs[index].Content = append(s.outputs[index].Content, ResponseContent{
		Type:      "thinking",
		Thinking:  thinking,
		Signature: signature,
	})
}

func (s *anthropicStreamState) applyContentBlock(block AnthropicContentBlock) {
	switch block.Type {
	case "text":
		s.appendText(block.Text)
	case "thinking":
		s.appendThinking(block.Thinking, block.Signature)
	case "tool_use":
		args := strings.TrimSpace(string(block.Input))
		if args == "" {
			args = "{}"
		}
		s.outputs = append(s.outputs, ResponseOutput{
			ID:     block.ID,
			Type:   "function_call",
			Status: "completed",
			CallID: block.ID,
			Name:   block.Name,
			Args:   args,
		})
	}
}

func (s *anthropicStreamState) response() *Response {
	return &Response{
		ID:      s.responseID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   s.model,
		Status:  "completed",
		Output:  append([]ResponseOutput(nil), s.outputs...),
		Usage: Usage{
			PromptTokens:     s.promptTokens,
			CompletionTokens: s.completionTokens,
			TotalTokens:      s.promptTokens + s.completionTokens,
			CachedTokens:     s.cachedTokens,
		},
	}
}

func (p *anthropicProvider) buildParams(req *ResponseRequest) (map[string]any, error) {
	maxTokens := req.RequestedMaxTokens()
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	messages, system, err := p.buildMessages(req.InputMessages())
	if err != nil {
		return nil, err
	}

	params := map[string]any{
		"model":      req.Model,
		"max_tokens": maxTokens,
		"messages":   messages,
		"stream":     req.Stream,
	}

	if system != "" {
		params["system"] = system
	}

	// 添加工具
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			toolMap, ok := tool.(map[string]any)
			if !ok {
				continue
			}
			// 处理 OpenAI 格式: {type: "function", function: {name, description, parameters}}
			funcMap, ok := toolMap["function"].(map[string]any)
			if !ok {
				continue
			}
			name, _ := funcMap["name"].(string)
			description, _ := funcMap["description"].(string)
			parameters, _ := funcMap["parameters"].(map[string]any)

			tools = append(tools, map[string]any{
				"name":         name,
				"description":  description,
				"input_schema": parameters,
			})
		}
		if len(tools) > 0 {
			params["tools"] = tools
		}
	}

	return params, nil
}

func (p *anthropicProvider) buildMessages(msgs []Message) ([]map[string]any, string, error) {
	var systemParts []string
	messages := make([]map[string]any, 0, len(msgs))

	for _, msg := range msgs {
		text := collectText(msg.Content)
		blocks := buildAnthropicBlocks(msg)
		switch msg.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			// 处理 assistant 消息（含工具调用）
			content := make([]map[string]any, 0, len(blocks))
			for _, block := range blocks {
				content = append(content, anthropicBlockToMap(block))
			}
			if len(content) > 0 {
				messages = append(messages, map[string]any{"role": "assistant", "content": content})
			}
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":     text,
					},
				},
			})
		case "user":
			if len(blocks) > 0 {
				content := make([]map[string]any, 0, len(blocks))
				for _, block := range blocks {
					content = append(content, anthropicBlockToMap(block))
				}
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": content,
				})
			}
		default:
			if len(blocks) > 0 {
				content := make([]map[string]any, 0, len(blocks))
				for _, block := range blocks {
					content = append(content, anthropicBlockToMap(block))
				}
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": content,
				})
			}
		}
	}

	return messages, strings.Join(systemParts, "\n\n"), nil
}

func anthropicBlockToMap(block anthropicContentBlock) map[string]any {
	switch block.Type {
	case "text":
		return map[string]any{"type": "text", "text": block.Text}
	case "thinking":
		return map[string]any{"type": "text", "text": block.Text}
	case "image":
		var source map[string]any
		_ = json.Unmarshal(block.Input, &source)
		return map[string]any{
			"type":   "image",
			"source": source,
		}
	case "tool_use":
		return map[string]any{
			"type":  "tool_use",
			"id":    block.ID,
			"name":  block.Name,
			"input": mustUnmarshalJSON(string(block.Input)),
		}
	default:
		return map[string]any{"type": "text", "text": block.Text}
	}
}

// 辅助函数

func mustUnmarshalJSON(data string) map[string]any {
	var v map[string]any
	json.Unmarshal([]byte(data), &v)
	return v
}

// === 旧类型和函数（兼容测试）===

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

// buildAnthropicBlocks creates content blocks for Anthropic messages
func buildAnthropicBlocks(message Message) []anthropicContentBlock {
	blocks := make([]anthropicContentBlock, 0, len(message.ToolCalls)+1)
	for _, item := range message.Content {
		block, ok := buildAnthropicTextBlock(item)
		if ok {
			blocks = append(blocks, block)
		}
	}

	for _, call := range message.ToolCalls {
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    firstNonEmpty(call.ID, message.ToolCallID),
			Name:  call.Function.Name,
			Input: marshalRawJSON(call.Function.Arguments),
		})
	}
	return blocks
}

// buildAnthropicTextBlock converts a message content item to an Anthropic text block
func buildAnthropicTextBlock(value ContentBlock) (anthropicContentBlock, bool) {
	switch value.Type {
	case "text", "output_text":
		if value.Text == "" {
			return anthropicContentBlock{}, false
		}
		return anthropicContentBlock{Type: "text", Text: value.Text}, true
	case "thinking":
		if value.Thinking == "" {
			return anthropicContentBlock{}, false
		}
		return anthropicContentBlock{Type: "thinking", Text: value.Thinking}, true
	case "image":
		if value.Image == nil {
			return anthropicContentBlock{}, false
		}
		source := map[string]any{
			"type":       firstNonEmpty(value.Image.SourceType, "base64"),
			"media_type": value.Image.MediaType,
			"data":       value.Image.Data,
		}
		if value.Image.URL != "" {
			source = map[string]any{
				"type": "url",
				"url":  value.Image.URL,
			}
		}
		raw, _ := json.Marshal(source)
		return anthropicContentBlock{Type: "image", Input: raw}, true
	case "refusal":
		if value.Refusal == "" {
			return anthropicContentBlock{}, false
		}
		return anthropicContentBlock{Type: "text", Text: value.Refusal}, true
	case "structured_output":
		if value.Structured == nil {
			return anthropicContentBlock{}, false
		}
		if len(value.Structured.Raw) > 0 {
			return anthropicContentBlock{Type: "text", Text: string(value.Structured.Raw)}, true
		}
		if value.Structured.Data != nil {
			raw, _ := json.Marshal(value.Structured.Data)
			return anthropicContentBlock{Type: "text", Text: string(raw)}, true
		}
		return anthropicContentBlock{}, false
	default:
		return anthropicContentBlock{}, false
	}
}

// marshalRawJSON ensures JSON is properly formatted
func marshalRawJSON(arguments string) json.RawMessage {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return json.RawMessage(`{}`)
	}
	var payload json.RawMessage
	if json.Unmarshal([]byte(arguments), &payload) == nil {
		return payload
	}
	raw, _ := json.Marshal(arguments)
	return raw
}

// buildAnthropicStreamResponse builds a response for streaming
func buildAnthropicStreamResponse(id, model string, outputs []ResponseOutput, promptTokens, completionTokens int) *Response {
	if completionTokens == 0 {
		completionTokens = RoughTokenCount(renderOutputSignature(outputs))
	}
	return &Response{
		ID:      id,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  outputs,
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
			CachedTokens:     0,
		},
	}
}

// convertAnthropicOutputs converts Anthropic content blocks to our Output format
func convertAnthropicOutputs(role string, blocks []AnthropicContentBlock) []ResponseOutput {
	outputs := make([]ResponseOutput, 0, len(blocks))
	messageOutputIndex := -1
	for _, block := range blocks {
		switch block.Type {
		case "thinking":
			if messageOutputIndex < 0 {
				outputs = append(outputs, ResponseOutput{
					Type:   "message",
					Role:   role,
					Status: "completed",
				})
				messageOutputIndex = len(outputs) - 1
			}
			outputs[messageOutputIndex].Content = append(outputs[messageOutputIndex].Content, ResponseContent{
				Type:      "thinking",
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "text":
			if messageOutputIndex < 0 {
				outputs = append(outputs, ResponseOutput{
					Type:   "message",
					Role:   role,
					Status: "completed",
				})
				messageOutputIndex = len(outputs) - 1
			}
			outputs[messageOutputIndex].Content = append(outputs[messageOutputIndex].Content, ResponseContent{
				Type: "output_text",
				Text: block.Text,
			})
		case "tool_use":
			messageOutputIndex = -1
			outputs = append(outputs, ResponseOutput{
				ID:     block.ID,
				Type:   "function_call",
				Status: "completed",
				CallID: block.ID,
				Name:   block.Name,
				Args:   strings.TrimSpace(string(block.Input)),
			})
		}
	}
	return outputs
}

// renderOutputSignature renders output for token estimation
func renderOutputSignature(outputs []ResponseOutput) string {
	var builder strings.Builder
	for _, output := range outputs {
		if output.Type == "function_call" {
			builder.WriteString(output.Name)
			builder.WriteString(output.Args)
			continue
		}
		for _, content := range output.Content {
			builder.WriteString(content.Text)
		}
	}
	return builder.String()
}

// === 测试兼容的类型和函数 ===

type anthropicResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	Role       string `json:"role"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type      string           `json:"type"`
		Text      string           `json:"text"`
		ID        string           `json:"id"`
		Name      string           `json:"name"`
		Input     json.RawMessage  `json:"input"`
		Source    *AnthropicSource `json:"source"`
		Thinking  string           `json:"thinking"`
		Signature string           `json:"signature"`
	} `json:"content"`
	Usage struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheHitInputTokens int `json:"cache_hit_input_tokens"`
	} `json:"usage"`
}

// convertAnthropicResponse converts Anthropic response to our Response format
func convertAnthropicResponse(raw anthropicResponse, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = raw.Model
	}

	// Convert old response format to new format
	oldContent := raw.Content
	newContent := make([]AnthropicContentBlock, len(oldContent))
	for i, c := range oldContent {
		newContent[i] = AnthropicContentBlock{
			Type:      c.Type,
			Text:      c.Text,
			ID:        c.ID,
			Name:      c.Name,
			Input:     c.Input,
			Source:    c.Source,
			Thinking:  c.Thinking,
			Signature: c.Signature,
		}
	}

	return &Response{
		ID:      raw.ID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  convertAnthropicOutputs(raw.Role, newContent),
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
			CachedTokens:     raw.Usage.CacheHitInputTokens,
		},
	}
}

// buildRequest 是兼容测试的别名
func (p *anthropicProvider) buildRequest(req *ResponseRequest, stream bool) (map[string]any, error) {
	return p.buildParams(req)
}

// 确保实现接口（编译时检查）
var _ Provider = (*anthropicProvider)(nil)
