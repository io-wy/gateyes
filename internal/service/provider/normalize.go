package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---- content normalization ----

func TextBlocks(text string) []ContentBlock {
	if text == "" {
		return nil
	}
	return []ContentBlock{{Type: "text", Text: text}}
}

func NormalizeMessageContent(value any) []ContentBlock {
	return normalizeContentBlocks(value)
}

func normalizeMessages(input any) []Message {
	switch value := input.(type) {
	case nil:
		return nil
	case string:
		return []Message{{Role: "user", Content: TextBlocks(value)}}
	case []Message:
		return cloneMessages(value)
	case Message:
		return []Message{cloneMessage(value)}
	case []any:
		messages := make([]Message, 0, len(value))
		for _, item := range value {
			messages = append(messages, normalizeMessages(item)...)
		}
		return messages
	case map[string]any:
		msg, ok := normalizeMessageMap(value)
		if !ok {
			return nil
		}
		return []Message{msg}
	default:
		return nil
	}
}

func normalizeMessageMap(value map[string]any) (Message, bool) {
	messageType, _ := value["type"].(string)
	switch messageType {
	case "function_call_output":
		callID, _ := value["call_id"].(string)
		content := value["output"]
		if content == nil {
			content = value["content"]
		}
		return Message{
			Role:       "tool",
			Type:       messageType,
			ToolCallID: callID,
			Content:    normalizeContentBlocks(content),
		}, callID != "" || content != nil
	case "function_call":
		return Message{
			Role: "assistant",
			Type: messageType,
			ToolCalls: []ToolCall{{
				ID:   stringValue(value["id"]),
				Type: "function",
				Function: FunctionCall{
					Name:      stringValue(value["name"]),
					Arguments: stringValue(value["arguments"]),
				},
			}},
		}, true
	}

	role, _ := value["role"].(string)
	if role == "" {
		role = "user"
	}

	message := Message{
		Role:       role,
		Type:       messageType,
		Name:       stringValue(value["name"]),
		ToolCallID: stringValue(value["tool_call_id"]),
		Content:    normalizeContentBlocks(value["content"]),
		ToolCalls:  normalizeToolCalls(value["tool_calls"]),
	}
	if len(message.Content) == 0 {
		if text := firstNonEmpty(collectText(value["text"]), collectText(value["input_text"])); text != "" {
			message.Content = TextBlocks(text)
		}
	}
	if len(message.Content) == 0 && len(message.ToolCalls) == 0 && message.ToolCallID == "" {
		return Message{}, false
	}
	return message, true
}

func normalizeContent(value any) any {
	return normalizeContentBlocks(value)
}

func normalizeContentBlocks(value any) []ContentBlock {
	switch current := value.(type) {
	case nil:
		return nil
	case string:
		return TextBlocks(current)
	case []ContentBlock:
		if len(current) == 0 {
			return nil
		}
		return append([]ContentBlock(nil), current...)
	case []ResponseContent:
		result := make([]ContentBlock, 0, len(current))
		for _, item := range current {
			result = append(result, responseContentToBlocks(item)...)
		}
		return result
	case []any:
		result := make([]ContentBlock, 0, len(current))
		for _, item := range current {
			result = append(result, normalizeContentBlocks(item)...)
		}
		return result
	case map[string]any:
		return normalizeContentBlockMap(current)
	default:
		text := collectText(current)
		return TextBlocks(text)
	}
}

func normalizeContentBlockMap(current map[string]any) []ContentBlock {
	typeName := firstNonEmpty(stringValue(current["type"]), "text")
	switch typeName {
	case "text", "input_text", "output_text":
		text := firstNonEmpty(stringValue(current["text"]), collectText(current["content"]), collectText(current["input_text"]))
		return TextBlocks(text)
	case "thinking":
		thinking := firstNonEmpty(stringValue(current["thinking"]), stringValue(current["text"]))
		if thinking == "" {
			return nil
		}
		return []ContentBlock{{
			Type:      "thinking",
			Thinking:  thinking,
			Signature: stringValue(current["signature"]),
		}}
	case "refusal":
		refusal := firstNonEmpty(stringValue(current["refusal"]), stringValue(current["text"]))
		if refusal == "" {
			return nil
		}
		return []ContentBlock{{Type: "refusal", Refusal: refusal}}
	case "image", "image_url":
		image := normalizeImageBlock(current)
		if image == nil {
			return nil
		}
		return []ContentBlock{{Type: "image", Image: image}}
	case "structured_output", "json":
		structured := normalizeStructuredContent(current)
		if structured == nil {
			return nil
		}
		return []ContentBlock{{Type: "structured_output", Structured: structured}}
	default:
		text := collectText(current)
		return TextBlocks(text)
	}
}

func responseContentToBlocks(item ResponseContent) []ContentBlock {
	switch item.Type {
	case "thinking":
		if item.Thinking == "" {
			return nil
		}
		return []ContentBlock{{
			Type:      "thinking",
			Thinking:  item.Thinking,
			Signature: item.Signature,
		}}
	case "output_text", "text":
		return TextBlocks(item.Text)
	case "refusal":
		if item.Text == "" {
			return nil
		}
		return []ContentBlock{{Type: "refusal", Refusal: item.Text}}
	default:
		return TextBlocks(item.Text)
	}
}

func normalizeImageBlock(current map[string]any) *ContentImage {
	if imageURL, ok := current["image_url"].(map[string]any); ok {
		return &ContentImage{
			SourceType: "url",
			URL:        stringValue(imageURL["url"]),
			Detail:     stringValue(imageURL["detail"]),
		}
	}
	if source, ok := current["source"].(map[string]any); ok {
		return &ContentImage{
			SourceType: stringValue(source["type"]),
			URL:        stringValue(source["url"]),
			MediaType:  stringValue(source["media_type"]),
			Data:       stringValue(source["data"]),
		}
	}
	return nil
}

func normalizeStructuredContent(current map[string]any) *StructuredContent {
	structured := &StructuredContent{
		Format: stringValue(current["format"]),
	}
	if structured.Format == "" {
		structured.Format = "json"
	}
	if data, ok := current["data"].(map[string]any); ok {
		structured.Data = data
	}
	if raw, ok := current["raw"].(string); ok && raw != "" {
		structured.Raw = json.RawMessage(raw)
	}
	return structured
}

func normalizeToolCalls(value any) []ToolCall {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]ToolCall, 0, len(list))
	for _, item := range list {
		current, ok := item.(map[string]any)
		if !ok {
			continue
		}
		call := ToolCall{
			ID:   stringValue(current["id"]),
			Type: firstNonEmpty(stringValue(current["type"]), "function"),
		}
		if fn, ok := current["function"].(map[string]any); ok {
			call.Function = FunctionCall{
				Name:      stringValue(fn["name"]),
				Arguments: stringValue(fn["arguments"]),
			}
		}
		if call.Function.Name == "" && call.Function.Arguments == "" && call.ID == "" {
			continue
		}
		result = append(result, call)
	}
	return result
}

// ---- text extraction ----

func collectText(value any) string {
	switch current := value.(type) {
	case nil:
		return ""
	case string:
		return current
	case []ContentBlock:
		parts := make([]string, 0, len(current))
		for _, block := range current {
			switch block.Type {
			case "text", "output_text":
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			case "thinking":
				if block.Thinking != "" {
					parts = append(parts, block.Thinking)
				}
			case "refusal":
				if block.Refusal != "" {
					parts = append(parts, block.Refusal)
				}
			}
		}
		return strings.Join(parts, "")
	case []any:
		parts := make([]string, 0, len(current))
		for _, item := range current {
			text := collectText(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if typeName, _ := current["type"].(string); isToolLikeType(typeName) {
			return ""
		}
		if text, ok := current["text"].(string); ok && text != "" {
			return text
		}
		if value, ok := current["content"]; ok {
			return collectText(value)
		}
		if value, ok := current["input_text"]; ok {
			return collectText(value)
		}
		return ""
	default:
		return fmt.Sprint(current)
	}
}

func isToolLikeType(typeName string) bool {
	switch typeName {
	case "function_call", "function_call_output", "tool_use", "tool_result":
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Message.Signature returns a canonical representation for token estimation.
func (m Message) Signature() string {
	var b strings.Builder
	if text := collectText(m.Content); text != "" {
		b.WriteString(text)
	}
	if m.ToolCallID != "" {
		b.WriteString("|tool_result:")
		b.WriteString(m.ToolCallID)
	}
	for _, call := range m.ToolCalls {
		b.WriteString("|tool_call:")
		b.WriteString(call.ID)
		b.WriteString(":")
		b.WriteString(call.Function.Name)
		b.WriteString(call.Function.Arguments)
	}
	return b.String()
}
