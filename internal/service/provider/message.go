package provider

import (
	"encoding/json"
)

type Message struct {
	Role       string         `json:"role,omitempty"`
	Content    []ContentBlock `json:"content,omitempty"`
	Type       string         `json:"type,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Type       string          `json:"type"`
		Name       string          `json:"name"`
		ToolCallID string          `json:"tool_call_id"`
		ToolCalls  []ToolCall      `json:"tool_calls"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	m.Type = raw.Type
	m.Name = raw.Name
	m.ToolCallID = raw.ToolCallID
	m.ToolCalls = raw.ToolCalls
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		m.Content = nil
		return nil
	}
	var contentValue any
	if err := json.Unmarshal(raw.Content, &contentValue); err != nil {
		return err
	}
	m.Content = NormalizeMessageContent(contentValue)
	return nil
}
