package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_EnvFileHydration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configData := `
server:
  listenAddr: :8080
providers:
  - name: p1
    type: openai
    baseURL: http://example.com
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}

	envPath := filepath.Join(dir, ".env1")
	if err := os.WriteFile(envPath, []byte("API_KEY=secret-from-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Providers[0].APIKey != "secret-from-env" {
		t.Errorf("APIKey = %q, want %q", cfg.Providers[0].APIKey, "secret-from-env")
	}
}

func TestReplaceEnvVars_UnknownVar(t *testing.T) {
	input := []byte("key=${UNKNOWN_VAR}")
	got := string(replaceEnvVars(input))
	if got != "key=${UNKNOWN_VAR}" {
		t.Errorf("replaceEnvVars() = %q, want %q", got, "key=${UNKNOWN_VAR}")
	}
}
