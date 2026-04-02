package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

type openAIProvider struct {
	cfg    config.ProviderConfig
	client *http.Client
}

func NewOpenAIProvider(cfg config.ProviderConfig) Provider {
	return &openAIProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}
}

func (p *openAIProvider) Name() string      { return p.cfg.Name }
func (p *openAIProvider) Type() string      { return p.cfg.Type }
func (p *openAIProvider) BaseURL() string   { return p.cfg.BaseURL }
func (p *openAIProvider) Model() string     { return p.cfg.Model }
func (p *openAIProvider) UnitCost() float64 { return p.cfg.PriceInput + p.cfg.PriceOutput }
func (p *openAIProvider) Cost(prompt, completion int) float64 {
	return float64(prompt)*p.cfg.PriceInput + float64(completion)*p.cfg.PriceOutput
}

func (p *openAIProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	httpReq, err := p.newRequest(ctx, req, false)
	if err != nil {
		return nil, err
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(httpResp.Body)
		return nil, &UpstreamError{StatusCode: httpResp.StatusCode, Message: string(payload)}
	}

	// 读取原始响应体
	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	// 自动检测响应格式：按真实响应结构识别，不是按配置猜
	responseFormat := detectResponseFormat(bodyBytes)
	switch responseFormat {
	case "chat":
		var raw chatCompletionResponse
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			return nil, err
		}
		return convertChatCompletionResponse(raw, req.Model), nil
	case "responses":
		var raw openAIResponsePayload
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			return nil, err
		}
		return convertOpenAIResponse(raw, req.Model), nil
	default:
		// 兜底：尝试 responses API 格式
		var raw openAIResponsePayload
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			// 再试 chat 格式
			var chatRaw chatCompletionResponse
			if err2 := json.Unmarshal(bodyBytes, &chatRaw); err2 != nil {
				return nil, errors.New("unable to parse upstream response")
			}
			return convertChatCompletionResponse(chatRaw, req.Model), nil
		}
		return convertOpenAIResponse(raw, req.Model), nil
	}
}

func (p *openAIProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		httpReq, err := p.newRequest(ctx, req, true)
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			payload, _ := io.ReadAll(resp.Body)
			errCh <- &UpstreamError{StatusCode: resp.StatusCode, Message: string(payload)}
			return
		}

		// 改进的 SSE 解析器：支持完整 SSE frame 解析
		// 1. 累积多行 data chunk
		// 2. 检测并处理 chat vs responses 格式
		// 3. 处理 role-only/tool-call 增量等边界情况

		reader := bufio.NewReader(resp.Body)
		var pendingData string
		var detectedFormat string // "chat" 或 "responses"

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// 处理最后的 pending data
					if pendingData != "" {
						event, err := parseSSELine(pendingData, detectedFormat, req.Model)
						if err != nil {
							errCh <- err
							return
						}
						if event != nil {
							result <- *event
						}
					}
					return
				}
				errCh <- err
				return
			}

			line = strings.TrimSpace(line)

			// 处理 data: 前缀
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimPrefix(line, "data:")
				data = strings.TrimSpace(data)

				// [DONE] 需要特殊处理：不等待空行，直接结束
				if data == "[DONE]" {
					// 处理最后的 pending data
					if pendingData != "" {
						event, err := parseSSELine(pendingData, detectedFormat, req.Model)
						if err != nil {
							errCh <- err
							return
						}
						if event != nil {
							result <- *event
						}
					}
					return
				}

				// 检测到首个 data: 行时立即确定格式，避免首个事件发出时格式还未记录
				if pendingData == "" {
					detectedFormat = detectStreamFormat(data)
				}

				// 累积多行 data（某些 provider 会分片发送）
				if pendingData != "" {
					pendingData = pendingData + "\n" + data
				} else {
					pendingData = data
				}
			} else if line == "" && pendingData != "" {
				// 空行表示一个 SSE frame 结束
				event, err := parseSSELine(pendingData, detectedFormat, req.Model)
				if err != nil {
					errCh <- err
					return
				}
				if event != nil {
					result <- *event
				}
				pendingData = ""
			}
		}
	}()

	return result, errCh
}

// detectStreamFormat 从 SSE data line 检测流式响应格式
func detectStreamFormat(data string) string {
	// 精确检测：解析 JSON 看 type 字段
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &typeCheck); err == nil && typeCheck.Type != "" {
		// 如果有 type 字段，检查是否是 responses API 特有类型
		if strings.HasPrefix(typeCheck.Type, "response.") {
			return "responses"
		}
	}

	// 回退：检查是否包含 choices（chat completions）
	if strings.Contains(data, `"choices"`) {
		return "chat"
	}

	return "chat" // 默认 chat
}

// parseSSELine 解析单个 SSE data line
func parseSSELine(data, format, requestedModel string) (*ResponseEvent, error) {
	// 先尝试自动检测格式
	if format == "" {
		format = detectStreamFormat(data)
	}

	if format == "chat" || strings.Contains(data, `"choices"`) {
		event, err := parseChatCompletionEvent(data, requestedModel)
		if err != nil {
			// 容忍解析错误，返回 nil 而不是错误
			// 这样不规范的 provider 响应不会中断流
			return nil, nil
		}
		return event, nil
	}

	// 否则用 responses API 格式解析
	event, err := parseOpenAIStreamEvent(data, requestedModel)
	if err != nil {
		// 容忍解析错误
		return nil, nil
	}
	return event, nil
}

// parseOpenAIStreamEvent 解析 responses API 流式事件
func parseOpenAIStreamEvent(data string, requestedModel string) (*ResponseEvent, error) {
	// 先检测 type
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &typeCheck); err != nil {
		return nil, err
	}

	switch typeCheck.Type {
	case "response.output_text.delta":
		var payload struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		return &ResponseEvent{
			Type:  payload.Type,
			Delta: payload.Delta,
		}, nil
	case "response.output_item.done":
		var payload struct {
			Type       string           `json:"type"`
			Item       openAIOutputItem `json:"item"`
			OutputItem openAIOutputItem `json:"output_item"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		item := payload.Item
		if item.Type == "" {
			item = payload.OutputItem
		}
		output := convertOpenAIOutputItem(item)
		if output == nil {
			return nil, nil
		}
		return &ResponseEvent{
			Type:   payload.Type,
			Output: output,
		}, nil
	case "response.completed":
		var payload struct {
			Type     string                `json:"type"`
			Response openAIResponsePayload `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		resp := convertOpenAIResponse(payload.Response, requestedModel)
		return &ResponseEvent{
			Type:     payload.Type,
			Response: resp,
		}, nil
	case "response.failed":
		var payload struct {
			Response struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		if payload.Response.Error.Message == "" {
			payload.Response.Error.Message = "upstream response failed"
		}
		return nil, errors.New(payload.Response.Error.Message)
	default:
		// 兜底尝试 chat 格式
		return parseChatCompletionEvent(data, requestedModel)
	}
}

func (p *openAIProvider) newRequest(ctx context.Context, req *ResponseRequest, stream bool) (*http.Request, error) {
	// 根据 Endpoint 配置选择端点和请求格式
	endpoint := p.cfg.Endpoint
	if endpoint == "" {
		endpoint = "chat"
	}

	var path string
	var payload map[string]any

	switch endpoint {
	case "responses":
		path = "/responses"
		payload = map[string]any{
			"model":  req.Model,
			"input":  buildOpenAIInput(req.InputMessages()),
			"stream": stream,
		}
		if maxTokens := req.RequestedMaxTokens(); maxTokens > 0 {
			payload["max_output_tokens"] = maxTokens
		}
	case "chat": {
		path = "/v1/chat/completions"
		payload = map[string]any{
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
	}
	default:
		// 完整路径，默认使用 chat 格式
		path = endpoint
		payload = map[string]any{
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
	}

	body, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	return httpReq, nil
}

func buildOpenAIInput(messages []Message) []map[string]any {
	items := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.ToolCallID != "" {
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  collectText(message.Content),
			})
			continue
		}

		if content := buildOpenAIMessageContent(message.Content); len(content) > 0 {
			role := message.Role
			if role == "" {
				role = "user"
			}
			items = append(items, map[string]any{
				"role":    role,
				"content": content,
			})
		}

		for _, call := range message.ToolCalls {
			items = append(items, map[string]any{
				"type":      "function_call",
				"id":        call.ID,
				"call_id":   firstNonEmpty(call.ID, message.ToolCallID),
				"name":      call.Function.Name,
				"arguments": call.Function.Arguments,
			})
		}
	}
	return items
}

// buildChatCompletionMessages creates messages for Chat Completions API (simple format)
func buildChatCompletionMessages(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		content := collectText(msg.Content)
		result = append(result, map[string]any{
			"role":    role,
			"content": content,
		})
	}
	return result
}

func buildOpenAIMessageContent(content any) []map[string]any {
	switch current := content.(type) {
	case nil:
		return nil
	case string:
		if current == "" {
			return nil
		}
		return []map[string]any{{
			"type": "input_text",
			"text": current,
		}}
	case []any:
		parts := make([]map[string]any, 0, len(current))
		for _, item := range current {
			part, ok := buildOpenAIContentPart(item)
			if ok {
				parts = append(parts, part)
			}
		}
		return parts
	case map[string]any:
		part, ok := buildOpenAIContentPart(current)
		if !ok {
			return nil
		}
		return []map[string]any{part}
	default:
		text := collectText(current)
		if text == "" {
			return nil
		}
		return []map[string]any{{
			"type": "input_text",
			"text": text,
		}}
	}
}

func buildOpenAIContentPart(value any) (map[string]any, bool) {
	current, ok := value.(map[string]any)
	if !ok {
		text := collectText(value)
		if text == "" {
			return nil, false
		}
		return map[string]any{
			"type": "input_text",
			"text": text,
		}, true
	}

	typeName := firstNonEmpty(stringValue(current["type"]), "input_text")
	text := collectText(current)
	if text == "" {
		return nil, false
	}
	return map[string]any{
		"type": normalizeOpenAITextType(typeName),
		"text": text,
	}, true
}

func normalizeOpenAITextType(typeName string) string {
	switch typeName {
	case "text", "output_text":
		return "input_text"
	default:
		return typeName
	}
}

type openAIResponsePayload struct {
	ID        string             `json:"id"`
	Object    string             `json:"object"`
	CreatedAt int64              `json:"created_at"`
	Model     string             `json:"model"`
	Status    string             `json:"status"`
	Output    []openAIOutputItem `json:"output"`
	Usage     struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat Completions API response format
type chatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int `json:"index"`
		Message      struct {
			Role       string     `json:"role"`
			Content    string     `json:"content"`
			ToolCalls  []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIOutputItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// detectResponseFormat 检测响应格式：responses API vs chat completions
// 通过检查响应 body 的结构特征来识别格式
func detectResponseFormat(body []byte) string {
	// 快速检查：responses API 有 output 字段，chat completions 有 choices 字段
	var preview struct {
		Output  json.RawMessage `json:"output"`
		Choices json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		return "unknown"
	}

	// 如果有 output 字段且无 choices，是 responses API
	if len(preview.Output) > 0 && len(preview.Choices) == 0 {
		return "responses"
	}

	// 如果有 choices 字段且无 output，是 chat completions
	if len(preview.Choices) > 0 && len(preview.Output) == 0 {
		return "chat"
	}

	// 如果两者都没有，检查 object 字段的典型值
	var objCheck struct {
		Object string `json:"object"`
	}
	if json.Unmarshal(body, &objCheck) == nil {
		if strings.Contains(objCheck.Object, "chat.completion") {
			return "chat"
		}
		if strings.Contains(objCheck.Object, "response") {
			return "responses"
		}
	}

	// 最后的兜底：检查是否有 choices 数组（更精确）
	var choicesCheck struct {
		Choices []any `json:"choices"`
	}
	if json.Unmarshal(body, &choicesCheck) == nil && len(choicesCheck.Choices) > 0 {
		return "chat"
	}

	var outputCheck struct {
		Output []any `json:"output"`
	}
	if json.Unmarshal(body, &outputCheck) == nil && len(outputCheck.Output) > 0 {
		return "responses"
	}

	return "unknown"
}

// parseOpenAIResponseEvent 是 parseOpenAIStreamEvent 的别名，保留以兼容测试
func parseOpenAIResponseEvent(data string, requestedModel string) (*ResponseEvent, error) {
	return parseOpenAIStreamEvent(data, requestedModel)
}

func parseChatCompletionEvent(data string, requestedModel string) (*ResponseEvent, error) {
	var chunk struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int `json:"index"`
			Delta        struct {
				Content   string `json:"content"`
				Role      string `json:"role"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name string `json:"name"`
						Args string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
				FinishReason string `json:"finish_reason"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, err
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	delta := chunk.Choices[0].Delta
	// 跳过无意义事件：delta.content 为空、无 tool_calls、无 finish_reason、无 usage
	// 部分 provider（如 longcat）会发送大量空 delta，只在最后一条带内容
	// 过滤空事件可减少 SSE 噪音，也避免客户端误认为流已结束
	if delta.Content == "" && len(delta.ToolCalls) == 0 && delta.FinishReason == "" && chunk.Choices[0].FinishReason == "" && chunk.Usage.TotalTokens == 0 {
		return nil, nil
	}

	event := ResponseEvent{
		Type: "chat.delta",
		Delta: delta.Content,
	}

	if len(delta.ToolCalls) > 0 {
		for _, tc := range delta.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, ToolCall{
				ID: tc.ID,
				Function: FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Args,
				},
			})
		}
	}

	if delta.FinishReason != "" {
		event.FinishReason = delta.FinishReason
	} else if chunk.Choices[0].FinishReason != "" {
		event.FinishReason = chunk.Choices[0].FinishReason
	}

	if chunk.Usage.TotalTokens > 0 {
		event.Usage = &Usage{
			PromptTokens:     chunk.Usage.PromptTokens,
			CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens:      chunk.Usage.TotalTokens,
		}
	}

	return &event, nil
}

func convertOpenAIResponse(raw openAIResponsePayload, requestedModel string) *Response {
	output := make([]ResponseOutput, 0, len(raw.Output))
	for _, item := range raw.Output {
		converted := convertOpenAIOutputItem(item)
		if converted == nil {
			continue
		}
		output = append(output, *converted)
	}

	model := requestedModel
	if model == "" {
		model = raw.Model
	}

	return &Response{
		ID:      raw.ID,
		Object:  "response",
		Created: raw.CreatedAt,
		Model:   model,
		Status:  raw.Status,
		Output:  output,
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		},
	}
}

func convertOpenAIOutputItem(item openAIOutputItem) *ResponseOutput {
	switch item.Type {
	case "message":
		content := make([]ResponseContent, 0, len(item.Content))
		for _, block := range item.Content {
			if block.Text == "" {
				continue
			}
			content = append(content, ResponseContent{
				Type: block.Type,
				Text: block.Text,
			})
		}
		return &ResponseOutput{
			ID:      item.ID,
			Type:    item.Type,
			Role:    item.Role,
			Status:  item.Status,
			Content: content,
		}
	case "function_call":
		return &ResponseOutput{
			ID:     item.ID,
			Type:   item.Type,
			Status: item.Status,
			CallID: firstNonEmpty(item.CallID, item.ID),
			Name:   item.Name,
			Args:   item.Arguments,
		}
	default:
		return nil
	}
}

func convertChatCompletionResponse(raw chatCompletionResponse, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = raw.Model
	}

	var output []ResponseOutput
	if len(raw.Choices) > 0 {
		msg := raw.Choices[0].Message

		// Handle tool_calls
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				output = append(output, ResponseOutput{
					Type:   "function_call",
					CallID: tc.ID,
					Name:   tc.Function.Name,
					Args:   tc.Function.Arguments,
				})
			}
		}

		// Handle content
		if msg.Content != "" {
			output = append(output, ResponseOutput{
				Type: "message",
				Content: []ResponseContent{
					{Type: "output_text", Text: msg.Content},
				},
			})
		}
	}

	return &Response{
		ID:      raw.ID,
		Object:  "chat.completion",
		Created: raw.Created,
		Model:   model,
		Output:  output,
		Usage: Usage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		},
	}
}
