package apicompat

import (
	"encoding/json"
	"strings"

	"github.com/gateyes/gateway/internal/service/provider"
)

func ConvertAnthropicRequest(req *AnthropicMessagesRequest) *provider.ResponseRequest {
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

	return &provider.ResponseRequest{
		Model:     req.Model,
		Input:     messages,
		Messages:  messages,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
		Tools:     tools,
		Extra: map[string]any{
			"system":        convertAnthropicSystem(req.System),
			"thinking":      req.Thinking,
			"cache_control": req.CacheControl,
		},
	}
}

func ConvertResponseToAnthropic(resp *provider.Response) *AnthropicMessagesResponse {
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

type AnthropicStreamEncoder struct {
	responseID  string
	model       string
	started     bool
	activeIndex int
	nextIndex   int
}

func NewAnthropicStreamEncoder(responseID, model string) *AnthropicStreamEncoder {
	return &AnthropicStreamEncoder{
		responseID:  responseID,
		model:       model,
		activeIndex: -1,
	}
}

func (e *AnthropicStreamEncoder) Encode(event provider.ResponseEvent) []*AnthropicEvent {
	if e == nil {
		return nil
	}

	switch event.Type {
	case provider.EventResponseStarted:
		e.started = true
		return []*AnthropicEvent{{
			Type: "message_start",
			Message: &AnthropicMessagesResponse{
				ID:      e.responseID,
				Type:    "message",
				Role:    "assistant",
				Model:   e.model,
				Content: []AnthropicContentBlock{},
				Usage: AnthropicUsage{
					InputTokens:  responsePromptTokens(event.Response),
					OutputTokens: 0,
				},
			},
		}}
	case provider.EventContentDelta:
		result := e.ensureMessageStart(nil)
		result = append(result, e.ensureTextBlockStart()...)
		if event.Delta != "" {
			result = append(result, &AnthropicEvent{
				Type:  "content_block_delta",
				Index: e.activeIndex,
				Delta: map[string]any{
					"type": "text_delta",
					"text": event.Delta,
				},
			})
		}
		if len(event.ToolCalls) > 0 {
			result = append(result, e.emitToolCalls(event.ToolCalls)...)
		}
		return result
	case provider.EventToolCallDone:
		if event.Output == nil {
			return nil
		}
		if event.Output.Type == "function_call" {
			return e.emitToolCalls([]provider.ToolCall{{
				ID:   firstNonEmpty(event.Output.ID, event.Output.CallID),
				Type: "function",
				Function: provider.FunctionCall{
					Name:      event.Output.Name,
					Arguments: event.Output.Args,
				},
			}})
		}
		return nil
	case provider.EventResponseCompleted:
		result := e.ensureMessageStart(event.Response)
		result = append(result, e.closeActiveBlock()...)
		stopReason := "end_turn"
		var usage AnthropicUsage
		if event.Response != nil {
			if len(event.Response.OutputToolCalls()) > 0 {
				stopReason = "tool_use"
			}
			usage = AnthropicUsage{
				InputTokens:  event.Response.Usage.PromptTokens,
				OutputTokens: event.Response.Usage.CompletionTokens,
			}
		}
		result = append(result,
			&AnthropicEvent{
				Type: "message_delta",
				Delta: map[string]any{
					"stop_reason": stopReason,
				},
				Message: &AnthropicMessagesResponse{
					StopReason: stopReason,
					Usage:      usage,
				},
			},
			&AnthropicEvent{Type: "message_stop"},
		)
		return result
	default:
		return nil
	}
}

func (e *AnthropicStreamEncoder) ensureMessageStart(resp *provider.Response) []*AnthropicEvent {
	if e.started {
		return nil
	}
	e.started = true
	return []*AnthropicEvent{{
		Type: "message_start",
		Message: &AnthropicMessagesResponse{
			ID:      e.responseID,
			Type:    "message",
			Role:    "assistant",
			Model:   e.model,
			Content: []AnthropicContentBlock{},
			Usage: AnthropicUsage{
				InputTokens: responsePromptTokens(resp),
			},
		},
	}}
}

func (e *AnthropicStreamEncoder) ensureTextBlockStart() []*AnthropicEvent {
	if e.activeIndex >= 0 {
		return nil
	}
	index := e.nextIndex
	e.activeIndex = index
	e.nextIndex++
	return []*AnthropicEvent{{
		Type:  "content_block_start",
		Index: index,
		Block: &AnthropicContentBlock{Type: "text", Text: ""},
	}}
}

func (e *AnthropicStreamEncoder) closeActiveBlock() []*AnthropicEvent {
	if e.activeIndex < 0 {
		return nil
	}
	index := e.activeIndex
	e.activeIndex = -1
	return []*AnthropicEvent{{Type: "content_block_stop", Index: index}}
}

func (e *AnthropicStreamEncoder) emitToolCalls(calls []provider.ToolCall) []*AnthropicEvent {
	result := e.closeActiveBlock()
	for _, call := range calls {
		index := e.nextIndex
		e.nextIndex++
		result = append(result,
			&AnthropicEvent{
				Type:  "content_block_start",
				Index: index,
				Block: &AnthropicContentBlock{
					Type:  "tool_use",
					ID:    call.ID,
					Name:  call.Function.Name,
					Input: marshalRawJSON(call.Function.Arguments),
				},
			},
			&AnthropicEvent{Type: "content_block_stop", Index: index},
		)
	}
	return result
}

func convertAnthropicSystem(system any) string {
	switch current := system.(type) {
	case string:
		return current
	case []any:
		parts := make([]string, 0, len(current))
		for _, item := range current {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func convertAnthropicMessages(msgs []AnthropicMessage) []provider.Message {
	result := make([]provider.Message, 0, len(msgs))
	for _, msg := range msgs {
		content := make([]provider.ContentBlock, 0, len(msg.Content))
		toolCalls := make([]provider.ToolCall, 0)
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   block.ID,
					Type: "function",
					Function: provider.FunctionCall{
						Name:      block.Name,
						Arguments: strings.TrimSpace(string(block.Input)),
					},
				})
				continue
			}
			if block.Type == "tool_result" {
				result = append(result, provider.Message{
					Role:       "tool",
					ToolCallID: block.ToolUseID,
					Content:    provider.TextBlocks(firstNonEmpty(block.Content, block.Text)),
				})
				continue
			}
			content = append(content, convertAnthropicBlock(block)...)
		}
		if len(content) == 0 && len(toolCalls) == 0 {
			continue
		}
		result = append(result, provider.Message{Role: msg.Role, Content: content, ToolCalls: toolCalls})
	}
	return result
}

func convertAnthropicBlock(block AnthropicContentBlock) []provider.ContentBlock {
	switch block.Type {
	case "text":
		return provider.TextBlocks(block.Text)
	case "thinking":
		return []provider.ContentBlock{{
			Type:      "thinking",
			Thinking:  block.Thinking,
			Signature: block.Signature,
		}}
	case "image":
		if block.Source == nil {
			return nil
		}
		return []provider.ContentBlock{{
			Type: "image",
			Image: &provider.ContentImage{
				SourceType: block.Source.Type,
				URL:        block.Source.Data, // URL-style image support remains provider-specific; keep raw source here
				MediaType:  block.Source.MediaType,
				Data:       block.Source.Data,
			},
		}}
	default:
		return nil
	}
}

func convertResponseToAnthropicContent(outputs []provider.ResponseOutput) []AnthropicContentBlock {
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
						blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: content.Text})
					}
				}
			}
		case "function_call":
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    firstNonEmpty(output.ID, output.CallID),
				Name:  output.Name,
				Input: marshalRawJSON(output.Args),
			})
		}
	}
	return blocks
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

func responsePromptTokens(resp *provider.Response) int {
	if resp == nil {
		return 0
	}
	return resp.Usage.PromptTokens
}
