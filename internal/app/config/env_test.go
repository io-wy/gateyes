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

func TestLoad_ProjectDotEnvSubstitution(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configData := `
server:
  listenAddr: ${GATEYES_TEST_LISTEN_ADDR}
providers:
  - name: p1
    type: openai
    baseURL: http://example.com
    apiKey: ${GATEYES_TEST_PROVIDER_KEY}
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GATEYES_TEST_LISTEN_ADDR=:19090\nGATEYES_TEST_PROVIDER_KEY=dotenv-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("GATEYES_TEST_LISTEN_ADDR")
	os.Unsetenv("GATEYES_TEST_PROVIDER_KEY")
	t.Cleanup(func() {
		os.Unsetenv("GATEYES_TEST_LISTEN_ADDR")
		os.Unsetenv("GATEYES_TEST_PROVIDER_KEY")
	})

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Server.ListenAddr != ":19090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.Server.ListenAddr, ":19090")
	}
	if cfg.Providers[0].APIKey != "dotenv-key" {
		t.Errorf("APIKey = %q, want %q", cfg.Providers[0].APIKey, "dotenv-key")
	}
}

func TestLoad_EnvironmentOverridesDotEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configData := `
server:
  listenAddr: :8080
providers:
  - name: p1
    type: openai
    baseURL: http://example.com
    apiKey: ${GATEYES_TEST_PROVIDER_KEY}
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GATEYES_TEST_PROVIDER_KEY=dotenv-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GATEYES_TEST_PROVIDER_KEY", "process-env-key")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Providers[0].APIKey != "process-env-key" {
		t.Errorf("APIKey = %q, want %q", cfg.Providers[0].APIKey, "process-env-key")
	}
}
