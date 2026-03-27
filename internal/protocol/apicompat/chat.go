package apicompat

import (
	"time"

	"github.com/gateyes/gateway/internal/service/provider"
)

func ConvertChatRequest(req *ChatCompletionRequest) *provider.ResponseRequest {
	if req == nil {
		return nil
	}

	var tools []any
	if len(req.Tools) > 0 {
		cloned, _ := cloneAny(req.Tools).([]any)
		tools = cloned
	}
	messages := cloneMessages(req.Messages)
	return &provider.ResponseRequest{
		Model:     req.Model,
		Input:     messages,
		Messages:  messages,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
		Tools:     tools,
	}
}

func ConvertResponseToChat(resp *provider.Response) *ChatCompletionResponse {
	if resp == nil {
		return nil
	}

	return &ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.Created,
		Model:   resp.Model,
		Choices: []ChatCompletionChoice{{
			Index: 0,
			Message: provider.Message{
				Role:      "assistant",
				Content:   resp.OutputText(),
				ToolCalls: resp.OutputToolCalls(),
			},
			FinishReason: responseFinishReason(resp),
		}},
		Usage: resp.Usage,
	}
}

type ChatStreamEncoder struct {
	responseID string
	model      string
	sentRole   bool
}

func NewChatStreamEncoder(responseID, model string) *ChatStreamEncoder {
	return &ChatStreamEncoder{
		responseID: responseID,
		model:      model,
	}
}

func (e *ChatStreamEncoder) Encode(event provider.ResponseEvent) []*ChatCompletionChunk {
	if e == nil {
		return nil
	}

	switch event.Type {
	case "response.created":
		return nil
	case "response.output_text.delta", "chat.delta", "chat.completion.chunk":
		chunks := e.ensureAssistantRole()
		chunk := e.newChunk()
		chunk.Choices[0].Delta.Content = event.Delta
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
		return append(chunks, chunk)
	case "response.output_item.done":
		if event.Output == nil || event.Output.Type != "function_call" {
			return nil
		}
		chunks := e.ensureAssistantRole()
		chunk := e.newChunk()
		chunk.Choices[0].Delta.ToolCalls = []ChatCompletionChunkToolCall{{
			Index: 0,
			ID:    firstNonEmpty(event.Output.ID, event.Output.CallID),
			Type:  "function",
			Function: provider.FunctionCall{
				Name:      event.Output.Name,
				Arguments: event.Output.Args,
			},
		}}
		return append(chunks, chunk)
	case "response.completed":
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

func (e *ChatStreamEncoder) ensureAssistantRole() []*ChatCompletionChunk {
	if e.sentRole {
		return nil
	}
	e.sentRole = true
	chunk := e.newChunk()
	chunk.Choices[0].Delta.Role = "assistant"
	return []*ChatCompletionChunk{chunk}
}

func (e *ChatStreamEncoder) convertToolCalls(calls []provider.ToolCall) []ChatCompletionChunkToolCall {
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

func responseFinishReason(resp *provider.Response) string {
	if len(resp.OutputToolCalls()) > 0 {
		return "tool_calls"
	}
	return "stop"
}
