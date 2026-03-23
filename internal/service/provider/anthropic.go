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

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Send completion event if not already sent
				result <- ResponseEvent{
					Type: "response.completed",
					Response: &Response{
						ID:      responseID,
						Model:   model,
						Usage:   Usage{},
					},
				}
				break
			}
			errCh <- err
			return
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		// Check for error event
		if strings.Contains(data, "\"type\":\"error\"") || strings.Contains(data, "\"error\":") {
			var errResp struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &errResp) == nil && errResp.Error.Message != "" {
				// stream 错误事件无法确定状态码，视为不可重试
				errCh <- &UpstreamError{StatusCode: 0, Message: errResp.Error.Message}
				return
			}
		}

		event := parseAnthropicStreamEvent(data, model, responseID)
		if event != nil {
			result <- *event
		}
	}
}

// uuid generates a simple UUID-like string
func uuid() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func parseAnthropicStreamEvent(data string, model string, responseID string) *ResponseEvent {
	// Parse Anthropic SSE event format
	// Event types: message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop, ping

	var event struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil
	}

	switch event.Type {
	case "message_start":
		var payload struct {
			Type    string `json:"type"`
			Message struct {
				ID      string `json:"id"`
				Content []AnthropicContentBlock `json:"content"`
				Usage   struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		return &ResponseEvent{
			Type: "response.created",
			Response: &Response{
				ID:      payload.Message.ID,
				Model:   model,
				Output:  convertAnthropicOutputs("assistant", payload.Message.Content),
				Usage:   Usage{PromptTokens: payload.Message.Usage.InputTokens},
			},
		}

	case "content_block_start":
		// MiniMax may send this
		return &ResponseEvent{
			Type: "response.created",
			Response: &Response{
				ID:      responseID,
				Model:   model,
				Output:  []ResponseOutput{},
			},
		}

	case "content_block_delta":
		var payload struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta any    `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}

		// Handle different delta formats
		if deltaMap, ok := payload.Delta.(map[string]any); ok {
			if deltaType, _ := deltaMap["type"].(string); deltaType == "text_delta" {
				if text, _ := deltaMap["text"].(string); text != "" {
					return &ResponseEvent{
						Type:  "response.output_text.delta",
						Delta: text,
					}
				}
			}
			// Fallback: try to get text directly
			if text, _ := deltaMap["text"].(string); text != "" {
				return &ResponseEvent{
					Type:  "response.output_text.delta",
					Delta: text,
				}
			}
		}

	case "content_block_stop":
		return &ResponseEvent{
			Type: "response.output_item.done",
		}

	case "message_delta":
		var payload struct {
			Type       string `json:"type"`
			Delta      any    `json:"delta"`
			StopReason string `json:"stop_reason"`
			Usage      struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			// Try minimal parsing
			return &ResponseEvent{
				Type: "response.completed",
				Response: &Response{
					ID:      responseID,
					Model:   model,
					Usage:   Usage{},
				},
			}
		}
		return &ResponseEvent{
			Type: "response.completed",
			Response: &Response{
				ID:      responseID,
				Model:   model,
				Usage: Usage{
					CompletionTokens: payload.Usage.OutputTokens,
				},
			},
		}

	case "message_stop":
		// End of stream - send completion
		return &ResponseEvent{
			Type: "response.completed",
			Response: &Response{
				ID:      responseID,
				Model:   model,
				Usage:   Usage{},
			},
		}

	case "ping":
		// Ignore ping events
		return nil
	}

	return nil
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
		"model":     req.Model,
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
				"name":        name,
				"description": description,
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
		switch msg.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			// 处理 assistant 消息（含工具调用）
			var content []map[string]any
			if text != "" {
				content = append(content, map[string]any{"type": "text", "text": text})
			}
			for _, tc := range msg.ToolCalls {
				// 直接使用原始参数，Anthropic 格式的 input 是对象
				inputMap := mustUnmarshalJSON(tc.Function.Arguments)
				content = append(content, map[string]any{
					"type": "tool_use",
					"id":   tc.ID,
					"name": tc.Function.Name,
					"input": inputMap,
				})
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
						"type":      "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":    text,
					},
				},
			})
		case "user":
			if text != "" {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": text,
				})
			}
		default:
			if text != "" {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": text,
				})
			}
		}
	}

	return messages, strings.Join(systemParts, "\n\n"), nil
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
	switch current := message.Content.(type) {
	case string:
		if current != "" {
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: current})
		}
	case []any:
		for _, item := range current {
			block, ok := buildAnthropicTextBlock(item)
			if ok {
				blocks = append(blocks, block)
			}
		}
	case map[string]any:
		block, ok := buildAnthropicTextBlock(current)
		if ok {
			blocks = append(blocks, block)
		}
	default:
		if text := collectText(current); text != "" {
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: text})
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
func buildAnthropicTextBlock(value any) (anthropicContentBlock, bool) {
	current, ok := value.(map[string]any)
	if !ok {
		text := collectText(value)
		if text == "" {
			return anthropicContentBlock{}, false
		}
		return anthropicContentBlock{Type: "text", Text: text}, true
	}
	text := collectText(current)
	if text == "" {
		return anthropicContentBlock{}, false
	}
	return anthropicContentBlock{Type: "text", Text: text}, true
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
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
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
			Type:  c.Type,
			Text:  c.Text,
			ID:    c.ID,
			Name:  c.Name,
			Input: c.Input,
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
		},
	}
}

// buildRequest 是兼容测试的别名
func (p *anthropicProvider) buildRequest(req *ResponseRequest, stream bool) (map[string]any, error) {
	return p.buildParams(req)
}

// 确保实现接口（编译时检查）
var _ Provider = (*anthropicProvider)(nil)
