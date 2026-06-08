package handler

import (
	"errors"
	"testing"

	"github.com/gateyes/gateway/internal/service/catalog"
)

func TestWrapCatalogError(t *testing.T) {
	h := NewHandler(&Dependencies{})
	cases := []struct {
		err    error
		status int
	}{
		{catalog.ErrServiceAccessDenied, 403},
		{catalog.ErrRateLimited, 429},
		{catalog.ErrServiceNotPublished, 400},
		{catalog.ErrServiceDisabled, 400},
		{catalog.ErrServiceSurfaceDenied, 400},
		{catalog.ErrPromptTemplateMissing, 400},
		{catalog.ErrPromptVariableMissing, 400},
		{catalog.ErrPolicyViolation, 400},
		{errors.New("unknown"), 500},
	}
	for _, tc := range cases {
		got := h.wrapCatalogError(tc.err)
		if got.Status != tc.status {
			t.Fatalf("wrapCatalogError(%v).Status = %d, want %d", tc.err, got.Status, tc.status)
		}
	}
}

func TestClassifyMetricsError(t *testing.T) {
	cases := []struct {
		err       error
		errType   string
		wantRes   string
		wantClass string
	}{
		{errors.New("timeout"), "", "timeout", "timeout"},
		{errors.New("rate_limit"), "", "rate_limited", "rate_limited"},
		{errors.New("forbidden access"), "", "auth_error", "forbidden"},
		{errors.New("some error"), "invalid_request_error", "client_error", "invalid_request"},
		{errors.New("some error"), "", "internal_error", "internal_error"},
	}
	for _, tc := range cases {
		res, class := classifyMetricsError(tc.err, tc.errType)
		if res != tc.wantRes || class != tc.wantClass {
			t.Fatalf("classifyMetricsError(%q,%q) = (%s,%s), want (%s,%s)", tc.err, tc.errType, res, class, tc.wantRes, tc.wantClass)
		}
	}
}

func TestNormalizeMetricsProvider(t *testing.T) {
	if got := normalizeMetricsProvider("test-openai"); got != "test-openai" {
		t.Fatalf("normalizeMetricsProvider = %q, want %q", got, "test-openai")
	}
	if got := normalizeMetricsProvider(""); got != "none" {
		t.Fatalf("normalizeMetricsProvider(empty) = %q, want %q", got, "none")
	}
}

func TestNormalizeMetricsProvider_Exported(t *testing.T) {
	if got := NormalizeMetricsProvider("my-provider"); got != "my-provider" {
		t.Fatalf("NormalizeMetricsProvider = %q, want %q", got, "my-provider")
	}
}

func TestProviderHealthStatusValue(t *testing.T) {
	if got := providerHealthStatusValue("healthy"); got != 0 {
		t.Fatalf("providerHealthStatusValue(healthy) = %d, want 0", got)
	}
	if got := providerHealthStatusValue("degraded"); got != 1 {
		t.Fatalf("providerHealthStatusValue(degraded) = %d, want 1", got)
	}
	if got := providerHealthStatusValue("unhealthy"); got != 2 {
		t.Fatalf("providerHealthStatusValue(unhealthy) = %d, want 2", got)
	}
	if got := providerHealthStatusValue("unknown"); got != 3 {
		t.Fatalf("providerHealthStatusValue(unknown) = %d, want 3", got)
	}
}

func TestRegistryString(t *testing.T) {
	if got := registryString(false, "x", "fallback"); got != "fallback" {
		t.Fatalf("registryString(false) = %q, want fallback", got)
	}
	if got := registryString(true, "", "fallback"); got != "fallback" {
		t.Fatalf("registryString(true,empty) = %q, want fallback", got)
	}
	if got := registryString(true, "val", "fallback"); got != "val" {
		t.Fatalf("registryString(true,val) = %q, want val", got)
	}
}

func TestRegistryInt(t *testing.T) {
	if got := registryInt(false, 5); got != 0 {
		t.Fatalf("registryInt(false) = %d, want 0", got)
	}
	if got := registryInt(true, 5); got != 5 {
		t.Fatalf("registryInt(true) = %d, want 5", got)
	}
}
