package provider

import (
	"encoding/json"
	"strings"
)

type AnthropicMessagesRequest struct {
	Model         string                 `json:"model"`
	Messages      []AnthropicMessage     `json:"messages"`
	System        any                    `json:"system,omitempty"`
	MaxTokens     int                    `json:"max_tokens,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
	Tools         []AnthropicTool        `json:"tools,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Temperature   float64                `json:"temperature,omitempty"`
	TopK          int                    `json:"top_k,omitempty"`
	TopP          float64                `json:"top_p,omitempty"`
	Thinking      *AnthropicThinking     `json:"thinking,omitempty"`
	CacheControl  *AnthropicCacheControl `json:"cache_control,omitempty"`
}

type AnthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type AnthropicCacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

func (m *AnthropicMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	if len(raw.Content) == 0 {
		return nil
	}
	if raw.Content[0] == '"' {
		var s string
		if err := json.Unmarshal(raw.Content, &s); err != nil {
			return err
		}
		m.Content = []AnthropicContentBlock{{Type: "text", Text: s}}
		return nil
	}
	return json.Unmarshal(raw.Content, &m.Content)
}

type AnthropicContentBlock struct {
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Input     json.RawMessage  `json:"input,omitempty"`
	ToolUseID string           `json:"tool_use_id,omitempty"`
	Content   string           `json:"content,omitempty"`
	Source    *AnthropicSource `json:"source,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	Signature string           `json:"signature,omitempty"`
}

type AnthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type AnthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type AnthropicMessagesResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicEvent struct {
	Type    string                     `json:"type"`
	Index   int                        `json:"index,omitempty"`
	Delta   any                        `json:"delta,omitempty"`
	Content []AnthropicContentBlock    `json:"content,omitempty"`
	Block   *AnthropicContentBlock     `json:"block,omitempty"`
	Message *AnthropicMessagesResponse `json:"message,omitempty"`
}

func ConvertAnthropicRequest(req *AnthropicMessagesRequest) *ResponseRequest {
	if req == nil {
		return nil
	}

	messages := convertAnthropicMessages(req.Messages)
	var tools []any
	for _, tool := range req.Tools {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}

	return &ResponseRequest{
		Model:     req.Model,
		Surface:   "messages",
		Input:     messages,
		Messages:  messages,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
		Tools:     tools,
		Options: &RequestOptions{
			System:       convertAnthropicSystem(req.System),
			Thinking:     req.Thinking,
			CacheControl: req.CacheControl,
		},
	}
}

func ConvertResponseToAnthropic(resp *Response) *AnthropicMessagesResponse {
	if resp == nil {
		return nil
	}

	stopReason := "end_turn"
	if len(resp.OutputToolCalls()) > 0 {
		stopReason = "tool_use"
	}

	return &AnthropicMessagesResponse{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    convertResponseToAnthropicContent(resp.Output),
		Model:      resp.Model,
		StopReason: stopReason,
		Usage: AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

func convertAnthropicSystem(system any) string {
	switch s := system.(type) {
	case string:
		return s
	case []any:
		var parts []string
		for _, item := range s {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func convertAnthropicMessages(msgs []AnthropicMessage) []Message {
	result := make([]Message, 0, len(msgs))
	for _, msg := range msgs {
		content := make([]ContentBlock, 0, len(msg.Content))
		toolCalls := make([]ToolCall, 0)
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_use":
				inputMap := make(map[string]any)
				if len(block.Input) > 0 {
					_ = json.Unmarshal(block.Input, &inputMap)
				}
				raw, _ := json.Marshal(inputMap)
				toolCalls = append(toolCalls, ToolCall{
					ID:   block.ID,
					Type: "function",
					Function: FunctionCall{
						Name:      block.Name,
						Arguments: string(raw),
					},
				})
			case "tool_result":
				result = append(result, Message{
					Role:       "tool",
					ToolCallID: block.ToolUseID,
					Content:    TextBlocks(firstNonEmpty(block.Content, block.Text)),
				})
			default:
				content = append(content, convertAnthropicBlock(block)...)
			}
		}
		if len(content) == 0 && len(toolCalls) == 0 {
			continue
		}
		result = append(result, Message{Role: msg.Role, Content: content, ToolCalls: toolCalls})
	}
	return result
}

func convertAnthropicBlock(block AnthropicContentBlock) []ContentBlock {
	switch block.Type {
	case "text":
		return TextBlocks(block.Text)
	case "thinking":
		return []ContentBlock{{
			Type:      "thinking",
			Thinking:  block.Thinking,
			Signature: block.Signature,
		}}
	case "image":
		if block.Source == nil {
			return nil
		}
		return []ContentBlock{{
			Type: "image",
			Image: &ContentImage{
				SourceType: block.Source.Type,
				MediaType:  block.Source.MediaType,
				Data:       block.Source.Data,
			},
		}}
	default:
		return nil
	}
}

func convertResponseToAnthropicContent(outputs []ResponseOutput) []AnthropicContentBlock {
	blocks := make([]AnthropicContentBlock, 0)
	for _, output := range outputs {
		switch output.Type {
		case "message":
			for _, content := range output.Content {
				switch content.Type {
				case "thinking":
					blocks = append(blocks, AnthropicContentBlock{
						Type:      "thinking",
						Thinking:  content.Thinking,
						Signature: content.Signature,
					})
				case "output_text":
					if content.Text != "" {
						blocks = append(blocks, AnthropicContentBlock{
							Type: "text",
							Text: content.Text,
						})
					}
				}
			}
		case "function_call":
			inputMap := make(map[string]any)
			if len(output.Args) > 0 {
				_ = json.Unmarshal([]byte(output.Args), &inputMap)
			}
			inputBytes, _ := json.Marshal(inputMap)
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    firstNonEmpty(output.ID, output.CallID),
				Name:  output.Name,
				Input: inputBytes,
			})
		}
	}
	return blocks
}
