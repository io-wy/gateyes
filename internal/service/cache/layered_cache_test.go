package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubCache struct {
	entry     *Entry
	hit       bool
	getErr    error
	setErr    error
	deleted   bool
	setCalled bool
}

func (s *stubCache) Get(context.Context, string) (*Entry, bool, error) {
	return s.entry, s.hit, s.getErr
}

func (s *stubCache) Set(context.Context, string, *Entry, time.Duration) error {
	s.setCalled = true
	return s.setErr
}

func (s *stubCache) Delete(context.Context, string) error {
	s.deleted = true
	return nil
}

func (s *stubCache) Stats() Stats { return Stats{} }
func (s *stubCache) Close() error { return nil }

func TestLayeredCacheGetsL0BeforeL1(t *testing.T) {
	l0 := &stubCache{entry: &Entry{Provider: "l0"}, hit: true}
	l1 := &stubCache{entry: &Entry{Provider: "l1"}, hit: true}
	c := NewLayeredCache(l0, l1)

	entry, hit, err := c.Get(context.Background(), "k")
	if err != nil || !hit || entry.Provider != "l0" {
		t.Fatalf("Get() = (%+v, %v, %v), want l0 hit", entry, hit, err)
	}
}

func TestLayeredCacheFallsBackToL1(t *testing.T) {
	l0 := &stubCache{}
	l1 := &stubCache{entry: &Entry{Provider: "l1"}, hit: true}
	c := NewLayeredCache(l0, l1)

	entry, hit, err := c.Get(context.Background(), "k")
	if err != nil || !hit || entry.Provider != "l1" {
		t.Fatalf("Get() = (%+v, %v, %v), want l1 hit", entry, hit, err)
	}
}

func TestLayeredCacheSetWritesBothAndReturnsL1Error(t *testing.T) {
	wantErr := errors.New("redis down")
	l0 := &stubCache{}
	l1 := &stubCache{setErr: wantErr}
	c := NewLayeredCache(l0, l1)

	err := c.Set(context.Background(), "k", &Entry{Provider: "p"}, time.Minute)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Set() error = %v, want %v", err, wantErr)
	}
	if !l0.setCalled || !l1.setCalled {
		t.Fatalf("setCalled = (%v,%v), want both", l0.setCalled, l1.setCalled)
	}
}

func TestLayeredCacheDeleteDeletesBoth(t *testing.T) {
	l0 := &stubCache{}
	l1 := &stubCache{}
	c := NewLayeredCache(l0, l1)

	if err := c.Delete(context.Background(), "k"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if !l0.deleted || !l1.deleted {
		t.Fatalf("deleted = (%v,%v), want both", l0.deleted, l1.deleted)
	}
}
