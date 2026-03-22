package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/gateyes/gateway/internal/config"
)

type anthropicProvider struct {
	cfg    config.ProviderConfig
	client anthropic.Client
}

func NewAnthropicProvider(cfg config.ProviderConfig) Provider {
	// 使用 SDK 创建 client，支持自定义 baseURL
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
	)
	return &anthropicProvider{
		cfg:    cfg,
		client: client,
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

	message, err := p.client.Messages.New(ctx, *params)
	if err != nil {
		return nil, fmt.Errorf("anthropic error: %w", err)
	}

	return p.convertResponse(message, req.Model), nil
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

		stream := p.client.Messages.NewStreaming(ctx, *params)
		var message anthropic.Message

		for stream.Next() {
			event := stream.Current()
			err := message.Accumulate(event)
			if err != nil {
				errCh <- err
				return
			}

			// 发送中间事件 - 通过 ContentBlockDeltaEvent 处理文本增量
			if textEvent := p.extractTextDelta(event); textEvent != "" {
				result <- ResponseEvent{
					Type:  "response.output_text.delta",
					Delta: textEvent,
				}
			}

			// 工具调用开始
			if toolEvent := p.extractToolStart(event); toolEvent != nil {
				result <- ResponseEvent{
					Type:   "response.output_item.done",
					Output: toolEvent,
				}
			}
		}

		if err := stream.Err(); err != nil {
			errCh <- fmt.Errorf("anthropic stream error: %w", err)
			return
		}

		// 发送完成事件
		result <- ResponseEvent{
			Type:     "response.completed",
			Response: p.convertResponse(&message, req.Model),
		}
	}()

	return result, errCh
}

func (p *anthropicProvider) buildParams(req *ResponseRequest) (*anthropic.MessageNewParams, error) {
	return p.buildRequest(req, false)
}

// buildRequest builds an Anthropic request (for backward compatibility with tests)
func (p *anthropicProvider) buildRequest(req *ResponseRequest, stream bool) (*anthropic.MessageNewParams, error) {
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

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	if system != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: system, Type: "text"},
		}
	}

	// 添加工具
	if len(req.Tools) > 0 {
		tools := make([]anthropic.ToolUnionParam, len(req.Tools))
		for i, tool := range req.Tools {
			toolMap, ok := tool.(map[string]any)
			if !ok {
				continue
			}
			name, _ := toolMap["name"].(string)
			description, _ := toolMap["description"].(string)
			parameters, _ := toolMap["parameters"].(map[string]any)

			tools[i] = anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        name,
					Description: anthropic.String(description),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: parameters,
						Type:       "object",
					},
				},
			}
		}
	}
	// 兼容 OpenAI 格式：tools[].function.parameters
	if len(req.Tools) > 0 && len(tools) == 0 {
		tools = make([]anthropic.ToolUnionParam, 0, len(req.Tools))
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

			tools = append(tools, anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        name,
					Description: anthropic.String(description),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: parameters,
						Type:       "object",
					},
				},
			})
		}
	}
	params.Tools = tools
	}

	return &params, nil
}

func (p *anthropicProvider) buildMessages(msgs []Message) ([]anthropic.MessageParam, string, error) {
	var systemParts []string
	messages := make([]anthropic.MessageParam, 0, len(msgs))

	for _, msg := range msgs {
		text := collectText(msg.Content)
		switch msg.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			// 处理 assistant 消息（含工具调用）
			var blocks []anthropic.ContentBlockParamUnion
			if text != "" {
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfText: &anthropic.TextBlockParam{Text: text, Type: "text"},
				})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Input: func() any {
							var m map[string]any
							json.Unmarshal([]byte(tc.Function.Arguments), &m)
							return m
						}(),
						Type: "tool_use",
					},
				})
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.ContentBlockParamUnion{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: msg.ToolCallID,
						Content: []anthropic.ToolResultBlockParamContentUnion{
							{OfText: &anthropic.TextBlockParam{Text: text, Type: "text"}},
						},
						Type: "tool_result",
					},
				},
			))
		case "user":
			if text != "" {
				messages = append(messages, anthropic.NewUserMessage(
					anthropic.ContentBlockParamUnion{
						OfText: &anthropic.TextBlockParam{Text: text, Type: "text"},
					},
				))
			}
		default:
			if text != "" {
				messages = append(messages, anthropic.NewUserMessage(
					anthropic.ContentBlockParamUnion{
						OfText: &anthropic.TextBlockParam{Text: text, Type: "text"},
					},
				))
			}
		}
	}

	return messages, strings.Join(systemParts, "\n\n"), nil
}

func (p *anthropicProvider) convertResponse(msg *anthropic.Message, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = string(msg.Model)
	}

	outputs := make([]ResponseOutput, 0)
	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			outputs = append(outputs, ResponseOutput{
				Type: "message",
				Content: []ResponseContent{
					{Type: "output_text", Text: v.Text},
				},
			})
		case anthropic.ToolUseBlock:
			var argsMap map[string]any
			json.Unmarshal(v.Input, &argsMap)
			inputJSON, _ := json.Marshal(argsMap["input"])
			outputs = append(outputs, ResponseOutput{
				ID:     v.ID,
				Type:   "function_call",
				Name:   v.Name,
				Args:   string(inputJSON),
				Status: "completed",
			})
		}
	}

	return &Response{
		ID:      msg.ID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  outputs,
		Usage: Usage{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
	}
}

// 从流事件中提取文本增量
func (p *anthropicProvider) extractTextDelta(event anthropic.MessageStreamEventUnion) string {
	switch e := event.AsAny().(type) {
	case anthropic.ContentBlockDeltaEvent:
		// 处理文本增量
		switch delta := e.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			return delta.Text
		}
	}
	return ""
}

// 从流事件中提取工具调用开始
func (p *anthropicProvider) extractToolStart(event anthropic.MessageStreamEventUnion) *ResponseOutput {
	switch e := event.AsAny().(type) {
	case anthropic.ContentBlockStartEvent:
		if e.ContentBlock.Type == "tool_use" {
			return &ResponseOutput{
				ID:     e.ContentBlock.ID,
				Type:   "function_call",
				Name:   e.ContentBlock.Name,
				Status: "in_progress",
			}
		}
	}
	return nil
}

// === 以下为测试兼容的辅助函数 ===

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
func convertAnthropicOutputs(role string, blocks []struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}) []ResponseOutput {
	outputs := make([]ResponseOutput, 0, len(blocks))
	messageOutputIndex := -1
	for _, block := range blocks {
		switch block.Type {
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

	return &Response{
		ID:      raw.ID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  convertAnthropicOutputs(raw.Role, raw.Content),
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
		},
	}
}

// 确保实现接口（编译时检查）
var _ Provider = (*anthropicProvider)(nil)
