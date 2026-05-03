package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type mockReloadable struct {
	reloaded bool
	cfg      *Config
	err      error
}

func (m *mockReloadable) Reload(cfg *Config) error {
	m.reloaded = true
	m.cfg = cfg
	return m.err
}

func (m *mockReloadable) Name() string { return "mock" }

func TestReloader_Register(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `server:
  listenAddr: :8080
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewReloader(path)
	m := &mockReloadable{}
	r.Register(m)

	if err := r.Reload(context.Background()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	if !m.reloaded {
		t.Error("registered service should receive reload")
	}
	if m.cfg == nil {
		t.Error("service should receive the new config")
	}
}

func TestReloader_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `server:
  listenAddr: :8080
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewReloader(path)
	m1 := &mockReloadable{}
	m2 := &mockReloadable{}
	r.Register(m1, m2)

	if err := r.Reload(context.Background()); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	if !m1.reloaded || !m2.reloaded {
		t.Error("all registered services should receive reload")
	}
}

func TestReloader_Reload_ErrorAggregation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `server:
  listenAddr: :8080
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewReloader(path)
	m1 := &mockReloadable{err: errors.New("fail1")}
	m2 := &mockReloadable{err: errors.New("fail2")}
	r.Register(m1, m2)

	err := r.Reload(context.Background())
	if err == nil {
		t.Fatal("expected error from first failing service")
	}
	if err.Error() != "mock: fail1" {
		t.Errorf("error = %q, want %q", err.Error(), "mock: fail1")
	}
}
