package provider

import (
	"testing"
)

func TestGetLayeredCacheKey_Basic(t *testing.T) {
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello"},
		},
	}

	key := req.GetLayeredCacheKey()

	if key.SystemHash == "" {
		t.Error("Expected system hash to be set")
	}
	if key.CurrentHash == "" {
		t.Error("Expected current hash to be set")
	}
	if key.AgentHash == "empty" {
		t.Log("Note: agent hash is empty (expected when no agent instructions)")
	}
}

func TestGetLayeredCacheKey_WithAgentInstructions(t *testing.T) {
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
		},
		Extra: map[string]any{
			"agent_instructions": "Always use XML tags in your response",
		},
	}

	key := req.GetLayeredCacheKey()

	if key.AgentHash == "empty" {
		t.Error("Expected agent hash to be set when agent_instructions is provided")
	}
}

func TestGetLayeredCacheKey_ConversationHistory(t *testing.T) {
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "What's the weather?"},
			{Role: "assistant", Content: "It's sunny today"},
			{Role: "user", Content: "What about tomorrow?"},
		},
	}

	key := req.GetLayeredCacheKey()

	// Current should be the last user message
	if key.CurrentHash == "" {
		t.Error("Expected current hash to be set")
	}

	// History should include system + first user + assistant
	if key.HistoryHash == "" {
		t.Error("Expected history hash to be set")
	}
}

func TestGetLayeredCacheKey_DifferentCurrentSameHistory(t *testing.T) {
	req1 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello"},
		},
	}

	req2 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hi there"},
		},
	}

	key1 := req1.GetLayeredCacheKey()
	key2 := req2.GetLayeredCacheKey()

	// Same system, different user -> different current hash, same system hash
	if key1.SystemHash != key2.SystemHash {
		t.Error("Expected same system hash")
	}
	if key1.CurrentHash == key2.CurrentHash {
		t.Error("Expected different current hash for different user messages")
	}
}

func TestGetLayeredCacheKey_SameCurrentDifferentHistory(t *testing.T) {
	req1 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "What's the weather?"},
			{Role: "assistant", Content: "It's sunny"},
			{Role: "user", Content: "Thanks"},
		},
	}

	req2 := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Tell me a joke"},
			{Role: "assistant", Content: "Why did the chicken..."},
			{Role: "user", Content: "Thanks"},
		},
	}

	key1 := req1.GetLayeredCacheKey()
	key2 := req2.GetLayeredCacheKey()

	// Same current ("Thanks"), different history
	if key1.CurrentHash != key2.CurrentHash {
		t.Error("Expected same current hash for same user message")
	}
	if key1.HistoryHash == key2.HistoryHash {
		t.Error("Expected different history hash for different conversations")
	}
}

func TestGetLayeredCacheKey_FullKeyFormat(t *testing.T) {
	req := &ResponseRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello"},
		},
	}

	key := req.GetLayeredCacheKey()

	// Full key should contain model and all hashes
	if key.FullKey == "" {
		t.Error("Expected full key to be set")
	}
	if len(key.FullKey) < 20 {
		t.Logf("Full key length: %d", len(key.FullKey))
	}
}
