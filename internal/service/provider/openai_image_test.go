package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestOpenAIProviderCreateImageGeneration(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("path = %q, want /images/generations", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1700000000,
			"data": []map[string]any{{
				"b64_json":       "aW1hZ2U=",
				"revised_prompt": "a small red cube",
			}},
			"usage": map[string]any{
				"input_tokens":  3,
				"output_tokens": 7,
				"total_tokens":  10,
				"input_tokens_details": map[string]any{
					"text_tokens":  3,
					"image_tokens": 0,
				},
			},
		})
	}))
	defer upstream.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
		Model:   "gpt-image-1",
		Timeout: 5,
	}).(*openAIProvider)

	resp, err := p.CreateImageGeneration(context.Background(), &ImageGenerationRequest{
		Prompt:         "red cube",
		N:              1,
		Size:           "1024x1024",
		ResponseFormat: "b64_json",
	})
	if err != nil {
		t.Fatalf("CreateImageGeneration() error: %v", err)
	}
	if upstreamBody["model"] != "gpt-image-1" || upstreamBody["prompt"] != "red cube" {
		t.Fatalf("unexpected upstream body: %#v", upstreamBody)
	}
	if resp.Created != 1700000000 || len(resp.Data) != 1 || resp.Data[0].B64JSON != "aW1hZ2U=" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 10 || resp.Usage.InputTokensDetails.TextTokens != 3 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestOpenAIProviderCreateImageGenerationAlias(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("path = %q, want /images/generations", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1700000000,
			"data": []map[string]any{{
				"b64_json":       "aW1hZ2U=",
				"revised_prompt": "a small red cube",
			}},
		})
	}))
	defer upstream.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
		Model:   "gpt-image-1",
		Timeout: 5,
		ModelAliases: map[string]string{
			"dall-e-alias": "gpt-image-1",
		},
	}).(*openAIProvider)

	_, err := p.CreateImageGeneration(context.Background(), &ImageGenerationRequest{
		Model:  "dall-e-alias",
		Prompt: "red cube",
	})
	if err != nil {
		t.Fatalf("CreateImageGeneration() error: %v", err)
	}
	if upstreamBody["model"] != "gpt-image-1" {
		t.Fatalf("expected alias resolved to gpt-image-1, got %v", upstreamBody["model"])
	}
}
