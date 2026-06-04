package provider

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	oairesponses "github.com/openai/openai-go/responses"
)

// --- Request builders: internal Message → SDK params ---

func buildOpenAIInput(messages []Message) []oairesponses.ResponseInputItemUnionParam {
	items := make([]oairesponses.ResponseInputItemUnionParam, 0, len(messages))
	for _, message := range messages {
		if message.ToolCallID != "" {
			items = append(items, oairesponses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &oairesponses.ResponseInputItemFunctionCallOutputParam{
					CallID: message.ToolCallID,
					Output: collectText(message.Content),
					Type:   "function_call_output",
				},
			})
			continue
		}

		if content := buildOpenAIMessageContent(message.Content); len(content) > 0 {
			role := message.Role
			if role == "" {
				role = "user"
			}

			contentParam := oairesponses.EasyInputMessageContentUnionParam{
				OfInputItemContentList: content,
			}
			if len(content) == 1 && content[0].OfInputText != nil {
				contentParam = oairesponses.EasyInputMessageContentUnionParam{
					OfString: param.NewOpt(content[0].OfInputText.Text),
				}
			}

			items = append(items, oairesponses.ResponseInputItemUnionParam{
				OfMessage: &oairesponses.EasyInputMessageParam{
					Role:    oairesponses.EasyInputMessageRole(role),
					Content: contentParam,
					Type:    "message",
				},
			})
		}

		for _, call := range message.ToolCalls {
			items = append(items, oairesponses.ResponseInputItemUnionParam{
				OfFunctionCall: &oairesponses.ResponseFunctionToolCallParam{
					Arguments: call.Function.Arguments,
					CallID:    firstNonEmpty(call.ID, message.ToolCallID),
					Name:      call.Function.Name,
					Type:      "function_call",
				},
			})
		}
	}
	return items
}

func buildChatCompletionMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		if len(msg.Content) == 0 && len(msg.ToolCalls) == 0 && msg.ToolCallID == "" {
			continue
		}

		switch role {
		case "system":
			text := collectText(msg.Content)
			if text != "" {
				result = append(result, openai.SystemMessage(text))
			}
		case "developer":
			text := collectText(msg.Content)
			if text != "" {
				result = append(result, openai.DeveloperMessage(text))
			}
		case "assistant":
			result = append(result, buildChatCompletionAssistantMessage(msg))
		case "tool":
			if msg.ToolCallID != "" {
				result = append(result, openai.ToolMessage(collectText(msg.Content), msg.ToolCallID))
			}
		default:
			if hasImageBlocks(msg.Content) {
				parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(msg.Content))
				for _, block := range msg.Content {
					if part, ok := buildChatCompletionContentPart(block); ok {
						parts = append(parts, part)
					}
				}
				if len(parts) > 0 {
					result = append(result, openai.UserMessage(parts))
				}
			} else {
				text := collectText(msg.Content)
				if text != "" {
					result = append(result, openai.UserMessage(text))
				}
			}
		}
	}
	return result
}

func buildChatCompletionAssistantMessage(msg Message) openai.ChatCompletionMessageParamUnion {
	var toolCalls []openai.ChatCompletionMessageToolCallParam
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
			ID:   tc.ID,
			Type: "function",
			Function: openai.ChatCompletionMessageToolCallFunctionParam{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	content := collectText(msg.Content)
	var contentUnion openai.ChatCompletionAssistantMessageParamContentUnion
	if content != "" {
		contentUnion.OfString = param.NewOpt(content)
	}

	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			Role:      "assistant",
			Content:   contentUnion,
			ToolCalls: toolCalls,
		},
	}
}

func buildOpenAIMessageContent(content []ContentBlock) []oairesponses.ResponseInputContentUnionParam {
	parts := make([]oairesponses.ResponseInputContentUnionParam, 0, len(content))
	for _, item := range content {
		part, ok := buildOpenAIContentPart(item)
		if ok {
			parts = append(parts, part)
		}
	}
	return parts
}

func buildOpenAIContentPart(value ContentBlock) (oairesponses.ResponseInputContentUnionParam, bool) {
	switch value.Type {
	case "text", "output_text":
		if value.Text == "" {
			return oairesponses.ResponseInputContentUnionParam{}, false
		}
		return oairesponses.ResponseInputContentParamOfInputText(value.Text), true
	case "thinking":
		if value.Thinking == "" {
			return oairesponses.ResponseInputContentUnionParam{}, false
		}
		return oairesponses.ResponseInputContentParamOfInputText(value.Thinking), true
	case "refusal":
		if value.Refusal == "" {
			return oairesponses.ResponseInputContentUnionParam{}, false
		}
		return oairesponses.ResponseInputContentParamOfInputText(value.Refusal), true
	case "image":
		if value.Image == nil {
			return oairesponses.ResponseInputContentUnionParam{}, false
		}
		img := &oairesponses.ResponseInputImageParam{Type: "input_image"}
		if value.Image.URL != "" {
			img.ImageURL = param.NewOpt(value.Image.URL)
		} else if value.Image.Data != "" {
			img.ImageURL = param.NewOpt("data:" + value.Image.MediaType + ";base64," + value.Image.Data)
		}
		return oairesponses.ResponseInputContentUnionParam{OfInputImage: img}, true
	case "structured_output":
		if value.Structured != nil && value.Structured.Data != nil {
			raw, _ := json.Marshal(value.Structured.Data)
			return oairesponses.ResponseInputContentParamOfInputText(string(raw)), true
		}
	}
	return oairesponses.ResponseInputContentUnionParam{}, false
}

func normalizeOpenAITextType(typeName string) string {
	switch typeName {
	case "text", "output_text":
		return "input_text"
	default:
		return typeName
	}
}

func buildChatCompletionContentPart(value ContentBlock) (openai.ChatCompletionContentPartUnionParam, bool) {
	switch value.Type {
	case "text", "output_text":
		if value.Text == "" {
			return openai.ChatCompletionContentPartUnionParam{}, false
		}
		return openai.TextContentPart(value.Text), true
	case "image":
		if value.Image == nil || value.Image.URL == "" {
			return openai.ChatCompletionContentPartUnionParam{}, false
		}
		imgURL := openai.ChatCompletionContentPartImageImageURLParam{URL: value.Image.URL}
		if value.Image.Detail != "" {
			imgURL.Detail = value.Image.Detail
		}
		return openai.ImageContentPart(imgURL), true
	default:
		return openai.ChatCompletionContentPartUnionParam{}, false
	}
}

func hasImageBlocks(content []ContentBlock) bool {
	for _, block := range content {
		if block.Type == "image" {
			return true
		}
	}
	return false
}

// detectResponseFormat detects responses API vs chat completions by response body structure.
func detectResponseFormat(body []byte) string {
	var preview struct {
		Output  json.RawMessage `json:"output"`
		Choices json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(body, &preview); err != nil {
		return "unknown"
	}

	if len(preview.Output) > 0 && len(preview.Choices) == 0 {
		return "responses"
	}
	if len(preview.Choices) > 0 && len(preview.Output) == 0 {
		return "chat"
	}

	var objCheck struct {
		Object string `json:"object"`
	}
	if json.Unmarshal(body, &objCheck) == nil {
		if strings.Contains(objCheck.Object, "chat.completion") {
			return "chat"
		}
		if strings.Contains(objCheck.Object, "response") {
			return "responses"
		}
	}

	var choicesCheck struct {
		Choices []any `json:"choices"`
	}
	if json.Unmarshal(body, &choicesCheck) == nil && len(choicesCheck.Choices) > 0 {
		return "chat"
	}

	var outputCheck struct {
		Output []any `json:"output"`
	}
	if json.Unmarshal(body, &outputCheck) == nil && len(outputCheck.Output) > 0 {
		return "responses"
	}

	return "unknown"
}

// --- Response converters: SDK types → internal Response ---

func convertSDKChatCompletion(resp openai.ChatCompletion, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = resp.Model
	}

	var output []ResponseOutput
	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message

		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				output = append(output, ResponseOutput{
					Type:   "function_call",
					CallID: tc.ID,
					Name:   tc.Function.Name,
					Args:   tc.Function.Arguments,
				})
			}
		}

		if msg.Content != "" {
			output = append(output, ResponseOutput{
				Type: "message",
				Content: []ResponseContent{
					{Type: "output_text", Text: msg.Content},
				},
			})
		}
	}

	return &Response{
		ID:      resp.ID,
		Object:  "response",
		Created: resp.Created,
		Model:   model,
		Status:  "completed",
		Output:  output,
		Usage: Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
			CachedTokens:     int(resp.Usage.PromptTokensDetails.CachedTokens),
		},
	}
}

func convertSDKResponse(resp oairesponses.Response, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = string(resp.Model)
	}

	output := make([]ResponseOutput, 0, len(resp.Output))
	for _, item := range resp.Output {
		converted := convertSDKOutputItem(item)
		if converted != nil {
			output = append(output, *converted)
		}
	}

	return &Response{
		ID:      resp.ID,
		Object:  "response",
		Created: int64(resp.CreatedAt),
		Model:   model,
		Status:  string(resp.Status),
		Output:  output,
		Usage: Usage{
			PromptTokens:     int(resp.Usage.InputTokens),
			CompletionTokens: int(resp.Usage.OutputTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
			CachedTokens:     int(resp.Usage.InputTokensDetails.CachedTokens),
		},
	}
}

func convertSDKOutputItem(item oairesponses.ResponseOutputItemUnion) *ResponseOutput {
	switch item.Type {
	case "message":
		content := make([]ResponseContent, 0, len(item.Content))
		for _, block := range item.Content {
			switch block.Type {
			case "output_text":
				if block.Text != "" {
					content = append(content, ResponseContent{Type: "output_text", Text: block.Text})
				}
			case "refusal":
				if block.Refusal != "" {
					content = append(content, ResponseContent{Type: "refusal", Refusal: block.Refusal})
				}
			}
		}
		return &ResponseOutput{
			ID:      item.ID,
			Type:    "message",
			Role:    string(item.Role),
			Status:  item.Status,
			Content: content,
		}
	case "function_call":
		return &ResponseOutput{
			ID:     item.ID,
			Type:   "function_call",
			CallID: item.CallID,
			Name:   item.Name,
			Args:   item.Arguments,
		}
	default:
		return nil
	}
}

// --- Stream parsers: SDK chunk/event types → ResponseEvent ---

func parseSDKChatCompletionChunk(chunk openai.ChatCompletionChunk, requestedModel string) *ResponseEvent {
	// Handle usage-only chunks (stream_options: {"include_usage": true})
	if len(chunk.Choices) == 0 {
		if chunk.Usage.TotalTokens > 0 {
			return &ResponseEvent{
				Type: EventContentDelta,
				Usage: &Usage{
					PromptTokens:     int(chunk.Usage.PromptTokens),
					CompletionTokens: int(chunk.Usage.CompletionTokens),
					TotalTokens:      int(chunk.Usage.TotalTokens),
				},
			}
		}
		return nil
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	text := delta.Content
	toolCalls := make([]ToolCall, 0, len(delta.ToolCalls))
	for _, tc := range delta.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Type: string(tc.Type),
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	if text == "" && len(toolCalls) == 0 && choice.FinishReason == "" && chunk.Usage.TotalTokens == 0 {
		return nil
	}

	event := ResponseEvent{
		Type:      EventContentDelta,
		Delta:     text,
		TextDelta: text,
	}
	if len(toolCalls) > 0 {
		event.ToolCalls = toolCalls
	}
	if choice.FinishReason != "" {
		event.FinishReason = choice.FinishReason
	}
	if chunk.Usage.TotalTokens > 0 {
		event.Usage = &Usage{
			PromptTokens:     int(chunk.Usage.PromptTokens),
			CompletionTokens: int(chunk.Usage.CompletionTokens),
			TotalTokens:      int(chunk.Usage.TotalTokens),
		}
	}
	return &event
}

func parseSDKResponseStreamEvent(event oairesponses.ResponseStreamEventUnion, requestedModel string) (*ResponseEvent, error) {
	switch variant := event.AsAny().(type) {
	case oairesponses.ResponseTextDeltaEvent:
		if variant.Delta == "" {
			return nil, nil
		}
		return &ResponseEvent{
			Type:      EventContentDelta,
			Delta:     variant.Delta,
			TextDelta: variant.Delta,
		}, nil
	case oairesponses.ResponseOutputItemDoneEvent:
		output := convertSDKOutputItem(variant.Item)
		if output == nil {
			return nil, nil
		}
		return &ResponseEvent{
			Type:   EventToolCallDone,
			Output: output,
		}, nil
	case oairesponses.ResponseCompletedEvent:
		return &ResponseEvent{
			Type:     EventResponseCompleted,
			Response: convertSDKResponse(variant.Response, requestedModel),
		}, nil
	case oairesponses.ResponseErrorEvent:
		msg := variant.Message
		if msg == "" {
			msg = "upstream response failed"
		}
		return nil, newProviderUpstreamMessageError(msg)
	case oairesponses.ResponseRefusalDeltaEvent:
		if variant.Delta == "" {
			return nil, nil
		}
		return &ResponseEvent{
			Type:      EventContentDelta,
			Delta:     variant.Delta,
			TextDelta: variant.Delta,
		}, nil
	default:
		return nil, nil
	}
}

// --- Extra-body helper: merge arbitrary fields into SDK params via JSON round-trip ---

func mergeExtraBody(params any, extraBody map[string]any) error {
	if len(extraBody) == 0 {
		return nil
	}
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	mergeAnyMap(payload, extraBody)
	body, err = json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, params)
}
