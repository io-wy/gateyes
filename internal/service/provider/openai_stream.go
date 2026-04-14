package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (p *openAIProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		httpReq, err := p.newRequest(ctx, req, true)
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- newProviderTransportError("provider.openai.stream_response", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errCh <- newUpstreamStatusError(resp)
			return
		}

		reader := bufio.NewReader(resp.Body)
		var pendingData string
		var detectedFormat string

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					if pendingData != "" {
						event, parseErr := parseSSELine(pendingData, detectedFormat, req.Model)
						if parseErr != nil {
							errCh <- parseErr
							return
						}
						if event != nil {
							result <- *event
						}
					}
					return
				}
				errCh <- newProviderTransportError("provider.openai.read_stream", err)
				return
			}

			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					if pendingData != "" {
						event, parseErr := parseSSELine(pendingData, detectedFormat, req.Model)
						if parseErr != nil {
							errCh <- parseErr
							return
						}
						if event != nil {
							result <- *event
						}
					}
					return
				}
				if pendingData == "" {
					detectedFormat = detectStreamFormat(data)
				}
				if pendingData != "" {
					pendingData += "\n" + data
				} else {
					pendingData = data
				}
				continue
			}
			if line == "" && pendingData != "" {
				event, parseErr := parseSSELine(pendingData, detectedFormat, req.Model)
				if parseErr != nil {
					errCh <- parseErr
					return
				}
				if event != nil {
					result <- *event
				}
				pendingData = ""
			}
		}
	}()

	return result, errCh
}

func detectStreamFormat(data string) string {
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &typeCheck); err == nil && typeCheck.Type != "" {
		if strings.HasPrefix(typeCheck.Type, "response.") {
			return "responses"
		}
	}
	if strings.Contains(data, `"choices"`) {
		return "chat"
	}
	return "chat"
}

func parseSSELine(data, format, requestedModel string) (*ResponseEvent, error) {
	if format == "" {
		format = detectStreamFormat(data)
	}
	if format == "chat" || strings.Contains(data, `"choices"`) {
		event, err := parseChatCompletionEvent(data, requestedModel)
		if err != nil {
			return nil, nil
		}
		return event, nil
	}
	event, err := parseOpenAIStreamEvent(data, requestedModel)
	if err != nil {
		return nil, nil
	}
	return event, nil
}

func parseOpenAIStreamEvent(data string, requestedModel string) (*ResponseEvent, error) {
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &typeCheck); err != nil {
		return nil, err
	}

	switch typeCheck.Type {
	case "response.output_text.delta":
		var payload struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		return &ResponseEvent{
			Type:      EventContentDelta,
			Delta:     payload.Delta,
			TextDelta: payload.Delta,
		}, nil
	case "response.output_item.done":
		var payload struct {
			Type       string           `json:"type"`
			Item       openAIOutputItem `json:"item"`
			OutputItem openAIOutputItem `json:"output_item"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		item := payload.Item
		if item.Type == "" {
			item = payload.OutputItem
		}
		output := convertOpenAIOutputItem(item)
		if output == nil {
			return nil, nil
		}
		return &ResponseEvent{
			Type:   EventToolCallDone,
			Output: output,
		}, nil
	case "response.completed":
		var payload struct {
			Type     string                `json:"type"`
			Response openAIResponsePayload `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		return &ResponseEvent{
			Type:     EventResponseCompleted,
			Response: convertOpenAIResponse(payload.Response, requestedModel),
		}, nil
	case "response.failed":
		var payload struct {
			Response struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, err
		}
		if payload.Response.Error.Message == "" {
			payload.Response.Error.Message = "upstream response failed"
		}
		return nil, newProviderUpstreamMessageError(payload.Response.Error.Message)
	default:
		return parseChatCompletionEvent(data, requestedModel)
	}
}

// parseOpenAIResponseEvent is an alias kept for compatibility with existing tests.
func parseOpenAIResponseEvent(data string, requestedModel string) (*ResponseEvent, error) {
	return parseOpenAIStreamEvent(data, requestedModel)
}

func parseChatCompletionEvent(data string, requestedModel string) (*ResponseEvent, error) {
	var chunk struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index int `json:"index"`
			Delta struct {
				Content   any    `json:"content"`
				Role      string `json:"role"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name string `json:"name"`
						Args string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
				FinishReason string `json:"finish_reason"`
			} `json:"delta"`
			Message struct {
				Role      string `json:"role"`
				Content   any    `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name string `json:"name"`
						Args string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Text         string `json:"text"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, err
	}
	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	choice := chunk.Choices[0]
	delta := choice.Delta
	text := extractChatCompletionText(choice)
	toolCalls := extractChatCompletionToolCalls(choice)
	if text == "" && len(toolCalls) == 0 && delta.FinishReason == "" && choice.FinishReason == "" && chunk.Usage.TotalTokens == 0 {
		return nil, nil
	}

	event := ResponseEvent{
		Type:      EventContentDelta,
		Delta:     text,
		TextDelta: text,
	}
	if len(toolCalls) > 0 {
		event.ToolCalls = toolCalls
	}
	if delta.FinishReason != "" {
		event.FinishReason = delta.FinishReason
	} else if choice.FinishReason != "" {
		event.FinishReason = choice.FinishReason
	}
	if chunk.Usage.TotalTokens > 0 {
		event.Usage = &Usage{
			PromptTokens:     chunk.Usage.PromptTokens,
			CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens:      chunk.Usage.TotalTokens,
		}
	}
	return &event, nil
}

func extractChatCompletionText(choice struct {
	Index int `json:"index"`
	Delta struct {
		Content   any    `json:"content"`
		Role      string `json:"role"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
				Args string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		FinishReason string `json:"finish_reason"`
	} `json:"delta"`
	Message struct {
		Role      string `json:"role"`
		Content   any    `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
				Args string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
}) string {
	if text := collectText(choice.Delta.Content); text != "" {
		return text
	}
	if text := collectText(choice.Message.Content); text != "" {
		return text
	}
	return choice.Text
}

func extractChatCompletionToolCalls(choice struct {
	Index int `json:"index"`
	Delta struct {
		Content   any    `json:"content"`
		Role      string `json:"role"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
				Args string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		FinishReason string `json:"finish_reason"`
	} `json:"delta"`
	Message struct {
		Role      string `json:"role"`
		Content   any    `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
				Args string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
}) []ToolCall {
	source := choice.Delta.ToolCalls
	if len(source) == 0 {
		source = choice.Message.ToolCalls
	}
	result := make([]ToolCall, 0, len(source))
	for _, tc := range source {
		result = append(result, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Args,
			},
		})
	}
	return result
}
