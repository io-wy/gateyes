package provider

import "time"

type ChatStreamEncoder struct {
	responseID string
	model      string
	sentRole   bool
	finished   bool
}

func NewChatStreamEncoder(responseID, model string) *ChatStreamEncoder {
	return &ChatStreamEncoder{
		responseID: responseID,
		model:      model,
	}
}

func (e *ChatStreamEncoder) Encode(event ResponseEvent) []*ChatCompletionChunk {
	if e == nil {
		return nil
	}

	switch event.Type {
	case EventResponseStarted:
		return nil
	case EventContentDelta:
		if e.finished {
			return nil
		}
		if event.Text() == "" && len(event.ToolCalls) == 0 && event.FinishReason == "" && event.Usage == nil {
			return nil
		}
		chunk := e.newChunk()
		if !e.sentRole {
			e.sentRole = true
			chunk.Choices[0].Delta.Role = "assistant"
		}
		chunk.Choices[0].Delta.Content = event.Text()
		if len(event.ToolCalls) > 0 {
			chunk.Choices[0].Delta.ToolCalls = e.convertToolCalls(event.ToolCalls)
		}
		if event.FinishReason != "" {
			chunk.Choices[0].FinishReason = event.FinishReason
		}
		if event.Usage != nil {
			usage := *event.Usage
			chunk.Usage = &usage
		}
		if chunk.Choices[0].FinishReason != "" {
			e.finished = true
		}
		return []*ChatCompletionChunk{chunk}
	case EventToolCallDone:
		if e.finished {
			return nil
		}
		if event.Output == nil || event.Output.Type != "function_call" {
			return nil
		}
		chunk := e.newChunk()
		if !e.sentRole {
			e.sentRole = true
			chunk.Choices[0].Delta.Role = "assistant"
		}
		chunk.Choices[0].Delta.ToolCalls = []ChatCompletionChunkToolCall{{
			Index: 0,
			ID:    firstNonEmpty(event.Output.ID, event.Output.CallID),
			Type:  "function",
			Function: FunctionCall{
				Name:      event.Output.Name,
				Arguments: event.Output.Args,
			},
		}}
		return []*ChatCompletionChunk{chunk}
	case EventResponseCompleted:
		if e.finished {
			return nil
		}
		e.finished = true
		chunk := e.newChunk()
		chunk.Choices[0].FinishReason = "stop"
		if event.Response != nil {
			chunk.Choices[0].FinishReason = responseFinishReason(event.Response)
			usage := event.Response.Usage
			chunk.Usage = &usage
		}
		return []*ChatCompletionChunk{chunk}
	default:
		return nil
	}
}

func (e *ChatStreamEncoder) convertToolCalls(calls []ToolCall) []ChatCompletionChunkToolCall {
	result := make([]ChatCompletionChunkToolCall, 0, len(calls))
	for index, call := range calls {
		result = append(result, ChatCompletionChunkToolCall{
			Index:    index,
			ID:       call.ID,
			Type:     firstNonEmpty(call.Type, "function"),
			Function: call.Function,
		})
	}
	return result
}

func (e *ChatStreamEncoder) newChunk() *ChatCompletionChunk {
	return &ChatCompletionChunk{
		ID:      e.responseID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   e.model,
		Choices: []ChatCompletionChunkChoice{{
			Index: 0,
			Delta: ChatCompletionChunkDelta{},
		}},
	}
}
