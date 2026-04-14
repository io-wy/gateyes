package provider

import (
	"encoding/json"
	"strings"
)

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
		content := buildChatCompletionMessageContent(msg.Content)
		if content == nil {
			continue
		}
		entry := map[string]any{
			"role":    role,
			"content": content,
		}
		if msg.ToolCallID != "" {
			entry["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			entry["tool_calls"] = msg.ToolCalls
		}
		result = append(result, entry)
	}
	return result
}

func buildOpenAIMessageContent(content []ContentBlock) []map[string]any {
	parts := make([]map[string]any, 0, len(content))
	for _, item := range content {
		part, ok := buildOpenAIContentPart(item)
		if ok {
			parts = append(parts, part)
		}
	}
	return parts
}

func buildChatCompletionMessageContent(content []ContentBlock) any {
	if len(content) == 0 {
		return ""
	}
	if hasImageBlocks(content) {
		parts := make([]map[string]any, 0, len(content))
		for _, block := range content {
			part, ok := buildChatCompletionContentPart(block)
			if ok {
				parts = append(parts, part)
			}
		}
		return parts
	}
	return collectText(content)
}

func buildOpenAIContentPart(value ContentBlock) (map[string]any, bool) {
	switch value.Type {
	case "text", "output_text":
		if value.Text == "" {
			return nil, false
		}
		return map[string]any{"type": "input_text", "text": value.Text}, true
	case "thinking":
		if value.Thinking == "" {
			return nil, false
		}
		return map[string]any{"type": "input_text", "text": value.Thinking}, true
	case "refusal":
		if value.Refusal == "" {
			return nil, false
		}
		return map[string]any{"type": "input_text", "text": value.Refusal}, true
	case "image":
		if value.Image == nil {
			return nil, false
		}
		if value.Image.URL != "" {
			return map[string]any{
				"type":      "input_image",
				"image_url": value.Image.URL,
			}, true
		}
		if value.Image.Data != "" {
			return map[string]any{
				"type":         "input_image",
				"image_base64": value.Image.Data,
			}, true
		}
	case "structured_output":
		if value.Structured != nil && value.Structured.Data != nil {
			raw, _ := json.Marshal(value.Structured.Data)
			return map[string]any{"type": "input_text", "text": string(raw)}, true
		}
	}
	return nil, false
}

func normalizeOpenAITextType(typeName string) string {
	switch typeName {
	case "text", "output_text":
		return "input_text"
	default:
		return typeName
	}
}

func buildChatCompletionContentPart(value ContentBlock) (map[string]any, bool) {
	switch value.Type {
	case "text", "output_text":
		if value.Text == "" {
			return nil, false
		}
		return map[string]any{"type": "text", "text": value.Text}, true
	case "image":
		if value.Image == nil || value.Image.URL == "" {
			return nil, false
		}
		imageURL := map[string]any{"url": value.Image.URL}
		if value.Image.Detail != "" {
			imageURL["detail"] = value.Image.Detail
		}
		return map[string]any{"type": "image_url", "image_url": imageURL}, true
	default:
		return nil, false
	}
}

func hasImageBlocks(content []ContentBlock) bool {
	for _, block := range content {
		if block.Type == "image" {
			return true
		}
	}
	return false
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
		CachedTokens int `json:"cached_tokens"`
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
		Index   int `json:"index"`
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
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
		Type      string `json:"type"`
		Text      string `json:"text"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
		Refusal   string `json:"refusal"`
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
			CachedTokens:     raw.Usage.CachedTokens,
		},
	}
}

func convertOpenAIOutputItem(item openAIOutputItem) *ResponseOutput {
	switch item.Type {
	case "message":
		content := make([]ResponseContent, 0, len(item.Content))
		for _, block := range item.Content {
			switch block.Type {
			case "thinking":
				if block.Thinking == "" {
					continue
				}
				content = append(content, ResponseContent{
					Type:      "thinking",
					Thinking:  block.Thinking,
					Signature: block.Signature,
				})
			case "refusal":
				if block.Refusal == "" {
					continue
				}
				content = append(content, ResponseContent{
					Type:    "refusal",
					Refusal: block.Refusal,
				})
			default:
				if block.Text == "" {
					continue
				}
				content = append(content, ResponseContent{
					Type: block.Type,
					Text: block.Text,
				})
			}
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
			CachedTokens:     raw.Usage.PromptTokensDetails.CachedTokens,
		},
	}
}
