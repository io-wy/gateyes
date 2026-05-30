package provider

import (
	"encoding/json"
)

func cloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}

	result := make([]Message, len(messages))
	for i := range messages {
		result[i] = cloneMessage(messages[i])
	}
	return result
}

func cloneMessage(message Message) Message {
	message.Content = cloneContentBlocks(message.Content)
	if len(message.ToolCalls) > 0 {
		message.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
	}
	return message
}

func cloneContentBlocks(content []ContentBlock) []ContentBlock {
	if len(content) == 0 {
		return nil
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return content
	}
	var cloned []ContentBlock
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return content
	}
	return cloned
}

func CloneRequestOptions(value *RequestOptions) *RequestOptions {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Thinking != nil {
		thinking := *value.Thinking
		cloned.Thinking = &thinking
	}
	if value.CacheControl != nil {
		cacheControl := *value.CacheControl
		cloned.CacheControl = &cacheControl
	}
	cloned.Raw = cloneStringAnyMapLocal(value.Raw)
	return &cloned
}

func cloneStringAnyMapLocal(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		cloned := make(map[string]any, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		fallback := make(map[string]any, len(value))
		for key, item := range value {
			fallback[key] = item
		}
		return fallback
	}
	return cloned
}
