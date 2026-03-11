package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"gateyes/internal/requestmeta"
)

type TokenUsage struct {
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

func EstimateRequest(req *http.Request, defaultCompletion int) TokenUsage {
	body, err := readAndRestoreRequestBody(req)
	if err != nil || len(body) == 0 {
		return TokenUsage{}
	}

	payload := map[string]interface{}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TokenUsage{}
	}

	model := strings.TrimSpace(toString(payload["model"]))
	promptTokens := estimatePromptTokens(payload)
	completionTokens := estimateCompletionTokens(payload, defaultCompletion)
	totalTokens := promptTokens + completionTokens
	if totalTokens < 0 {
		totalTokens = 0
	}

	return TokenUsage{
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

func FromHeaders(req *http.Request) TokenUsage {
	prompt := parseInt64Header(req.Header.Get(requestmeta.HeaderUsagePromptTokens))
	completion := parseInt64Header(req.Header.Get(requestmeta.HeaderUsageCompletionTokens))
	total := parseInt64Header(req.Header.Get(requestmeta.HeaderUsageTotalTokens))
	if total <= 0 && (prompt > 0 || completion > 0) {
		total = prompt + completion
	}

	return TokenUsage{
		Model:            strings.TrimSpace(req.Header.Get(requestmeta.HeaderResolvedModel)),
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	}
}

func ClearInternalHeaders(req *http.Request) {
	req.Header.Del(requestmeta.HeaderResolvedProvider)
	req.Header.Del(requestmeta.HeaderResolvedModel)
	req.Header.Del(requestmeta.HeaderUsagePromptTokens)
	req.Header.Del(requestmeta.HeaderUsageCompletionTokens)
	req.Header.Del(requestmeta.HeaderUsageTotalTokens)
	req.Header.Del(requestmeta.HeaderUsageEstimatedTokens)
	req.Header.Del(requestmeta.HeaderRetryCount)
	req.Header.Del(requestmeta.HeaderFallbackCount)
	req.Header.Del(requestmeta.HeaderCircuitOpenCount)
	req.Header.Del(requestmeta.HeaderCacheStatus)
}

func SetEstimatedHeader(req *http.Request, estimate TokenUsage) {
	if req == nil || estimate.TotalTokens <= 0 {
		return
	}
	req.Header.Set(requestmeta.HeaderUsageEstimatedTokens, strconv.FormatInt(estimate.TotalTokens, 10))
}

func AttachToHeaders(req *http.Request, usage TokenUsage) {
	if req == nil {
		return
	}
	if usage.Model != "" {
		req.Header.Set(requestmeta.HeaderResolvedModel, usage.Model)
	}
	if usage.PromptTokens > 0 {
		req.Header.Set(requestmeta.HeaderUsagePromptTokens, strconv.FormatInt(usage.PromptTokens, 10))
	}
	if usage.CompletionTokens > 0 {
		req.Header.Set(requestmeta.HeaderUsageCompletionTokens, strconv.FormatInt(usage.CompletionTokens, 10))
	}
	if usage.TotalTokens > 0 {
		req.Header.Set(requestmeta.HeaderUsageTotalTokens, strconv.FormatInt(usage.TotalTokens, 10))
	}
}

func ExtractResponse(body []byte) (TokenUsage, bool) {
	if len(body) == 0 || len(body) > 2*1024*1024 {
		return TokenUsage{}, false
	}

	var payload struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TokenUsage{}, false
	}
	if payload.Usage.TotalTokens <= 0 &&
		payload.Usage.PromptTokens <= 0 &&
		payload.Usage.CompletionTokens <= 0 {
		return TokenUsage{}, false
	}

	totalTokens := payload.Usage.TotalTokens
	if totalTokens <= 0 {
		totalTokens = payload.Usage.PromptTokens + payload.Usage.CompletionTokens
	}

	return TokenUsage{
		Model:            payload.Model,
		PromptTokens:     payload.Usage.PromptTokens,
		CompletionTokens: payload.Usage.CompletionTokens,
		TotalTokens:      totalTokens,
	}, true
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
