package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func (p *anthropicProvider) parseStream(body io.Reader, result chan<- ResponseEvent, errCh chan<- error, model string) {
	reader := bufio.NewReader(body)
	responseID := "stream-" + uuid()
	state := &anthropicStreamState{
		responseID: responseID,
		model:      model,
	}
	var eventName string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if event := parseAnthropicStreamEvent(eventName, strings.Join(dataLines, "\n"), state); event != nil {
					result <- *event
				}
				if !state.completed {
					result <- ResponseEvent{Type: EventResponseCompleted, Response: state.response()}
				}
				break
			}
			errCh <- newProviderTransportError("provider.anthropic.read_stream", err)
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			data := strings.Join(dataLines, "\n")
			if data == "[DONE]" {
				return
			}
			if strings.Contains(data, "\"type\":\"error\"") || strings.Contains(data, "\"error\":") {
				var errResp struct {
					Type  string `json:"type"`
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal([]byte(data), &errResp) == nil && errResp.Error.Message != "" {
					errCh <- newUpstreamError(0, errResp.Error.Message)
					return
				}
			}
			if event := parseAnthropicStreamEvent(eventName, data, state); event != nil {
				result <- *event
			}
			eventName = ""
			dataLines = nil
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func uuid() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func parseAnthropicStreamEvent(eventName, data string, state *anthropicStreamState) *ResponseEvent {
	if state == nil || data == "" {
		return nil
	}

	var event struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(data), &event) != nil && eventName == "" {
		return nil
	}
	if eventName == "" {
		eventName = event.Type
	}

	switch eventName {
	case "message_start":
		var payload struct {
			Message struct {
				ID      string                  `json:"id"`
				Content []AnthropicContentBlock `json:"content"`
				Usage   struct {
					InputTokens         int `json:"input_tokens"`
					OutputTokens        int `json:"output_tokens"`
					CacheHitInputTokens int `json:"cache_hit_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		if payload.Message.ID != "" {
			state.responseID = payload.Message.ID
		}
		state.promptTokens = payload.Message.Usage.InputTokens
		state.cachedTokens = payload.Message.Usage.CacheHitInputTokens
		for _, block := range payload.Message.Content {
			state.applyContentBlock(block)
		}
		return nil

	case "content_block_start":
		var payload struct {
			Index        int                   `json:"index"`
			ContentBlock AnthropicContentBlock `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		switch payload.ContentBlock.Type {
		case "text":
			if payload.ContentBlock.Text != "" {
				state.appendText(payload.ContentBlock.Text)
				return &ResponseEvent{Type: EventContentDelta, Delta: payload.ContentBlock.Text, TextDelta: payload.ContentBlock.Text}
			}
		case "thinking":
			if payload.ContentBlock.Thinking != "" {
				state.appendThinking(payload.ContentBlock.Thinking, payload.ContentBlock.Signature)
				return &ResponseEvent{Type: EventThinkingDelta, ThinkingDelta: payload.ContentBlock.Thinking}
			}
		case "tool_use":
			args := strings.TrimSpace(string(payload.ContentBlock.Input))
			if args == "" {
				args = "{}"
			}
			state.activeTool = &ResponseOutput{
				ID:     payload.ContentBlock.ID,
				Type:   "function_call",
				Status: "completed",
				CallID: payload.ContentBlock.ID,
				Name:   payload.ContentBlock.Name,
				Args:   args,
			}
		}
		return nil

	case "content_block_delta":
		var payload struct {
			Index int `json:"index"`
			Delta any `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		if deltaMap, ok := payload.Delta.(map[string]any); ok {
			if deltaType, _ := deltaMap["type"].(string); deltaType == "text_delta" {
				if text, _ := deltaMap["text"].(string); text != "" {
					state.appendText(text)
					return &ResponseEvent{Type: EventContentDelta, Delta: text, TextDelta: text}
				}
			}
			if text, _ := deltaMap["text"].(string); text != "" {
				state.appendText(text)
				return &ResponseEvent{Type: EventContentDelta, Delta: text, TextDelta: text}
			}
			if thinking, _ := deltaMap["thinking"].(string); thinking != "" {
				state.appendThinking(thinking, "")
				return &ResponseEvent{Type: EventThinkingDelta, ThinkingDelta: thinking}
			}
			if deltaType, _ := deltaMap["type"].(string); deltaType == "thinking_delta" {
				if thinking, _ := deltaMap["thinking"].(string); thinking != "" {
					state.appendThinking(thinking, "")
					return &ResponseEvent{Type: EventThinkingDelta, ThinkingDelta: thinking}
				}
			}
			if partial, _ := deltaMap["partial_json"].(string); partial != "" && state.activeTool != nil {
				state.activeTool.Args += partial
			}
		}
		return nil

	case "content_block_stop":
		if state.activeTool == nil {
			return nil
		}
		output := *state.activeTool
		state.outputs = append(state.outputs, output)
		state.activeTool = nil
		return &ResponseEvent{Type: EventToolCallDone, Output: &output}

	case "message_delta":
		var payload struct {
			Delta any `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		if deltaMap, ok := payload.Delta.(map[string]any); ok {
			if text, _ := deltaMap["text"].(string); text != "" {
				state.appendText(text)
				return &ResponseEvent{Type: EventContentDelta, Delta: text, TextDelta: text}
			}
		}
		if payload.Content != "" {
			state.appendText(payload.Content)
			return &ResponseEvent{Type: EventContentDelta, Delta: payload.Content, TextDelta: payload.Content}
		}
		if payload.Usage.OutputTokens > 0 {
			state.completionTokens = payload.Usage.OutputTokens
		}
		return nil

	case "message_stop":
		state.completed = true
		return &ResponseEvent{Type: EventResponseCompleted, Response: state.response()}

	case "ping":
		return nil

	case "text_block", "content_block":
		var payload struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(data), &payload) == nil && payload.Text != "" {
			state.appendText(payload.Text)
			return &ResponseEvent{Type: EventContentDelta, Delta: payload.Text, TextDelta: payload.Text}
		}
	}

	return nil
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
