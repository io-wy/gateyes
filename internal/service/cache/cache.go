// Package cache provides L1 response cache for the LLM gateway.
//
// L1 cache is exact-match: requests with identical (tenant, model, prompt)
// hit the same key. Semantic similarity is out of scope — that belongs to
// a future L2 layer.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Layer identifies the cache tier emitted in metrics labels.
const (
	LayerL1               = "l1"
	LayerL1Stream         = "l1_stream"
	LayerL2Semantic       = "l2_semantic"
	LayerL2SemanticStream = "l2_semantic_stream"
)

// Usage mirrors token accounting fields needed for cached responses.
// We intentionally avoid importing internal/service/provider here to keep
// cache self-contained and prevent circular dependencies.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

// Entry is the cached payload for one logical request.
//
// For non-streaming responses, Response holds the full marshalled
// OpenAI/Anthropic response body. For streaming, StreamTranscript holds the
// normalized provider events that were visible to the client, and Stream is
// set to true. StreamRaw is kept for backwards-compatible cache payloads.
type Entry struct {
	Response         []byte        `json:"response,omitempty"`
	StreamRaw        []byte        `json:"stream_raw,omitempty"`
	StreamTranscript []StreamEvent `json:"stream_transcript,omitempty"`
	Stream           bool          `json:"stream"`
	Model            string        `json:"model"`
	Provider         string        `json:"provider"`
	Usage            Usage         `json:"usage"`
	CreatedAt        int64         `json:"created_at"`
}

type StreamEvent struct {
	Type          string          `json:"type"`
	Delta         string          `json:"delta,omitempty"`
	TextDelta     string          `json:"text_delta,omitempty"`
	ThinkingDelta string          `json:"thinking_delta,omitempty"`
	Response      json.RawMessage `json:"response,omitempty"`
	Output        json.RawMessage `json:"output,omitempty"`
	ToolCalls     json.RawMessage `json:"tool_calls,omitempty"`
	FinishReason  string          `json:"finish_reason,omitempty"`
	Usage         *Usage          `json:"usage,omitempty"`
	OutputIndex   *int            `json:"output_index,omitempty"`
	ContentIndex  *int            `json:"content_index,omitempty"`
}

// KeyInput is the canonical input used to derive a cache key.
//
// PromptCanon must be a stable, canonical encoding of the request body
// (typically JSON with sorted keys). Callers are responsible for canonicalisation
// — see CanonicalizeMessages and CanonicalizeJSON helpers.
//
// Bucket is an opt-in extra dimension for A/B isolation. Two requests with
// the same prompt but different buckets get different cache keys; empty
// bucket preserves prior behavior.
type KeyInput struct {
	TenantID    string
	Model       string
	PromptCanon string
	Stream      bool
	Surface     string // "responses" | "chat_completions" | "messages" | "embeddings"
	Bucket      string
}

// BuildKey returns a deterministic cache key for the given input.
// Different tenants, models, surfaces, or stream flags always produce
// distinct keys to prevent cross-tenant or cross-mode collisions.
func BuildKey(in KeyInput) string {
	h := sha256.New()
	writeField(h, in.TenantID)
	writeField(h, in.Model)
	writeField(h, in.Surface)
	writeField(h, in.PromptCanon)
	if in.Stream {
		_, _ = h.Write([]byte{0x01})
	} else {
		_, _ = h.Write([]byte{0x00})
	}
	if in.Bucket != "" {
		writeField(h, in.Bucket)
	}
	return "gw:l1:" + hex.EncodeToString(h.Sum(nil))
}

func writeField(h interface{ Write([]byte) (int, error) }, s string) {
	_, _ = h.Write([]byte(s))
	_, _ = h.Write([]byte{0x00})
}

// CanonicalizeJSON serialises arbitrary JSON-marshallable input with
// deterministic key ordering. Returns the canonical bytes or an error.
func CanonicalizeJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return canonicalizeBytes(raw)
}

func canonicalizeBytes(raw []byte) ([]byte, error) {
	var any any
	if err := json.Unmarshal(raw, &any); err != nil {
		return nil, err
	}
	return marshalCanonical(any)
}

func marshalCanonical(v any) ([]byte, error) {
	switch typed := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			sb.Write(kb)
			sb.WriteByte(':')
			vb, err := marshalCanonical(typed[k])
			if err != nil {
				return nil, err
			}
			sb.Write(vb)
		}
		sb.WriteByte('}')
		return []byte(sb.String()), nil
	case []any:
		var sb strings.Builder
		sb.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				sb.WriteByte(',')
			}
			ib, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			sb.Write(ib)
		}
		sb.WriteByte(']')
		return []byte(sb.String()), nil
	default:
		return json.Marshal(typed)
	}
}

// Stats holds aggregate counters for a Cache instance.
type Stats struct {
	Hits   uint64
	Misses uint64
	Writes uint64
	Errors uint64
}

// Cache is the L1 cache contract. Implementations must be safe for
// concurrent use and tolerate transient backend failures via fail-open
// (return ok=false, err=nil) when backend is degraded; only return err
// for unexpected protocol violations the caller should log.
type Cache interface {
	// Get fetches the entry for key. Returns (entry, true, nil) on hit,
	// (nil, false, nil) on miss, and (nil, false, err) on infrastructure
	// failure. Callers should treat err as a soft miss.
	Get(ctx context.Context, key string) (*Entry, bool, error)

	// Set persists the entry under key with TTL. ttl<=0 means use the
	// implementation's default. Failures are returned but should not
	// block the caller's hot path — write asynchronously when possible.
	Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error

	// Delete removes the entry for key. Missing key is not an error.
	Delete(ctx context.Context, key string) error

	// Stats returns a snapshot of aggregate counters since process start.
	Stats() Stats

	// Close releases any underlying resources (network, file handles).
	// Safe to call multiple times.
	Close() error
}

// Now is overridable for tests to inject deterministic timestamps.
var Now = time.Now
