package budget

import (
	"testing"
	"time"

	"gateyes/internal/dto"
)

func TestMemoryBackendConsumeAndAdjust(t *testing.T) {
	service := NewMemoryBackend()

	first, err := service.Consume(t.Context(), []dto.BudgetCounter{{
		Key:   "k1",
		Limit: 10,
		Cost:  4,
		TTL:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("consume failed: %v", err)
	}
	if !first.Allowed {
		t.Fatalf("expected allowed")
	}

	second, err := service.Consume(t.Context(), []dto.BudgetCounter{{
		Key:   "k1",
		Limit: 10,
		Cost:  7,
		TTL:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("second consume failed: %v", err)
	}
	if second.Allowed {
		t.Fatalf("expected denied on limit exceed")
	}

	if err := service.Adjust(t.Context(), []dto.BudgetAdjustment{{
		Key:   "k1",
		Delta: -2,
		TTL:   time.Minute,
	}}); err != nil {
		t.Fatalf("adjust failed: %v", err)
	}

	third, err := service.Consume(t.Context(), []dto.BudgetCounter{{
		Key:   "k1",
		Limit: 10,
		Cost:  6,
		TTL:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("third consume failed: %v", err)
	}
	if !third.Allowed {
		t.Fatalf("expected allowed after refund adjustment")
	}
}

func TestNewBackendRedisStrictInitFailure(t *testing.T) {
	service, err := NewBackend("redis", "", "", 0, "gateyes", "rate", true)
	if err == nil {
		t.Fatalf("expected init error in redis strict mode")
	}
	if service != nil {
		t.Fatalf("expected nil service when redis strict init fails")
	}
}

func TestNewBackendRedisNonStrictFallbackToMemory(t *testing.T) {
	service, err := NewBackend("redis", "", "", 0, "gateyes", "rate", false)
	if err != nil {
		t.Fatalf("unexpected init error: %v", err)
	}
	if service == nil {
		t.Fatalf("expected fallback memory service")
	}
	if _, ok := service.(*MemoryBackend); !ok {
		t.Fatalf("expected *MemoryBackend fallback, got %T", service)
	}
}
