package ingress

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gateyes/gateway/internal/proxy"
)

func TestBackendSelector_RoundRobin(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)
	b3 := proxy.NewBackend("b3", "10.0.0.3:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2, b3})
	rule := &proxy.RouteRule{
		ID:          "rr-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Collect 6 selections, expect cycling through b1, b2, b3, b1, b2, b3.
	var names []string
	for i := 0; i < 6; i++ {
		res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
		if err != nil {
			t.Fatalf("select error: %v", err)
		}
		names = append(names, res.Backend.Name())
	}

	want := []string{"b1", "b2", "b3", "b1", "b2", "b3"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("selection %d = %q, want %q", i, names[i], w)
		}
	}
}

func TestBackendSelector_WeightedRoundRobin(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 3)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2})
	rule := &proxy.RouteRule{
		ID:          "wrr-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Over 4 selections, b1 should be picked 3 times, b2 once.
	counts := map[string]int{}
	for i := 0; i < 4; i++ {
		res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
		if err != nil {
			t.Fatalf("select error: %v", err)
		}
		counts[res.Backend.Name()]++
	}

	if counts["b1"] != 3 {
		t.Errorf("b1 count = %d, want 3", counts["b1"])
	}
	if counts["b2"] != 1 {
		t.Errorf("b2 count = %d, want 1", counts["b2"])
	}
}

func TestBackendSelector_CanaryWeight(t *testing.T) {
	stable1 := proxy.NewBackend("stable1", "10.0.0.1:8080", "http", 1)
	canary1 := proxy.NewBackend("canary1", "10.0.0.2:8080", "http", 1)

	stablePool := proxy.NewBackendPool([]proxy.Backend{stable1})
	canaryPool := proxy.NewBackendPool([]proxy.Backend{canary1})

	rule := &proxy.RouteRule{
		ID:                "canary-test",
		BackendPool:       stablePool,
		CanaryBackendPool: canaryPool,
		Annotations: &proxy.Annotations{
			Canary:       true,
			CanaryWeight: 30, // 30% canary
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Run many selections and verify approximate split.
	canaryCount := 0
	iterations := 1000
	for i := 0; i < iterations; i++ {
		res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
		if err != nil {
			t.Fatalf("select error: %v", err)
		}
		if res.Backend.Name() == "canary1" {
			canaryCount++
		}
	}

	ratio := float64(canaryCount) / float64(iterations)
	if ratio < 0.20 || ratio > 0.40 {
		t.Errorf("canary ratio = %.2f, expected ~0.30", ratio)
	}
}

func TestBackendSelector_CanaryByHeader(t *testing.T) {
	stable1 := proxy.NewBackend("stable1", "10.0.0.1:8080", "http", 1)
	canary1 := proxy.NewBackend("canary1", "10.0.0.2:8080", "http", 1)

	stablePool := proxy.NewBackendPool([]proxy.Backend{stable1})
	canaryPool := proxy.NewBackendPool([]proxy.Backend{canary1})

	rule := &proxy.RouteRule{
		ID:                "canary-hdr-test",
		BackendPool:       stablePool,
		CanaryBackendPool: canaryPool,
		Annotations: &proxy.Annotations{
			Canary:       true,
			CanaryWeight: 0,
			CanaryByHeader: "X-Canary",
		},
	}

	sel := NewBackendSelector()

	// Request with header "always" should always go to canary.
	req := mustReq("http://example.com/")
	req.Header.Set("X-Canary", "always")
	res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "canary1" {
		t.Errorf("expected canary backend, got %q", res.Backend.Name())
	}

	// Request with header "true" should also go to canary.
	req2 := mustReq("http://example.com/")
	req2.Header.Set("X-Canary", "true")
	res2, err := sel.Select(SelectionContext{Request: req2, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res2.Backend.Name() != "canary1" {
		t.Errorf("expected canary backend, got %q", res2.Backend.Name())
	}

	// Request without header should go to stable.
	req3 := mustReq("http://example.com/")
	res3, err := sel.Select(SelectionContext{Request: req3, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res3.Backend.Name() != "stable1" {
		t.Errorf("expected stable backend, got %q", res3.Backend.Name())
	}
}

func TestBackendSelector_CanaryByHeaderOverridesWeight(t *testing.T) {
	stable1 := proxy.NewBackend("stable1", "10.0.0.1:8080", "http", 1)
	canary1 := proxy.NewBackend("canary1", "10.0.0.2:8080", "http", 1)

	stablePool := proxy.NewBackendPool([]proxy.Backend{stable1})
	canaryPool := proxy.NewBackendPool([]proxy.Backend{canary1})

	rule := &proxy.RouteRule{
		ID:                "canary-override-test",
		BackendPool:       stablePool,
		CanaryBackendPool: canaryPool,
		Annotations: &proxy.Annotations{
			Canary:         true,
			CanaryWeight:   0, // No canary by weight
			CanaryByHeader: "X-Canary",
		},
	}

	sel := NewBackendSelector()

	// Even with weight=0, header should force canary.
	req := mustReq("http://example.com/")
	req.Header.Set("X-Canary", "always")
	res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "canary1" {
		t.Errorf("expected canary backend via header override, got %q", res.Backend.Name())
	}
}

func TestBackendSelector_SessionAffinity(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2})
	rule := &proxy.RouteRule{
		ID:          "affinity-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			Affinity:           "cookie",
			AffinityCookieName: "session",
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// First selection without cookie: should set cookie.
	res1, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res1.CookieName != "session" {
		t.Errorf("expected cookie name 'session', got %q", res1.CookieName)
	}
	if res1.CookieVal == "" {
		t.Fatal("expected non-empty cookie value")
	}

	// Second selection with the same cookie: should pin to same backend.
	res2, err := sel.Select(SelectionContext{
		Request:   req,
		Rule:      rule,
		CookieVal: res1.CookieVal,
	})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res2.Backend.Name() != res1.Backend.Name() {
		t.Errorf("affinity broken: first=%q, second=%q", res1.Backend.Name(), res2.Backend.Name())
	}
	// When cookie matches existing backend, no new cookie should be set.
	if res2.CookieName != "" {
		t.Errorf("expected no cookie to set when pinned, got %q", res2.CookieName)
	}
}

func TestBackendSelector_SessionAffinityConsistentHash(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)
	b3 := proxy.NewBackend("b3", "10.0.0.3:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2, b3})
	rule := &proxy.RouteRule{
		ID:          "affinity-hash-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			Affinity:           "cookie",
			AffinityCookieName: "session",
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Multiple requests without cookie should be deterministically hashed.
	res1, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}

	// Same request again should hash to the same backend.
	res2, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res1.Backend.Name() != res2.Backend.Name() {
		t.Errorf("consistent hash broken: first=%q, second=%q", res1.Backend.Name(), res2.Backend.Name())
	}
}

func TestBackendSelector_IPWhitelist_Allow(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	pool := proxy.NewBackendPool([]proxy.Backend{b1})
	rule := &proxy.RouteRule{
		ID:          "whitelist-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			WhitelistSourceRange: []string{"10.0.0.0/8"},
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")
	req.RemoteAddr = "10.1.2.3:12345"

	res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "b1" {
		t.Errorf("expected b1, got %q", res.Backend.Name())
	}
}

func TestBackendSelector_IPWhitelist_Deny(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	pool := proxy.NewBackendPool([]proxy.Backend{b1})
	rule := &proxy.RouteRule{
		ID:          "whitelist-deny-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			WhitelistSourceRange: []string{"10.0.0.0/8"},
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")
	req.RemoteAddr = "192.168.1.1:12345"

	_, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err == nil {
		t.Fatal("expected error for IP not in whitelist")
	}
	if !strings.Contains(err.Error(), "not in whitelist") {
		t.Errorf("expected whitelist error, got: %v", err)
	}
}

func TestBackendSelector_NoHealthyBackend(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b1.SetHealthy(false)
	pool := proxy.NewBackendPool([]proxy.Backend{b1})
	rule := &proxy.RouteRule{
		ID:          "no-healthy-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	_, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err == nil {
		t.Fatal("expected error for no healthy backends")
	}
}

func TestBackendSelector_ConcurrentRoundRobin(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2})
	rule := &proxy.RouteRule{
		ID:          "concurrent-rr-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	var b1Count, b2Count int64
	var wg sync.WaitGroup
	workers := 100
	selectionsPerWorker := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < selectionsPerWorker; j++ {
				res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
				if err != nil {
					t.Errorf("select error: %v", err)
					return
				}
				if res.Backend.Name() == "b1" {
					atomic.AddInt64(&b1Count, 1)
				} else {
					atomic.AddInt64(&b2Count, 1)
				}
			}
		}()
	}
	wg.Wait()

	total := b1Count + b2Count
	expected := int64(workers * selectionsPerWorker)
	if total != expected {
		t.Errorf("total selections = %d, want %d", total, expected)
	}
	// With equal weights, should be roughly balanced.
	diff := b1Count - b2Count
	if diff < 0 {
		diff = -diff
	}
	if float64(diff)/float64(total) > 0.2 {
		t.Errorf("round-robin unbalanced: b1=%d, b2=%d", b1Count, b2Count)
	}
}

func TestCheckIPWhitelist(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		allowed    []string
		want       bool
	}{
		{
			name:       "ipv4 in range",
			remoteAddr: "10.0.1.5:12345",
			allowed:    []string{"10.0.0.0/8"},
			want:       true,
		},
		{
			name:       "ipv4 not in range",
			remoteAddr: "192.168.1.1:12345",
			allowed:    []string{"10.0.0.0/8"},
			want:       false,
		},
		{
			name:       "multiple ranges match second",
			remoteAddr: "172.16.5.5:12345",
			allowed:    []string{"10.0.0.0/8", "172.16.0.0/12"},
			want:       true,
		},
		{
			name:       "exact ip match",
			remoteAddr: "1.2.3.4:56789",
			allowed:    []string{"1.2.3.4/32"},
			want:       true,
		},
		{
			name:       "no port",
			remoteAddr: "10.0.0.1",
			allowed:    []string{"10.0.0.0/8"},
			want:       true,
		},
		{
			name:       "invalid cidr skipped",
			remoteAddr: "10.0.0.1:12345",
			allowed:    []string{"invalid", "10.0.0.0/8"},
			want:       true,
		},
		{
			name:       "ipv6 in range",
			remoteAddr: "[::1]:12345",
			allowed:    []string{"::1/128"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkIPWhitelist(tt.remoteAddr, tt.allowed)
			if got != tt.want {
				t.Errorf("checkIPWhitelist(%q, %v) = %v, want %v", tt.remoteAddr, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestConsistentHash(t *testing.T) {
	// Same key should always map to same bucket.
	key := "test-key"
	buckets := 5
	var results []int
	for i := 0; i < 10; i++ {
		results = append(results, consistentHash(key, buckets))
	}
	first := results[0]
	for i, r := range results {
		if r != first {
			t.Errorf("consistent hash not stable: iteration %d = %d, first = %d", i, r, first)
		}
	}
	if first < 0 || first >= buckets {
		t.Errorf("hash out of range: %d, buckets = %d", first, buckets)
	}
}

func TestClientIPFromString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"10.0.0.1:12345", "10.0.0.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"[::1]:12345", "::1"},
	}
	for _, tt := range tests {
		got := clientIPFromString(tt.input)
		if got == nil {
			t.Errorf("clientIPFromString(%q) = nil, want %q", tt.input, tt.want)
			continue
		}
		if got.String() != tt.want {
			t.Errorf("clientIPFromString(%q) = %q, want %q", tt.input, got.String(), tt.want)
		}
	}
}

func TestClientIPFromRequest(t *testing.T) {
	// Test X-Forwarded-For.
	req := mustReq("http://example.com/")
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	got := clientIPFromRequest(req)
	if got != "1.2.3.4" {
		t.Errorf("X-Forwarded-For: got %q, want 1.2.3.4", got)
	}

	// Test X-Real-Ip.
	req2 := mustReq("http://example.com/")
	req2.Header.Set("X-Real-Ip", "9.8.7.6")
	got2 := clientIPFromRequest(req2)
	if got2 != "9.8.7.6" {
		t.Errorf("X-Real-Ip: got %q, want 9.8.7.6", got2)
	}

	// Test fallback.
	req3 := mustReq("http://example.com/")
	req3.RemoteAddr = "10.20.30.40:56789"
	got3 := clientIPFromRequest(req3)
	if got3 != "10.20.30.40" {
		t.Errorf("RemoteAddr fallback: got %q, want 10.20.30.40", got3)
	}
}

func TestBackendSelector_CanaryWeightZero(t *testing.T) {
	stable1 := proxy.NewBackend("stable1", "10.0.0.1:8080", "http", 1)
	canary1 := proxy.NewBackend("canary1", "10.0.0.2:8080", "http", 1)

	stablePool := proxy.NewBackendPool([]proxy.Backend{stable1})
	canaryPool := proxy.NewBackendPool([]proxy.Backend{canary1})

	rule := &proxy.RouteRule{
		ID:                "canary-zero-test",
		BackendPool:       stablePool,
		CanaryBackendPool: canaryPool,
		Annotations: &proxy.Annotations{
			Canary:       true,
			CanaryWeight: 0, // No canary traffic
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Should always go to stable.
	for i := 0; i < 20; i++ {
		res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
		if err != nil {
			t.Fatalf("select error: %v", err)
		}
		if res.Backend.Name() != "stable1" {
			t.Errorf("expected stable backend with weight=0, got %q", res.Backend.Name())
		}
	}
}

func TestBackendSelector_CanaryNoHealthyCanary(t *testing.T) {
	stable1 := proxy.NewBackend("stable1", "10.0.0.1:8080", "http", 1)
	canary1 := proxy.NewBackend("canary1", "10.0.0.2:8080", "http", 1)
	canary1.SetHealthy(false)

	stablePool := proxy.NewBackendPool([]proxy.Backend{stable1})
	canaryPool := proxy.NewBackendPool([]proxy.Backend{canary1})

	rule := &proxy.RouteRule{
		ID:                "canary-no-healthy-test",
		BackendPool:       stablePool,
		CanaryBackendPool: canaryPool,
		Annotations: &proxy.Annotations{
			Canary:       true,
			CanaryWeight: 100, // Would always go canary, but canary is unhealthy
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Should fall through to stable because canary is unhealthy.
	res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "stable1" {
		t.Errorf("expected stable fallback when canary unhealthy, got %q", res.Backend.Name())
	}
}

func TestBackendSelector_AffinityCookieMatchesBackend(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2})
	rule := &proxy.RouteRule{
		ID:          "affinity-match-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			Affinity:           "cookie",
			AffinityCookieName: "session",
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Cookie value matching b2 should pin to b2.
	res, err := sel.Select(SelectionContext{
		Request:   req,
		Rule:      rule,
		CookieVal: "b2",
	})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "b2" {
		t.Errorf("expected b2 via cookie match, got %q", res.Backend.Name())
	}
	if res.CookieName != "" {
		t.Errorf("expected no new cookie when pinned, got name=%q", res.CookieName)
	}
}

func TestBackendSelector_AffinityCookieNoMatch(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := proxy.NewBackend("b2", "10.0.0.2:8080", "http", 1)

	pool := proxy.NewBackendPool([]proxy.Backend{b1, b2})
	rule := &proxy.RouteRule{
		ID:          "affinity-nomatch-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			Affinity:           "cookie",
			AffinityCookieName: "session",
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	// Cookie value not matching any backend should trigger consistent hash and set new cookie.
	res, err := sel.Select(SelectionContext{
		Request:   req,
		Rule:      rule,
		CookieVal: "nonexistent",
	})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.CookieName != "session" {
		t.Errorf("expected cookie to be set, got name=%q", res.CookieName)
	}
	if res.CookieVal == "" {
		t.Fatal("expected non-empty cookie value")
	}
	// Cookie value should be one of the available backends.
	if res.CookieVal != "b1" && res.CookieVal != "b2" {
		t.Errorf("cookie value %q not a valid backend name", res.CookieVal)
	}
}

func TestBackendSelector_NilAnnotations(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	pool := proxy.NewBackendPool([]proxy.Backend{b1})
	rule := &proxy.RouteRule{
		ID:          "nil-annot-test",
		BackendPool: pool,
		Annotations: nil,
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "b1" {
		t.Errorf("expected b1, got %q", res.Backend.Name())
	}
}

func TestBackendSelector_WhitelistEmpty(t *testing.T) {
	b1 := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	pool := proxy.NewBackendPool([]proxy.Backend{b1})
	rule := &proxy.RouteRule{
		ID:          "whitelist-empty-test",
		BackendPool: pool,
		Annotations: &proxy.Annotations{
			WhitelistSourceRange: []string{}, // empty = allow all
		},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")
	req.RemoteAddr = "192.168.1.1:12345"

	res, err := sel.Select(SelectionContext{Request: req, Rule: rule})
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if res.Backend.Name() != "b1" {
		t.Errorf("expected b1, got %q", res.Backend.Name())
	}
}

// BenchmarkBackendSelector_Select measures selection performance.
func BenchmarkBackendSelector_Select(b *testing.B) {
	backends := make([]proxy.Backend, 10)
	for i := 0; i < 10; i++ {
		backends[i] = proxy.NewBackend(fmt.Sprintf("b%d", i), fmt.Sprintf("10.0.0.%d:8080", i), "http", 1)
	}
	pool := proxy.NewBackendPool(backends)
	rule := &proxy.RouteRule{
		ID:          "bench",
		BackendPool: pool,
		Annotations: &proxy.Annotations{},
	}

	sel := NewBackendSelector()
	req := mustReq("http://example.com/")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sel.Select(SelectionContext{Request: req, Rule: rule})
	}
}
