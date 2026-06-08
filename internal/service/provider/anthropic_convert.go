package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/gateyes/gateway/internal/app/config"
)

func buildAnthropicStreamResponse(id, model string, outputs []ResponseOutput, promptTokens, completionTokens int) *Response {
	if completionTokens == 0 {
		completionTokens = RoughTokenCount(renderOutputSignature(outputs))
	}
	return &Response{
		ID:      id,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  outputs,
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
			CachedTokens:     0,
		},
	}
}

func convertAnthropicOutputs(role string, blocks []AnthropicContentBlock) []ResponseOutput {
	outputs := make([]ResponseOutput, 0, len(blocks))
	messageOutputIndex := -1
	for _, block := range blocks {
		switch block.Type {
		case "thinking":
			if messageOutputIndex < 0 {
				outputs = append(outputs, ResponseOutput{
					Type:   "message",
					Role:   role,
					Status: "completed",
				})
				messageOutputIndex = len(outputs) - 1
			}
			outputs[messageOutputIndex].Content = append(outputs[messageOutputIndex].Content, ResponseContent{
				Type:      "thinking",
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "text":
			if messageOutputIndex < 0 {
				outputs = append(outputs, ResponseOutput{
					Type:   "message",
					Role:   role,
					Status: "completed",
				})
				messageOutputIndex = len(outputs) - 1
			}
			outputs[messageOutputIndex].Content = append(outputs[messageOutputIndex].Content, ResponseContent{
				Type: "output_text",
				Text: block.Text,
			})
		case "tool_use":
			messageOutputIndex = -1
			outputs = append(outputs, ResponseOutput{
				ID:     block.ID,
				Type:   "function_call",
				Status: "completed",
				CallID: block.ID,
				Name:   block.Name,
				Args:   strings.TrimSpace(string(block.Input)),
			})
		}
	}
	return outputs
}

func renderOutputSignature(outputs []ResponseOutput) string {
	var builder strings.Builder
	for _, output := range outputs {
		if output.Type == "function_call" {
			builder.WriteString(output.Name)
			builder.WriteString(output.Args)
			continue
		}
		for _, content := range output.Content {
			builder.WriteString(content.Text)
		}
	}
	return builder.String()
}

func convertSDKAnthropicMessage(msg anthropic.Message, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = msg.Model
	}

	outputs := make([]AnthropicContentBlock, 0, len(msg.Content))
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			outputs = append(outputs, AnthropicContentBlock{Type: "text", Text: block.Text})
		case "thinking":
			outputs = append(outputs, AnthropicContentBlock{Type: "thinking", Thinking: block.Thinking, Signature: block.Signature})
		case "tool_use":
			outputs = append(outputs, AnthropicContentBlock{Type: "tool_use", ID: block.ID, Name: block.Name, Input: block.Input})
		}
	}

	cachedTokens := int(msg.Usage.CacheReadInputTokens)

	return &Response{
		ID:      msg.ID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  convertAnthropicOutputs("assistant", outputs),
		Usage: Usage{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
			CachedTokens:     cachedTokens,
		},
	}
}

var _ Provider = (*anthropicProvider)(nil)

func uuid() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

type anthropicStreamState struct {
	responseID       string
	model            string
	promptTokens     int
	completionTokens int
	cachedTokens     int
	outputs          []ResponseOutput
	activeTool       *ResponseOutput
	completed        bool
}

func (s *anthropicStreamState) appendText(text string) {
	if text == "" {
		return
	}
	if len(s.outputs) == 0 || s.outputs[len(s.outputs)-1].Type != "message" {
		s.outputs = append(s.outputs, ResponseOutput{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
		})
	}
	index := len(s.outputs) - 1
	s.outputs[index].Content = append(s.outputs[index].Content, ResponseContent{
		Type: "output_text",
		Text: text,
	})
}

func (s *anthropicStreamState) appendThinking(thinking, signature string) {
	if thinking == "" {
		return
	}
	if len(s.outputs) == 0 || s.outputs[len(s.outputs)-1].Type != "message" {
		s.outputs = append(s.outputs, ResponseOutput{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
		})
	}
	index := len(s.outputs) - 1
	s.outputs[index].Content = append(s.outputs[index].Content, ResponseContent{
		Type:      "thinking",
		Thinking:  thinking,
		Signature: signature,
	})
}

func (s *anthropicStreamState) applyContentBlock(block AnthropicContentBlock) {
	switch block.Type {
	case "text":
		s.appendText(block.Text)
	case "thinking":
		s.appendThinking(block.Thinking, block.Signature)
	case "tool_use":
		args := strings.TrimSpace(string(block.Input))
		if args == "" {
			args = "{}"
		}
		s.outputs = append(s.outputs, ResponseOutput{
			ID:     block.ID,
			Type:   "function_call",
			Status: "completed",
			CallID: block.ID,
			Name:   block.Name,
			Args:   args,
		})
	}
}

func (s *anthropicStreamState) response() *Response {
	return &Response{
		ID:      s.responseID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   s.model,
		Status:  "completed",
		Output:  append([]ResponseOutput(nil), s.outputs...),
		Usage: Usage{
			PromptTokens:     s.promptTokens,
			CompletionTokens: s.completionTokens,
			TotalTokens:      s.promptTokens + s.completionTokens,
			CachedTokens:     s.cachedTokens,
		},
	}
}

func handleAnthropicStreamEvent(event anthropic.MessageStreamEventUnion, state *anthropicStreamState) *ResponseEvent {
	if state == nil {
		return nil
	}

	switch variant := event.AsAny().(type) {
	case anthropic.MessageStartEvent:
		if variant.Message.ID != "" {
			state.responseID = variant.Message.ID
		}
		state.promptTokens = int(variant.Message.Usage.InputTokens)
		state.cachedTokens = int(variant.Message.Usage.CacheReadInputTokens)
		for _, block := range variant.Message.Content {
			applySDKContentBlockToState(state, block)
		}
		return nil

	case anthropic.ContentBlockStartEvent:
		cb := variant.ContentBlock
		switch cb.Type {
		case "text":
			if cb.Text != "" {
				state.appendText(cb.Text)
				return &ResponseEvent{Type: EventContentDelta, Delta: cb.Text, TextDelta: cb.Text}
			}
		case "thinking":
			if cb.Thinking != "" {
				state.appendThinking(cb.Thinking, cb.Signature)
				return &ResponseEvent{Type: EventThinkingDelta, ThinkingDelta: cb.Thinking}
			}
		case "tool_use":
			var inputBytes []byte
			if cb.Input != nil {
				inputBytes, _ = json.Marshal(cb.Input)
			}
			args := strings.TrimSpace(string(inputBytes))
			if args == "" {
				args = "{}"
			}
			state.activeTool = &ResponseOutput{
				ID:     cb.ID,
				Type:   "function_call",
				Status: "completed",
				CallID: cb.ID,
				Name:   cb.Name,
				Args:   args,
			}
		}
		return nil

	case anthropic.ContentBlockDeltaEvent:
		delta := variant.Delta
		switch delta.Type {
		case "text_delta":
			if delta.Text != "" {
				state.appendText(delta.Text)
				return &ResponseEvent{Type: EventContentDelta, Delta: delta.Text, TextDelta: delta.Text}
			}
		case "thinking_delta":
			if delta.Thinking != "" {
				state.appendThinking(delta.Thinking, "")
				return &ResponseEvent{Type: EventThinkingDelta, ThinkingDelta: delta.Thinking}
			}
		case "input_json_delta":
			if delta.PartialJSON != "" && state.activeTool != nil {
				state.activeTool.Args += delta.PartialJSON
			}
		}
		return nil

	case anthropic.ContentBlockStopEvent:
		if state.activeTool == nil {
			return nil
		}
		output := *state.activeTool
		state.outputs = append(state.outputs, output)
		state.activeTool = nil
		return &ResponseEvent{Type: EventToolCallDone, Output: &output}

	case anthropic.MessageDeltaEvent:
		state.completionTokens = int(variant.Usage.OutputTokens)
		return nil

	case anthropic.MessageStopEvent:
		state.completed = true
		return &ResponseEvent{Type: EventResponseCompleted, Response: state.response()}
	}

	return nil
}

func applySDKContentBlockToState(state *anthropicStreamState, block anthropic.ContentBlockUnion) {
	switch block.Type {
	case "text":
		state.appendText(block.Text)
	case "thinking":
		state.appendThinking(block.Thinking, block.Signature)
	case "tool_use":
		args := strings.TrimSpace(string(block.Input))
		if args == "" {
			args = "{}"
		}
		state.outputs = append(state.outputs, ResponseOutput{
			ID:     block.ID,
			Type:   "function_call",
			Status: "completed",
			CallID: block.ID,
			Name:   block.Name,
			Args:   args,
		})
	}
}

// --- Request builders ---

func buildAnthropicParams(req *ResponseRequest, cfg config.ProviderConfig) (anthropic.MessageNewParams, error) {
	maxTokens := req.RequestedMaxTokens()
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	messages, system, _ := buildAnthropicMessages(req.InputMessages())

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	if req.Options != nil {
		if req.Options.System != "" {
			params.System = []anthropic.TextBlockParam{{Text: req.Options.System}}
		}
		if req.Options.Thinking != nil {
			// TODO: map thinking config to SDK ThinkingConfigParamUnion
		}
	}

	if len(req.Tools) > 0 {
		tools := make([]anthropic.ToolUnionParam, 0, len(req.Tools))
		for _, tool := range req.Tools {
			toolMap, ok := tool.(map[string]any)
			if !ok {
				continue
			}
			funcMap, ok := toolMap["function"].(map[string]any)
			if !ok {
				continue
			}
			name, _ := funcMap["name"].(string)
			description, _ := funcMap["description"].(string)
			parameters, _ := funcMap["parameters"].(map[string]any)
			if name == "" {
				continue
			}

			toolSchema := anthropic.ToolInputSchemaParam{
				Properties: parameters["properties"],
			}
			if req, ok := parameters["required"].([]string); ok {
				toolSchema.Required = req
			} else if reqAny, ok := parameters["required"].([]any); ok {
				for _, v := range reqAny {
					if s, ok := v.(string); ok {
						toolSchema.Required = append(toolSchema.Required, s)
					}
				}
			}
			toolParam := anthropic.ToolUnionParamOfTool(toolSchema, name)
			toolParam.OfTool.Description = param.NewOpt(description)
			tools = append(tools, toolParam)
		}
		if len(tools) > 0 {
			params.Tools = tools
		}
	}

	extraBody := buildExtraBody(cfg)
	if err := mergeExtraBody(&params, extraBody); err != nil {
		return anthropic.MessageNewParams{}, fmt.Errorf("merge extra body: %w", err)
	}

	return params, nil
}

func buildAnthropicMessages(msgs []Message) ([]anthropic.MessageParam, string, error) {
	var systemParts []string
	messages := make([]anthropic.MessageParam, 0, len(msgs))

	for _, msg := range msgs {
		text := collectText(msg.Content)
		blocks := buildAnthropicContentBlocks(msg)

		switch msg.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolCallID, text, false),
			))
		case "user":
			fallthrough
		default:
			if len(blocks) == 0 {
				continue
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
		}
	}

	return messages, strings.Join(systemParts, "\n\n"), nil
}

func buildAnthropicContentBlocks(msg Message) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content)+len(msg.ToolCalls))
	for _, item := range msg.Content {
		block, ok := buildAnthropicTextBlock(item)
		if ok {
			blocks = append(blocks, block)
		}
	}
	for _, call := range msg.ToolCalls {
		var input any
		_ = json.Unmarshal([]byte(call.Function.Arguments), &input)
		blocks = append(blocks, anthropic.NewToolUseBlock(
			firstNonEmpty(call.ID, msg.ToolCallID),
			input,
			call.Function.Name,
		))
	}
	return blocks
}

func buildAnthropicTextBlock(value ContentBlock) (anthropic.ContentBlockParamUnion, bool) {
	switch value.Type {
	case "text", "output_text":
		if value.Text == "" {
			return anthropic.ContentBlockParamUnion{}, false
		}
		return anthropic.NewTextBlock(value.Text), true
	case "thinking":
		if value.Thinking == "" {
			return anthropic.ContentBlockParamUnion{}, false
		}
		return anthropic.NewThinkingBlock("", value.Thinking), true
	case "image":
		if value.Image == nil {
			return anthropic.ContentBlockParamUnion{}, false
		}
		if value.Image.URL != "" {
			return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: value.Image.URL}), true
		}
		if value.Image.Data != "" {
			return anthropic.NewImageBlockBase64(value.Image.MediaType, value.Image.Data), true
		}
	case "refusal":
		if value.Refusal == "" {
			return anthropic.ContentBlockParamUnion{}, false
		}
		return anthropic.NewTextBlock(value.Refusal), true
	case "structured_output":
		if value.Structured == nil {
			return anthropic.ContentBlockParamUnion{}, false
		}
		if len(value.Structured.Raw) > 0 {
			return anthropic.NewTextBlock(string(value.Structured.Raw)), true
		}
		if value.Structured.Data != nil {
			raw, _ := json.Marshal(value.Structured.Data)
			return anthropic.NewTextBlock(string(raw)), true
		}
		return anthropic.ContentBlockParamUnion{}, false
	default:
		return anthropic.ContentBlockParamUnion{}, false
	}
	return anthropic.ContentBlockParamUnion{}, false
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

func mustUnmarshalJSON(data string) map[string]any {
	var value map[string]any
	_ = json.Unmarshal([]byte(data), &value)
	return value
}
