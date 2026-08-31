package handler

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

var updateAdminContractGoldens = flag.Bool("update-admin-contract-goldens", false, "rewrite Admin contract golden fixtures")

func TestContractAdminV1MeResponse(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	seedAdminToken(t, env, repository.RoleSuperAdmin, "contract-admin", "secret")
	token := "contract-admin:secret"

	v1 := performJSONRequest(t, env, http.MethodGet, "/admin/v1/me", token, "")
	if v1.Code != http.StatusOK {
		t.Fatalf("GET /admin/v1/me status = %d, want %d: %s", v1.Code, http.StatusOK, v1.Body.String())
	}
	want := assertAdminContractFixture(t, "me.json", v1.Body.Bytes())

	legacy := performJSONRequest(t, env, http.MethodGet, "/admin/me", token, "")
	if legacy.Code != http.StatusOK {
		t.Fatalf("GET /admin/me status = %d, want %d: %s", legacy.Code, http.StatusOK, legacy.Body.String())
	}
	got := normalizeAdminContractJSON(t, legacy.Body.Bytes())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy /admin/me response differs from Admin v1\ngot:\n%s\nwant:\n%s", prettyAdminContractJSON(got), prettyAdminContractJSON(want))
	}
}

func TestContractAdminV1ProvidersResponse(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	seedAdminToken(t, env, repository.RoleSuperAdmin, "contract-provider-admin", "secret")
	token := "contract-provider-admin:secret"

	v1 := performJSONRequest(t, env, http.MethodGet, "/admin/v1/providers", token, "")
	if v1.Code != http.StatusOK {
		t.Fatalf("GET /admin/v1/providers status = %d, want %d: %s", v1.Code, http.StatusOK, v1.Body.String())
	}
	want := assertAdminContractFixture(t, "providers.json", v1.Body.Bytes())

	legacy := performJSONRequest(t, env, http.MethodGet, "/admin/providers", token, "")
	if legacy.Code != http.StatusOK {
		t.Fatalf("GET /admin/providers status = %d, want %d: %s", legacy.Code, http.StatusOK, legacy.Body.String())
	}
	got := normalizeAdminContractJSON(t, legacy.Body.Bytes())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy /admin/providers response differs from Admin v1\ngot:\n%s\nwant:\n%s", prettyAdminContractJSON(got), prettyAdminContractJSON(want))
	}
}

func assertAdminContractFixture(t *testing.T, name string, actual []byte) any {
	t.Helper()
	got := normalizeAdminContractJSON(t, actual)
	path := filepath.Join("..", "..", "testdata", "contracts", "http", "admin", name)
	if *updateAdminContractGoldens {
		if err := os.WriteFile(path, append([]byte(prettyAdminContractJSON(got)), '\n'), 0o644); err != nil {
			t.Fatalf("write Admin contract fixture %s: %v", path, err)
		}
		return got
	}
	wantBody, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Admin contract fixture %s: %v (run with -update-admin-contract-goldens to create it)\nactual:\n%s", path, err, prettyAdminContractJSON(got))
	}
	want := normalizeAdminContractJSON(t, wantBody)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Admin contract %s mismatch\ngot:\n%s\nwant:\n%s", name, prettyAdminContractJSON(got), prettyAdminContractJSON(want))
	}
	return want
}

func normalizeAdminContractJSON(t *testing.T, body []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode Admin contract JSON: %v\nbody: %s", err, body)
	}
	normalizeAdminContractValue(value)
	return value
}

func normalizeAdminContractValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch key {
			case "created_at", "updated_at":
				if item != nil && item != "" {
					typed[key] = "<timestamp>"
				}
			case "user_id", "api_key_id":
				if item != nil && item != "" {
					typed[key] = "<id>"
				}
			default:
				normalizeAdminContractValue(item)
			}
		}
	case []any:
		for _, item := range typed {
			normalizeAdminContractValue(item)
		}
	}
}

func prettyAdminContractJSON(value any) string {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return strings.TrimSuffix(body.String(), "\n")
}
