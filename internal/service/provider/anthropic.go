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
	Role    string `json:"role"`
	Content string `json:"content"`
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
		Type string `json:"type"`
		Text string `json:"text"`
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

func (p *anthropicProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
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

	var builder strings.Builder
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			builder.WriteString(block.Text)
		}
	}

	return &ChatResponse{
		ID:      anthropicResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   anthropicResp.Model,
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role:    "assistant",
				Content: builder.String(),
			},
			FinishReason: anthropicResp.StopReason,
		}},
		Usage: Usage{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}, nil
}

func (p *anthropicProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan string, <-chan error) {
	result := make(chan string)
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
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
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

			if currentEvent != "content_block_delta" {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var event struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}
			if event.Delta.Text == "" {
				continue
			}

			chunk, err := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{
						"content": event.Delta.Text,
					},
				}},
			})
			if err != nil {
				errCh <- err
				return
			}
			result <- string(chunk)
		}
	}()

	return result, errCh
}

func (p *anthropicProvider) buildRequest(req *ChatRequest, stream bool) (*anthropicRequest, error) {
	systemParts := make([]string, 0)
	messages := make([]anthropicMessage, 0, len(req.Messages))

	for _, message := range req.Messages {
		switch message.Role {
		case "system", "developer":
			systemParts = append(systemParts, message.Content)
		case "assistant", "user":
			messages = append(messages, anthropicMessage{Role: message.Role, Content: message.Content})
		default:
			messages = append(messages, anthropicMessage{Role: "user", Content: message.Content})
		}
	}

	maxTokens := req.MaxTokens
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
