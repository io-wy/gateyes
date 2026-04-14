package provider

import (
	"encoding/json"
	"strings"
)

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
	if req.Options != nil {
		for key, value := range req.Options.Raw {
			params[key] = value
		}
		if req.Options.System != "" {
			params["system"] = req.Options.System
		}
		if req.Options.Thinking != nil {
			params["thinking"] = req.Options.Thinking
		}
		if req.Options.CacheControl != nil {
			params["cache_control"] = req.Options.CacheControl
		}
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			toolMap, ok := tool.(map[string]any)
			if !ok {
				continue
			}
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

	applyProviderPayloadProfile(p.cfg, params)
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
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": msg.ToolCallID,
					"content":     text,
				}},
			})
		case "user":
			fallthrough
		default:
			if len(blocks) == 0 {
				continue
			}
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

func mustUnmarshalJSON(data string) map[string]any {
	var value map[string]any
	_ = json.Unmarshal([]byte(data), &value)
	return value
}

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

func (p *anthropicProvider) buildRequest(req *ResponseRequest, stream bool) (map[string]any, error) {
	return p.buildParams(req)
}
