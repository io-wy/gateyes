package provider

import "time"

type ChatMessage struct {
	Role       string     `json:"role,omitempty"`
	Content    any        `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ChatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []ChatMessage `json:"messages"`
	Stream         bool          `json:"stream,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	Tools          []any         `json:"tools,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object,omitempty"`
	Created int64                  `json:"created,omitempty"`
	Model   string                 `json:"model,omitempty"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int         `json:"index,omitempty"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Usage   *Usage                      `json:"usage,omitempty"`
}

type ChatCompletionChunkChoice struct {
	Index        int                      `json:"index"`
	Delta        ChatCompletionChunkDelta `json:"delta"`
	FinishReason string                   `json:"finish_reason,omitempty"`
}

type ChatCompletionChunkDelta struct {
	Role      string                        `json:"role,omitempty"`
	Content   string                        `json:"content,omitempty"`
	ToolCalls []ChatCompletionChunkToolCall `json:"tool_calls,omitempty"`
}

type ChatCompletionChunkToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function,omitempty"`
}

func ConvertChatRequest(req *ChatCompletionRequest) *ResponseRequest {
	if req == nil {
		return nil
	}

	messages := make([]Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, Message{
			Role:       msg.Role,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  append([]ToolCall(nil), msg.ToolCalls...),
			Content:    NormalizeMessageContent(msg.Content),
		})
	}
	return &ResponseRequest{
		Model:        req.Model,
		Surface:      "chat",
		Input:        messages,
		Messages:     messages,
		Stream:       req.Stream,
		MaxTokens:    req.MaxTokens,
		Tools:        req.Tools,
		OutputFormat: normalizeOutputFormatValue(req.ResponseFormat),
	}
}

func ConvertResponseToChat(resp *Response) *ChatCompletionResponse {
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
			Message: ChatMessage{
				Role:      "assistant",
				Content:   resp.OutputText(),
				ToolCalls: resp.OutputToolCalls(),
			},
			FinishReason: responseFinishReason(resp),
		}},
		Usage: resp.Usage,
	}
}

func ConvertEventToChatChunk(responseID, model string, event ResponseEvent) *ChatCompletionChunk {
	chunk := &ChatCompletionChunk{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChunkChoice{{
			Index: 0,
			Delta: ChatCompletionChunkDelta{},
		}},
	}

	switch event.Type {
	case EventResponseStarted:
		return chunk
	case EventContentDelta:
		chunk.Choices[0].Delta = ChatCompletionChunkDelta{Content: event.Text()}
		chunk.Choices[0].FinishReason = event.FinishReason
		if len(event.ToolCalls) > 0 {
			toolCalls := make([]ChatCompletionChunkToolCall, 0, len(event.ToolCalls))
			for index, call := range event.ToolCalls {
				toolCalls = append(toolCalls, ChatCompletionChunkToolCall{
					Index: index,
					ID:    call.ID,
					Type:  firstNonEmpty(call.Type, "function"),
					Function: FunctionCall{
						Name:      call.Function.Name,
						Arguments: call.Function.Arguments,
					},
				})
			}
			chunk.Choices[0].Delta.ToolCalls = toolCalls
		}
		if event.Usage != nil {
			chunk.Usage = event.Usage
		}
	case EventToolCallDone:
		if event.Output == nil || event.Output.Type != "function_call" {
			return nil
		}
		chunk.Choices[0].Delta.ToolCalls = []ChatCompletionChunkToolCall{{
			Index: 0,
			ID:    event.Output.ID,
			Type:  "function",
			Function: FunctionCall{
				Name:      event.Output.Name,
				Arguments: event.Output.Args,
			},
		}}
	case EventResponseCompleted:
		chunk.Choices[0].FinishReason = "stop"
		if event.Response != nil {
			chunk.Choices[0].FinishReason = responseFinishReason(event.Response)
			usage := event.Response.Usage
			chunk.Usage = &usage
		}
	default:
		return chunk
	}

	return chunk
}

func normalizeOutputFormatValue(value any) *OutputFormat {
	current, ok := value.(map[string]any)
	if !ok || len(current) == 0 {
		return nil
	}
	format := &OutputFormat{Raw: current}
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

func responseFinishReason(resp *Response) string {
	if len(resp.OutputToolCalls()) > 0 {
		return "tool_calls"
	}
	return "stop"
}
