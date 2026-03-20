package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

const anthropicVersion = "2023-06-01"

type anthropicProvider struct {
	cfg    config.ProviderConfig
	client *http.Client
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	Role       string `json:"role"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func NewAnthropicProvider(cfg config.ProviderConfig) Provider {
	return &anthropicProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}
}

func (p *anthropicProvider) Name() string      { return p.cfg.Name }
func (p *anthropicProvider) Type() string      { return p.cfg.Type }
func (p *anthropicProvider) BaseURL() string   { return p.cfg.BaseURL }
func (p *anthropicProvider) Model() string     { return p.cfg.Model }
func (p *anthropicProvider) UnitCost() float64 { return p.cfg.PriceInput + p.cfg.PriceOutput }
func (p *anthropicProvider) Cost(prompt, completion int) float64 {
	return float64(prompt)*p.cfg.PriceInput + float64(completion)*p.cfg.PriceOutput
}

func (p *anthropicProvider) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	payload, err := p.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	resp, err := p.doRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, string(data))
	}

	var anthropicResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, err
	}

	return convertAnthropicResponse(anthropicResp, req.Model), nil
}

func (p *anthropicProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		payload, err := p.buildRequest(req, true)
		if err != nil {
			errCh <- err
			return
		}

		resp, err := p.doRequest(ctx, payload)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("upstream error: %d %s", resp.StatusCode, string(data))
			return
		}

		reader := bufio.NewReader(resp.Body)
		currentEvent := ""
		responseID := ""
		model := req.Model
		promptTokens := 0
		completionTokens := 0
		outputs := make([]ResponseOutput, 0, 4)
		textOutputIndex := -1
		var currentTool *ResponseOutput
		var currentToolArgs strings.Builder

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					if responseID != "" {
						result <- ResponseEvent{
							Type: "response.completed",
							Response: buildAnthropicStreamResponse(
								responseID,
								model,
								outputs,
								promptTokens,
								completionTokens,
							),
						}
					}
					return
				}
				errCh <- err
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				currentEvent = ""
				continue
			}

			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			switch currentEvent {
			case "message_start":
				var event struct {
					Message struct {
						ID    string `json:"id"`
						Model string `json:"model"`
						Usage struct {
							InputTokens int `json:"input_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(raw), &event); err != nil {
					continue
				}
				responseID = event.Message.ID
				if event.Message.Model != "" {
					model = event.Message.Model
				}
				promptTokens = event.Message.Usage.InputTokens
			case "content_block_start":
				var event struct {
					ContentBlock struct {
						Type  string          `json:"type"`
						ID    string          `json:"id"`
						Name  string          `json:"name"`
						Text  string          `json:"text"`
						Input json.RawMessage `json:"input"`
					} `json:"content_block"`
				}
				if err := json.Unmarshal([]byte(raw), &event); err != nil {
					continue
				}
				switch event.ContentBlock.Type {
				case "text":
					if textOutputIndex < 0 {
						outputs = append(outputs, ResponseOutput{
							Type:   "message",
							Role:   "assistant",
							Status: "completed",
						})
						textOutputIndex = len(outputs) - 1
					}
					if event.ContentBlock.Text != "" {
						outputs[textOutputIndex].Content = append(outputs[textOutputIndex].Content, ResponseContent{
							Type: "output_text",
							Text: event.ContentBlock.Text,
						})
						result <- ResponseEvent{
							Type:  "response.output_text.delta",
							Delta: event.ContentBlock.Text,
						}
					}
				case "tool_use":
					textOutputIndex = -1
					currentToolArgs.Reset()
					if len(event.ContentBlock.Input) > 0 {
						currentToolArgs.Write(event.ContentBlock.Input)
					}
					currentTool = &ResponseOutput{
						ID:     event.ContentBlock.ID,
						Type:   "function_call",
						Status: "completed",
						CallID: event.ContentBlock.ID,
						Name:   event.ContentBlock.Name,
					}
				}
			case "content_block_delta":
				var event struct {
					Delta struct {
						Text        string `json:"text"`
						PartialJSON string `json:"partial_json"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(raw), &event); err != nil {
					continue
				}
				if event.Delta.Text == "" {
					if currentTool != nil && event.Delta.PartialJSON != "" {
						currentToolArgs.WriteString(event.Delta.PartialJSON)
					}
					continue
				}
				if textOutputIndex < 0 {
					outputs = append(outputs, ResponseOutput{
						Type:   "message",
						Role:   "assistant",
						Status: "completed",
					})
					textOutputIndex = len(outputs) - 1
				}
				if len(outputs[textOutputIndex].Content) == 0 {
					outputs[textOutputIndex].Content = append(outputs[textOutputIndex].Content, ResponseContent{
						Type: "output_text",
						Text: event.Delta.Text,
					})
				} else {
					last := len(outputs[textOutputIndex].Content) - 1
					outputs[textOutputIndex].Content[last].Text += event.Delta.Text
				}
				result <- ResponseEvent{
					Type:  "response.output_text.delta",
					Delta: event.Delta.Text,
				}
			case "content_block_stop":
				if currentTool == nil {
					continue
				}
				currentTool.Args = currentToolArgs.String()
				outputs = append(outputs, *currentTool)
				toolOutput := outputs[len(outputs)-1]
				result <- ResponseEvent{
					Type:   "response.output_item.done",
					Output: &toolOutput,
				}
				currentTool = nil
				currentToolArgs.Reset()
			case "message_delta":
				var event struct {
					Usage struct {
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				}
				if err := json.Unmarshal([]byte(raw), &event); err != nil {
					continue
				}
				if event.Usage.OutputTokens > 0 {
					completionTokens = event.Usage.OutputTokens
				}
			case "message_stop":
				if completionTokens == 0 {
					completionTokens = RoughTokenCount(renderOutputSignature(outputs))
				}
				result <- ResponseEvent{
					Type: "response.completed",
					Response: buildAnthropicStreamResponse(
						responseID,
						model,
						outputs,
						promptTokens,
						completionTokens,
					),
				}
				return
			case "error":
				var event struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(raw), &event); err != nil {
					errCh <- fmt.Errorf("upstream error")
					return
				}
				errCh <- fmt.Errorf("upstream error: %s", event.Error.Message)
				return
			}
		}
	}()

	return result, errCh
}

func (p *anthropicProvider) buildRequest(req *ResponseRequest, stream bool) (*anthropicRequest, error) {
	systemParts := make([]string, 0)
	messages := make([]anthropicMessage, 0, len(req.InputMessages()))

	for _, message := range req.InputMessages() {
		text := collectText(message.Content)
		switch message.Role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			blocks := buildAnthropicBlocks(message)
			if len(blocks) > 0 {
				messages = append(messages, anthropicMessage{Role: "assistant", Content: blocks})
			}
		case "tool":
			if message.ToolCallID == "" {
				continue
			}
			messages = append(messages, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: message.ToolCallID,
					Content:   text,
				}},
			})
		case "user":
			blocks := buildAnthropicBlocks(message)
			if len(blocks) > 0 {
				messages = append(messages, anthropicMessage{Role: "user", Content: blocks})
			}
		default:
			if text == "" {
				continue
			}
			messages = append(messages, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type: "text",
					Text: text,
				}},
			})
		}
	}

	maxTokens := req.RequestedMaxTokens()
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	return &anthropicRequest{
		Model:     req.Model,
		System:    strings.Join(systemParts, "\n\n"),
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    stream,
	}, nil
}

func (p *anthropicProvider) doRequest(ctx context.Context, payload *anthropicRequest) (*http.Response, error) {
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if payload.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	return p.client.Do(httpReq)
}

func convertAnthropicResponse(raw anthropicResponse, requestedModel string) *Response {
	model := requestedModel
	if model == "" {
		model = raw.Model
	}

	return &Response{
		ID:      raw.ID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  convertAnthropicOutputs(raw.Role, raw.Content),
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
		},
	}
}

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
		},
	}
}

func buildAnthropicBlocks(message Message) []anthropicContentBlock {
	blocks := make([]anthropicContentBlock, 0, len(message.ToolCalls)+1)
	switch current := message.Content.(type) {
	case string:
		if current != "" {
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: current})
		}
	case []any:
		for _, item := range current {
			block, ok := buildAnthropicTextBlock(item)
			if ok {
				blocks = append(blocks, block)
			}
		}
	case map[string]any:
		block, ok := buildAnthropicTextBlock(current)
		if ok {
			blocks = append(blocks, block)
		}
	default:
		if text := collectText(current); text != "" {
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: text})
		}
	}

	for _, call := range message.ToolCalls {
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    firstNonEmpty(call.ID, message.ToolCallID),
			Name:  call.Function.Name,
			Input: marshalRawJSON(call.Function.Arguments),
		})
	}
	return blocks
}

func buildAnthropicTextBlock(value any) (anthropicContentBlock, bool) {
	current, ok := value.(map[string]any)
	if !ok {
		text := collectText(value)
		if text == "" {
			return anthropicContentBlock{}, false
		}
		return anthropicContentBlock{Type: "text", Text: text}, true
	}
	text := collectText(current)
	if text == "" {
		return anthropicContentBlock{}, false
	}
	return anthropicContentBlock{Type: "text", Text: text}, true
}

func convertAnthropicOutputs(role string, blocks []struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}) []ResponseOutput {
	outputs := make([]ResponseOutput, 0, len(blocks))
	messageOutputIndex := -1
	for _, block := range blocks {
		switch block.Type {
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
