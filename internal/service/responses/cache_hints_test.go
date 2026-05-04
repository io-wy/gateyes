package responses

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestCacheHintsContextRoundtrip(t *testing.T) {
	ctx := context.Background()
	if got := CacheHintsFrom(ctx); got != (CacheHints{}) {
		t.Fatalf("empty ctx hints = %+v, want zero", got)
	}

	hint := CacheHints{Skip: true, TTL: 30 * time.Second, Bucket: "ab-1"}
	ctx2 := WithCacheHints(ctx, hint)
	if got := CacheHintsFrom(ctx2); got != hint {
		t.Fatalf("CacheHintsFrom = %+v, want %+v", got, hint)
	}
}

func TestCacheHintsZeroValueDoesNotMutateCtx(t *testing.T) {
	base := context.Background()
	derived := WithCacheHints(base, CacheHints{})
	if derived != base {
		t.Fatal("zero hints should return same context to avoid wrapping")
	}
}

func TestParseCacheHintsFromHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    CacheHints
	}{
		{"empty", nil, CacheHints{}},
		{"skip 1", map[string]string{"X-Gateyes-Cache-Skip": "1"}, CacheHints{Skip: true}},
		{"skip true", map[string]string{"X-Gateyes-Cache-Skip": "true"}, CacheHints{Skip: true}},
		{"skip yes", map[string]string{"X-Gateyes-Cache-Skip": "YES"}, CacheHints{Skip: true}},
		{"skip 0 not set", map[string]string{"X-Gateyes-Cache-Skip": "0"}, CacheHints{}},
		{"ttl 60", map[string]string{"X-Gateyes-Cache-TTL": "60"}, CacheHints{TTL: 60 * time.Second}},
		{"ttl bad", map[string]string{"X-Gateyes-Cache-TTL": "abc"}, CacheHints{}},
		{"ttl negative", map[string]string{"X-Gateyes-Cache-TTL": "-5"}, CacheHints{}},
		{"bucket", map[string]string{"X-Gateyes-Cache-Bucket": "experiment-x"}, CacheHints{Bucket: "experiment-x"}},
		{"all three", map[string]string{
			"X-Gateyes-Cache-Skip":   "1",
			"X-Gateyes-Cache-TTL":    "120",
			"X-Gateyes-Cache-Bucket": "ab-2",
		}, CacheHints{Skip: true, TTL: 120 * time.Second, Bucket: "ab-2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCacheHintsFromHeaders(func(name string) string { return tc.headers[name] })
			if got != tc.want {
				t.Fatalf("ParseCacheHintsFromHeaders() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func minimalCacheReq() *provider.ResponseRequest {
	return &provider.ResponseRequest{Model: "m1", Input: "hello"}
}

func minimalCacheIdentity() *repository.AuthIdentity {
	return &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
}

func TestShouldSkipCacheHonorsHintOverride(t *testing.T) {
	svc := newCacheService(newMockCache())
	req := minimalCacheReq()
	if svc.shouldSkipCache(context.Background(), req) {
		t.Fatal("baseline should not skip with cache enabled")
	}
	ctx := WithCacheHints(context.Background(), CacheHints{Skip: true})
	if !svc.shouldSkipCache(ctx, req) {
		t.Fatal("Skip=true hint should force skip")
	}
}

func TestBuildCacheKeyDiffersByBucket(t *testing.T) {
	svc := newCacheService(newMockCache())
	req := minimalCacheReq()
	identity := minimalCacheIdentity()
	baseKey := svc.buildCacheKey(context.Background(), identity, req)
	bucketCtx := WithCacheHints(context.Background(), CacheHints{Bucket: "x"})
	bucketKey := svc.buildCacheKey(bucketCtx, identity, req)
	if baseKey == bucketKey {
		t.Fatal("buildCacheKey should differ when CacheHints.Bucket is set")
	}
}
