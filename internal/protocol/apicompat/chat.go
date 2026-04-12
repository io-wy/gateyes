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
	messages := make([]provider.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, provider.Message{
			Role:       msg.Role,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  append([]provider.ToolCall(nil), msg.ToolCalls...),
			Content:    provider.NormalizeMessageContent(msg.Content),
		})
	}
	return &provider.ResponseRequest{
		Model:        req.Model,
		Input:        messages,
		Messages:     messages,
		Stream:       req.Stream,
		MaxTokens:    req.MaxTokens,
		Tools:        tools,
		OutputFormat: normalizeOutputFormat(req.ResponseFormat),
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
			Message: provider.ChatMessage{
				Role:      "assistant",
				Content:   resp.OutputText(),
				ToolCalls: resp.OutputToolCalls(),
			},
			FinishReason: responseFinishReason(resp),
		}},
		Usage: resp.Usage,
	}
}

func normalizeOutputFormat(value any) *provider.OutputFormat {
	current, ok := value.(map[string]any)
	if !ok || len(current) == 0 {
		return nil
	}
	format := &provider.OutputFormat{
		Type: "",
		Raw:  current,
	}
	format.Type, _ = current["type"].(string)
	if jsonSchema, ok := current["json_schema"].(map[string]any); ok {
		format.Type = "json_schema"
		format.Name, _ = jsonSchema["name"].(string)
		format.Strict, _ = jsonSchema["strict"].(bool)
		if schema, ok := jsonSchema["schema"].(map[string]any); ok {
			format.Schema = schema
		}
	}
	if format.Type == "" {
		format.Type = "text"
	}
	return format
}

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

func (e *ChatStreamEncoder) Encode(event provider.ResponseEvent) []*ChatCompletionChunk {
	if e == nil {
		return nil
	}

	switch event.Type {
	case provider.EventResponseStarted:
		return nil
	case provider.EventContentDelta:
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
	case provider.EventToolCallDone:
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
			Function: provider.FunctionCall{
				Name:      event.Output.Name,
				Arguments: event.Output.Args,
			},
		}}
		return []*ChatCompletionChunk{chunk}
	case provider.EventResponseCompleted:
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
