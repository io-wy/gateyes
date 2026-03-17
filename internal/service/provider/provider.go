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

type Manager struct {
	providers map[string]*Provider
	defaultProvider string
	Stats      *Stats
}

type Provider struct {
	Name     string
	Type     string
	BaseURL  string
	APIKey   string
	Model    string
	Weight   int
	PriceIn  float64
	PriceOut float64
	MaxTokens int
	Timeout  time.Duration
	client   *http.Client
}

type ChatRequest struct {
	Model    string                  `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
	Stream   bool                    `json:"stream,omitempty"`
	MaxTokens int                   `json:"max_tokens,omitempty"`
}

type ChatResponse struct {
	ID      string `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage `json:"usage"`
}

type Choice struct {
	Message map[string]interface{} `json:"message"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type StreamChunk struct {
	Choices []map[string]interface{} `json:"choices"`
	Done    bool `json:"done"`
}

func NewManager(cfg []config.ProviderConfig) *Manager {
	m := &Manager{
		providers: make(map[string]*Provider),
		Stats:     NewStats(),
	}
	for _, p := range cfg {
		provider := &Provider{
			Name:     p.Name,
			Type:     p.Type,
			BaseURL:  p.BaseURL,
			APIKey:   p.APIKey,
			Model:    p.Model,
			Weight:   p.Weight,
			PriceIn:  p.PriceInput,
			PriceOut: p.PriceOutput,
			MaxTokens: p.MaxTokens,
			Timeout:  time.Duration(p.Timeout) * time.Second,
			client:   &http.Client{Timeout: time.Duration(p.Timeout) * time.Second},
		}
		m.providers[p.Name] = provider
		m.Stats.Register(provider)
		if m.defaultProvider == "" {
			m.defaultProvider = p.Name
		}
	}
	return m
}

func (m *Manager) Get(name string) (*Provider, bool) {
	p, ok := m.providers[name]
	return p, ok
}

func (m *Manager) GetDefault() *Provider {
	return m.providers[m.defaultProvider]
}

func (m *Manager) List() []*Provider {
	result := make([]*Provider, 0, len(m.providers))
	for _, p := range m.providers {
		result = append(result, p)
	}
	return result
}

func (p *Provider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, string(b))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, err
	}
	return &chatResp, nil
}

func (p *Provider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan string, <-chan error) {
	result := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		body, _ := json.Marshal(req)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("upstream error: %d %s", resp.StatusCode, string(b))
			return
		}

		// 使用 bufio.Reader 逐行读取 SSE
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				errCh <- err
				return
			}

			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}

			// 提取 data: 后面的 JSON
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			// 解析 JSON
			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // 跳过无效的 JSON
			}

			if len(chunk.Choices) > 0 || chunk.Done {
				result <- data
			}

			if chunk.Done {
				break
			}
		}
	}()

	return result, errCh
}

func (p *Provider) Cost(promptTokens, completionTokens int) float64 {
	return float64(promptTokens)*p.PriceIn + float64(completionTokens)*p.PriceOut
}

// ============ OpenAI Responses API Types ============

type ResponsesRequest struct {
	Model    string         `json:"model"`
	Messages []ResponseMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type ResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Type    string `json:"type,omitempty"`
}

type ResponsesResponse struct {
	ID        string               `json:"id"`
	Object    string               `json:"object"`
	Created   int64                `json:"created"`
	Model     string               `json:"model"`
	Choices   []ResponsesChoice    `json:"choices"`
	Usage     Usage                `json:"usage"`
}

type ResponsesChoice struct {
	Index        int                 `json:"index"`
	Message      ResponseMessage      `json:"message"`
	FinishReason string               `json:"finish_reason"`
}

// Convert ChatResponse to Responses API format
func ConvertToResponses(chatResp *ChatResponse) *ResponsesResponse {
	var choices []ResponsesChoice
	for i, c := range chatResp.Choices {
		msg := ResponseMessage{}
		if msgMap, ok := c.Message["content"].(string); ok {
			msg.Content = msgMap
		}
		if roleMap, ok := c.Message["role"].(string); ok {
			msg.Role = roleMap
		}

		finishReason := "stop"
		if finish, ok := c.Message["finish_reason"].(string); ok {
			finishReason = finish
		}

		choices = append(choices, ResponsesChoice{
			Index:        i,
			Message:      msg,
			FinishReason: finishReason,
		})
	}

	return &ResponsesResponse{
		ID:        chatResp.ID,
		Object:    "chat.completion",
		Created:   time.Now().Unix(),
		Model:     chatResp.Choices[0].Message["model"].(string),
		Choices:   choices,
		Usage:     chatResp.Usage,
	}
}
