package provider

import (
	"encoding/json"
	"strings"
	"time"
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

type anthropicResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	Role       string `json:"role"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type      string           `json:"type"`
		Text      string           `json:"text"`
		ID        string           `json:"id"`
		Name      string           `json:"name"`
		Input     json.RawMessage  `json:"input"`
		Source    *AnthropicSource `json:"source"`
		Thinking  string           `json:"thinking"`
		Signature string           `json:"signature"`
	} `json:"content"`
	Usage struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheHitInputTokens int `json:"cache_hit_input_tokens"`
	} `json:"usage"`
}

func convertAnthropicResponse(raw anthropicResponse, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = raw.Model
	}

	newContent := make([]AnthropicContentBlock, len(raw.Content))
	for i, block := range raw.Content {
		newContent[i] = AnthropicContentBlock{
			Type:      block.Type,
			Text:      block.Text,
			ID:        block.ID,
			Name:      block.Name,
			Input:     block.Input,
			Source:    block.Source,
			Thinking:  block.Thinking,
			Signature: block.Signature,
		}
	}

	return &Response{
		ID:      raw.ID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  convertAnthropicOutputs(raw.Role, newContent),
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
			CachedTokens:     raw.Usage.CacheHitInputTokens,
		},
	}
}

var _ Provider = (*anthropicProvider)(nil)
