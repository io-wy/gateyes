package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

type tokenUsage struct {
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

type quotaSubject struct {
	User       string
	VirtualKey string
	Tenant     string
	Model      string
	Provider   string
	IP         string
	Path       string
}

func estimateTokenUsage(req *http.Request, defaultCompletion int) tokenUsage {
	body, err := readAndRestoreRequestBody(req)
	if err != nil || len(body) == 0 {
		return tokenUsage{}
	}

	payload := map[string]interface{}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return tokenUsage{}
	}

	model := strings.TrimSpace(toString(payload["model"]))
	promptTokens := estimatePromptTokens(payload)
	completionTokens := estimateCompletionTokens(payload, defaultCompletion)
	totalTokens := promptTokens + completionTokens
	if totalTokens < 0 {
		totalTokens = 0
	}

	return tokenUsage{
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

func usageFromRequestMeta(req *http.Request) tokenUsage {
	prompt := parseInt64Header(req.Header.Get(requestmeta.HeaderUsagePromptTokens))
	completion := parseInt64Header(req.Header.Get(requestmeta.HeaderUsageCompletionTokens))
	total := parseInt64Header(req.Header.Get(requestmeta.HeaderUsageTotalTokens))
	if total <= 0 && (prompt > 0 || completion > 0) {
		total = prompt + completion
	}

	return tokenUsage{
		Model:            strings.TrimSpace(req.Header.Get(requestmeta.HeaderResolvedModel)),
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	}
}

func clearInternalUsageHeaders(req *http.Request) {
	req.Header.Del(requestmeta.HeaderResolvedProvider)
	req.Header.Del(requestmeta.HeaderResolvedModel)
	req.Header.Del(requestmeta.HeaderUsagePromptTokens)
	req.Header.Del(requestmeta.HeaderUsageCompletionTokens)
	req.Header.Del(requestmeta.HeaderUsageTotalTokens)
}

func resolveQuotaSubject(
	req *http.Request,
	authCfg config.AuthConfig,
	tenantHeader string,
	fallbackModel string,
) quotaSubject {
	token := strings.TrimSpace(extractToken(req, authCfg.Header, authCfg.QueryParam))
	virtualKey := ""
	if token != "" {
		if virtualCfg, ok := authCfg.VirtualKeys[token]; ok && virtualCfg.Enabled {
			virtualKey = token
		}
	}

	tenantValue := strings.TrimSpace(req.Header.Get(tenantHeader))
	if tenantValue == "" {
		tenantValue = strings.TrimSpace(req.Header.Get("X-Tenant-ID"))
	}

	model := strings.TrimSpace(fallbackModel)
	if model == "" {
		model = strings.TrimSpace(req.Header.Get(requestmeta.HeaderResolvedModel))
	}

	provider := strings.TrimSpace(req.Header.Get(requestmeta.HeaderResolvedProvider))
	if provider == "" {
		provider = strings.TrimSpace(req.Header.Get("X-Gateyes-Provider"))
	}
	if provider == "" {
		provider = strings.TrimSpace(req.URL.Query().Get("provider"))
	}

	return quotaSubject{
		User:       token,
		VirtualKey: virtualKey,
		Tenant:     tenantValue,
		Model:      model,
		Provider:   provider,
		IP:         normalizeRemoteAddr(req.RemoteAddr),
		Path:       req.URL.Path,
	}
}

func normalizeDimensionList(dimensions []string) []string {
	if len(dimensions) == 0 {
		return []string{"user"}
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(dimensions))
	for _, dim := range dimensions {
		value := strings.ToLower(strings.TrimSpace(dim))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return []string{"user"}
	}
	return out
}

func buildDimensionKey(dimensions []string, subject quotaSubject) string {
	parts := make([]string, 0, len(dimensions))
	for _, dim := range normalizeDimensionList(dimensions) {
		parts = append(parts, dim+"="+sanitizeDimensionValue(dimensionValue(dim, subject)))
	}
	return strings.Join(parts, "|")
}

func dimensionValue(dim string, subject quotaSubject) string {
	switch dim {
	case "user":
		return subject.User
	case "virtual_key":
		return subject.VirtualKey
	case "tenant":
		return subject.Tenant
	case "model":
		return subject.Model
	case "provider":
		return subject.Provider
	case "ip":
		return subject.IP
	case "path":
		return subject.Path
	default:
		return ""
	}
}

func sanitizeDimensionValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	trimmed = strings.ReplaceAll(trimmed, "|", "_")
	trimmed = strings.ReplaceAll(trimmed, "=", "_")
	return trimmed
}

func normalizeRemoteAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func estimatePromptTokens(payload map[string]interface{}) int64 {
	var total int64
	if messages, ok := payload["messages"]; ok {
		total += estimateTokensFromValue(messages)
	}
	if prompt, ok := payload["prompt"]; ok {
		total += estimateTokensFromValue(prompt)
	}
	if input, ok := payload["input"]; ok {
		total += estimateTokensFromValue(input)
	}
	if total <= 0 {
		total = estimateTokensFromValue(payload)
	}
	return total
}

func estimateCompletionTokens(payload map[string]interface{}, defaultCompletion int) int64 {
	if value := parseInt64Value(payload["max_completion_tokens"]); value > 0 {
		return value
	}
	if value := parseInt64Value(payload["max_tokens"]); value > 0 {
		return value
	}
	if defaultCompletion > 0 {
		return int64(defaultCompletion)
	}
	return 0
}

func estimateTokensFromValue(value interface{}) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case string:
		return estimateTokensFromText(typed)
	case []interface{}:
		var total int64
		for _, item := range typed {
			total += estimateTokensFromValue(item)
		}
		return total
	case map[string]interface{}:
		if content, ok := typed["content"]; ok {
			return estimateTokensFromValue(content)
		}
		raw, err := json.Marshal(typed)
		if err != nil {
			return 0
		}
		return estimateTokensFromText(string(raw))
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return 0
		}
		return estimateTokensFromText(string(raw))
	}
}

func estimateTokensFromText(text string) int64 {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	runes := int64(len([]rune(trimmed)))
	tokens := runes / 4
	if runes%4 != 0 {
		tokens++
	}
	if tokens <= 0 {
		tokens = 1
	}
	return tokens
}

func parseInt64Header(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func parseInt64Value(value interface{}) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case int:
		if typed < 0 {
			return 0
		}
		return int64(typed)
	case int32:
		if typed < 0 {
			return 0
		}
		return int64(typed)
	case int64:
		if typed < 0 {
			return 0
		}
		return typed
	case float64:
		if typed < 0 {
			return 0
		}
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil || parsed < 0 {
			return 0
		}
		return parsed
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil || parsed < 0 {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func toString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func readAndRestoreRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	if req.Method != http.MethodPost && req.Method != http.MethodPut && req.Method != http.MethodPatch {
		return nil, nil
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}
