package provider

type AnthropicStreamEncoder struct {
	responseID  string
	model       string
	started     bool
	activeIndex int
	activeType  string
	nextIndex   int
}

func NewAnthropicStreamEncoder(responseID, model string) *AnthropicStreamEncoder {
	return &AnthropicStreamEncoder{
		responseID:  responseID,
		model:       model,
		activeIndex: -1,
	}
}

func (e *AnthropicStreamEncoder) Encode(event ResponseEvent) []*AnthropicEvent {
	if e == nil {
		return nil
	}

	switch event.Type {
	case EventResponseStarted:
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
	case EventContentDelta:
		result := e.ensureMessageStart(nil)
		if event.Text() != "" {
			result = append(result, e.ensureBlockStart("text")...)
			result = append(result, &AnthropicEvent{
				Type:  "content_block_delta",
				Index: e.activeIndex,
				Delta: map[string]any{
					"type": "text_delta",
					"text": event.Text(),
				},
			})
		}
		if len(event.ToolCalls) > 0 {
			result = append(result, e.emitToolCalls(event.ToolCalls)...)
		}
		return result
	case EventThinkingDelta:
		if event.ThinkingDelta == "" {
			return nil
		}
		result := e.ensureMessageStart(nil)
		result = append(result, e.ensureBlockStart("thinking")...)
		result = append(result, &AnthropicEvent{
			Type:  "content_block_delta",
			Index: e.activeIndex,
			Delta: map[string]any{
				"type":     "thinking_delta",
				"thinking": event.ThinkingDelta,
			},
		})
		return result
	case EventToolCallDone:
		if event.Output == nil {
			return nil
		}
		if event.Output.Type == "function_call" {
			return e.emitToolCalls([]ToolCall{{
				ID:   firstNonEmpty(event.Output.ID, event.Output.CallID),
				Type: "function",
				Function: FunctionCall{
					Name:      event.Output.Name,
					Arguments: event.Output.Args,
				},
			}})
		}
		return nil
	case EventResponseCompleted:
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

func (e *AnthropicStreamEncoder) ensureMessageStart(resp *Response) []*AnthropicEvent {
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

func (e *AnthropicStreamEncoder) ensureBlockStart(blockType string) []*AnthropicEvent {
	if e.activeIndex >= 0 && e.activeType == blockType {
		return nil
	}
	result := e.closeActiveBlock()
	index := e.nextIndex
	e.activeIndex = index
	e.activeType = blockType
	e.nextIndex++
	return append(result, &AnthropicEvent{
		Type:  "content_block_start",
		Index: index,
		Block: &AnthropicContentBlock{Type: blockType, Text: ""},
	})
}

func (e *AnthropicStreamEncoder) closeActiveBlock() []*AnthropicEvent {
	if e.activeIndex < 0 {
		return nil
	}
	index := e.activeIndex
	e.activeIndex = -1
	e.activeType = ""
	return []*AnthropicEvent{{Type: "content_block_stop", Index: index}}
}

func (e *AnthropicStreamEncoder) emitToolCalls(calls []ToolCall) []*AnthropicEvent {
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

func responsePromptTokens(resp *Response) int {
	if resp == nil {
		return 0
	}
	return resp.Usage.PromptTokens
}
