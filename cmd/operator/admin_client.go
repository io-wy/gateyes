package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/platform"
)

type adminSyncClient struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

func newAdminSyncClient(rawURL string, token string) (*adminSyncClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("admin-url must include scheme and host")
	}
	return &adminSyncClient{
		baseURL: parsed,
		token:   token,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

func (c *adminSyncClient) SyncProvider(target platform.ProviderSyncTarget) error {
	if c == nil {
		return fmt.Errorf("admin client is nil")
	}
	name := strings.TrimSpace(target.Provider.Name)
	if name == "" {
		return fmt.Errorf("provider name is required")
	}

	status, _, err := c.do(http.MethodGet, "/admin/v1/providers/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		return c.writeProvider(http.MethodPut, "/admin/v1/providers/"+url.PathEscape(name), target.Provider)
	case http.StatusNotFound:
		return c.writeProvider(http.MethodPost, "/admin/v1/providers", target.Provider)
	default:
		return fmt.Errorf("get provider %s: unexpected status %d", name, status)
	}
}

func (c *adminSyncClient) SyncRouter(router config.RouterConfig) error {
	status, body, err := c.do(http.MethodPost, "/admin/v1/sync/router", router)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("sync router: unexpected status %d: %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *adminSyncClient) SyncBudget(budget platform.BudgetSyncTarget) error {
	payload := budgetSyncPayload{
		SubjectKind:     budget.SubjectKind,
		SubjectName:     budget.SubjectName,
		BudgetUSD:       budget.BudgetUSD,
		BudgetPolicy:    budget.BudgetPolicy,
		RateLimitQPS:    budget.RateLimitQPS,
		MonthlyTokens:   budget.MonthlyTokens,
		AlertThresholds: budget.AlertThresholds,
	}
	status, body, err := c.do(http.MethodPost, "/admin/v1/sync/budget", payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("sync budget %s/%s: unexpected status %d: %s", budget.SubjectKind, budget.SubjectName, status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *adminSyncClient) writeProvider(method string, endpoint string, provider config.ProviderConfig) error {
	payload := providerSyncPayload{
		Name:          provider.Name,
		Type:          provider.Type,
		Vendor:        provider.Vendor,
		BaseURL:       provider.BaseURL,
		Endpoint:      provider.Endpoint,
		APIKey:        provider.APIKey,
		Model:         provider.Model,
		RoutingWeight: provider.Weight,
		PriceInput:    provider.PriceInput,
		PriceOutput:   provider.PriceOutput,
		MaxTokens:     provider.MaxTokens,
		Timeout:       provider.Timeout,
		Enabled:       provider.Enabled,
		Headers:       provider.Headers,
		ExtraBody:     provider.ExtraBody,
		Labels:        provider.Labels,
	}
	applyCapabilities(&payload, provider.Capabilities)

	status, body, err := c.do(method, endpoint, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s %s: unexpected status %d: %s", method, endpoint, status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *adminSyncClient) do(method string, endpoint string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, c.endpointURL(endpoint), body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(c.token) != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func (c *adminSyncClient) endpointURL(endpoint string) string {
	next := *c.baseURL
	next.Path = path.Join(c.baseURL.Path, endpoint)
	return next.String()
}

type providerSyncPayload struct {
	Name                     string            `json:"name,omitempty"`
	Type                     string            `json:"type,omitempty"`
	Vendor                   string            `json:"vendor,omitempty"`
	BaseURL                  string            `json:"base_url,omitempty"`
	Endpoint                 string            `json:"endpoint,omitempty"`
	APIKey                   string            `json:"api_key,omitempty"`
	Model                    string            `json:"model,omitempty"`
	RoutingWeight            int               `json:"routing_weight,omitempty"`
	PriceInput               float64           `json:"price_input,omitempty"`
	PriceOutput              float64           `json:"price_output,omitempty"`
	MaxTokens                int               `json:"max_tokens,omitempty"`
	Timeout                  int               `json:"timeout,omitempty"`
	Enabled                  bool              `json:"enabled"`
	Headers                  map[string]string `json:"headers,omitempty"`
	ExtraBody                map[string]any    `json:"extra_body,omitempty"`
	Labels                   map[string]string `json:"labels,omitempty"`
	SupportsChat             *bool             `json:"supports_chat,omitempty"`
	SupportsResponses        *bool             `json:"supports_responses,omitempty"`
	SupportsMessages         *bool             `json:"supports_messages,omitempty"`
	SupportsStream           *bool             `json:"supports_stream,omitempty"`
	SupportsTools            *bool             `json:"supports_tools,omitempty"`
	SupportsImages           *bool             `json:"supports_images,omitempty"`
	SupportsStructuredOutput *bool             `json:"supports_structured_output,omitempty"`
	SupportsLongContext      *bool             `json:"supports_long_context,omitempty"`
	SupportsEmbeddings       *bool             `json:"supports_embeddings,omitempty"`
}

type budgetSyncPayload struct {
	SubjectKind     string    `json:"subject_kind"`
	SubjectName     string    `json:"subject_name"`
	BudgetUSD       float64   `json:"budget_usd"`
	BudgetPolicy    string    `json:"budget_policy"`
	RateLimitQPS    int       `json:"rate_limit_qps"`
	MonthlyTokens   int64     `json:"monthly_tokens"`
	AlertThresholds []float64 `json:"alert_thresholds"`
}

func applyCapabilities(payload *providerSyncPayload, caps config.ProviderCapabilitiesConfig) {
	payload.SupportsChat = caps.Chat
	payload.SupportsResponses = caps.Responses
	payload.SupportsMessages = caps.Messages
	payload.SupportsStream = caps.Stream
	payload.SupportsTools = caps.Tools
	payload.SupportsImages = caps.Images
	payload.SupportsStructuredOutput = caps.StructuredOutput
	payload.SupportsLongContext = caps.LongContext
	payload.SupportsEmbeddings = caps.Embeddings
}
