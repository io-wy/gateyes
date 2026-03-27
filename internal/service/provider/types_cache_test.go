package provider

import (
	"testing"
)

func TestNormalizeCacheKey_OrderPreserved(t *testing.T) {
	// 测试消息顺序被保留
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
			{Role: "user", Content: "How are you?"},
		},
	}

	key := req.NormalizeCacheKey()

	// 验证顺序：user -> assistant -> user
	if !containsInOrder(key, "user:Hello", "assistant:Hi", "user:How") {
		t.Errorf("Cache key does not preserve message order")
	}
}

func TestNormalizeCacheKey_DifferentOrderDifferentKey(t *testing.T) {
	// 测试不同顺序生成不同 key
	req1 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
		},
	}

	req2 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "assistant", Content: "Hi"},
			{Role: "user", Content: "Hello"},
		},
	}

	key1 := req1.NormalizeCacheKey()
	key2 := req2.NormalizeCacheKey()

	if key1 == key2 {
		t.Errorf("Different message order should produce different cache keys")
	}
}

func TestNormalizeCacheKey_WhitespaceNormalized(t *testing.T) {
	// 测试空白被规范化
	req1 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "user", Content: "  Hello  "},
		},
	}

	req2 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	key1 := req1.NormalizeCacheKey()
	key2 := req2.NormalizeCacheKey()

	if key1 != key2 {
		t.Errorf("Whitespace should be normalized, but got different keys: %q vs %q", key1, key2)
	}
}

func TestNormalizeCacheKey_ToolCallsIncluded(t *testing.T) {
	// 测试 tool calls 被包含在 key 中
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{
				Role:  "assistant",
				Content: "I'll check the weather",
				ToolCalls: []ToolCall{
					{
						ID: "call_123",
						Function: FunctionCall{
							Name:      "get_weather",
							Arguments: `{"city": "Beijing"}`,
						},
					},
				},
			},
		},
	}

	key := req.NormalizeCacheKey()

	// 验证 tool call 信息在 key 中
	if !contains(key, "get_weather") {
		t.Errorf("Cache key should include tool call function name")
	}
}

func TestNormalizeCacheKey_ToolCallIDIncluded(t *testing.T) {
	// 测试 tool call ID 被包含在 key 中
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{
				Role:       "tool",
				Content:    "The weather is sunny",
				ToolCallID: "call_123",
			},
		},
	}

	key := req.NormalizeCacheKey()

	// 验证 tool call ID 在 key 中
	if !contains(key, "call_123") {
		t.Errorf("Cache key should include tool_call_id")
	}
}

func TestNormalizeCacheKey_ComplexContent(t *testing.T) {
	// 测试复杂 content 生成不同 key
	req1 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "Hello"},
					map[string]any{"type": "image", "url": "https://example.com/img1.png"},
				},
			},
		},
	}

	req2 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "Hello"},
					map[string]any{"type": "image", "url": "https://example.com/img2.png"},
				},
			},
		},
	}

	key1 := req1.NormalizeCacheKey()
	key2 := req2.NormalizeCacheKey()

	// 不同图片 URL 应该生成不同 key
	if key1 == key2 {
		t.Errorf("Different complex content should produce different cache keys")
	}
}

// contains 检查 s 是否包含 substr
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0
}

// containsInOrder 检查 s 是否按顺序包含多个子串
func containsInOrder(s string, substrs ...string) bool {
	pos := 0
	for _, substr := range substrs {
		i := indexOf(s[pos:], substr)
		if i < 0 {
			return false
		}
		pos += i + len(substr)
	}
	return true
}

// indexOf 简单实现
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
