package provider

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gateyes/gateway/internal/config"
	"github.com/openai/openai-go"
	oairesponses "github.com/openai/openai-go/responses"
)

func TestJSONOutput(t *testing.T) {
	// 构建一个内部统一的 Response（模拟从 Anthropic 转换后的结果）
	internalResp := &Response{
		ID:      "msg_01ABC",
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   "public-claude",
		Status:  "completed",
		Output: []ResponseOutput{
			{
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []ResponseContent{
					{Type: "thinking", Thinking: "Let me think...", Signature: "sig123"},
					{Type: "output_text", Text: "The answer is 42."},
				},
			},
		},
		Usage: Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CachedTokens: 5},
	}

	fmt.Println("=== 1. 内部统一格式 Response（网关内部使用，不直接返回客户端）===")
	b, _ := json.MarshalIndent(internalResp, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 2. 返回给 Responses API 客户端的 JSON ===")
	// Responses API 直接序列化内部 Response
	b, _ = json.MarshalIndent(internalResp, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 3. 返回给 ChatCompletion 客户端的 JSON ===")
	chatResp := ConvertResponseToChat(internalResp)
	b, _ = json.MarshalIndent(chatResp, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 4. 返回给 Anthropic Messages API 客户端的 JSON ===")
	anthropicResp := ConvertResponseToAnthropic(internalResp)
	b, _ = json.MarshalIndent(anthropicResp, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 5. ChatCompletion 请求 JSON ===")
	chatReq := ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: []ContentBlock{{Type: "text", Text: "You are a helpful assistant"}}},
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "Hello"}}},
		},
		Tools: []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get weather for a city",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []string{"city"},
					},
				},
			},
		},
	}
	chatParams := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel("gpt-4"),
		Messages: buildChatCompletionMessages(chatReq.Messages),
	}
	chatParams.MaxTokens = openai.Int(1024)
	b, _ = json.MarshalIndent(chatParams, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 6. Responses API 请求 JSON ===")
	respParams := oairesponses.ResponseNewParams{
		Model: oairesponses.ResponsesModel("gpt-4"),
		Input: oairesponses.ResponseNewParamsInputUnion{
			OfInputItemList: buildOpenAIInput(chatReq.Messages),
		},
	}
	respParams.MaxOutputTokens = openai.Int(1024)
	b, _ = json.MarshalIndent(respParams, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 7. Anthropic 请求 JSON ===")
	anthropicParams, _ := buildAnthropicParams(&chatReq, config.ProviderConfig{MaxTokens: 1024})
	b, _ = json.MarshalIndent(anthropicParams, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 8. ChatCompletion Chunk (文本增量) ===")
	chunk := ConvertEventToChatChunk("chatcmpl-test", "gpt-4", ResponseEvent{
		Type:      EventContentDelta,
		Delta:     "Hello",
		TextDelta: "Hello",
	})
	b, _ = json.MarshalIndent(chunk, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 9. ChatCompletion Chunk (工具调用) ===")
	chunkTool := ConvertEventToChatChunk("chatcmpl-test", "gpt-4", ResponseEvent{
		Type: EventToolCallDone,
		Output: &ResponseOutput{
			ID:     "call_abc",
			Type:   "function_call",
			CallID: "call_abc",
			Name:   "get_weather",
			Args:   `{"city":"Shanghai"}`,
		},
	})
	b, _ = json.MarshalIndent(chunkTool, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 10. ChatCompletion Chunk (流结束) ===")
	chunkDone := ConvertEventToChatChunk("chatcmpl-test", "gpt-4", ResponseEvent{
		Type:     EventResponseCompleted,
		Response: &Response{Usage: Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}},
	})
	b, _ = json.MarshalIndent(chunkDone, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 11. Anthropic 原生 → 内部 Response 转换 ===")
	// 展示 Anthropic SDK 响应如何被转换为内部 Response
	anthropicNative := anthropic.Message{
		ID:    "msg_01ABC",
		Model: "claude-3-sonnet",
		Role:  "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "thinking", Thinking: "Let me think...", Signature: "sig123"},
			{Type: "text", Text: "The answer is 42."},
		},
		Usage: anthropic.Usage{
			InputTokens:          10,
			OutputTokens:         20,
			CacheReadInputTokens: 5,
		},
	}
	converted := convertSDKAnthropicMessage(anthropicNative, "public-claude")
	b, _ = json.MarshalIndent(converted, "", "  ")
	fmt.Println(string(b))

	fmt.Println("\n=== 12. 流式结束事件（携带完整 Response）===")
	streamResp := ResponseEvent{
		Type: EventResponseCompleted,
		Response: &Response{
			ID:      "stream-test",
			Object:  "response",
			Model:   "gpt-4",
			Status:  "completed",
			Output:  []ResponseOutput{{Type: "message", Role: "assistant", Content: []ResponseContent{{Type: "output_text", Text: "Streamed response."}}}},
			Usage:   Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}
	b, _ = json.MarshalIndent(streamResp, "", "  ")
	fmt.Println(string(b))
}
