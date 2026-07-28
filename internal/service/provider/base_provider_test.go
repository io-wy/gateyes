package provider

import (
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestResolveModel(t *testing.T) {
	cases := []struct {
		name      string
		aliases   map[string]string
		requested string
		want      string
	}{
		{
			name:      "no aliases",
			aliases:   nil,
			requested: "claude-sonnet-4-6",
			want:      "claude-sonnet-4-6",
		},
		{
			name:      "exact alias match",
			aliases:   map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"},
			requested: "claude-sonnet-4-6",
			want:      "deepseek-v4-flash",
		},
		{
			name:      "no match returns requested",
			aliases:   map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"},
			requested: "gpt-5.4",
			want:      "gpt-5.4",
		},
		{
			name:      "chained alias",
			aliases:   map[string]string{"claude-sonnet-4-6": "alias-a", "alias-a": "deepseek-v4-flash"},
			requested: "claude-sonnet-4-6",
			want:      "deepseek-v4-flash",
		},
		{
			name:      "cycle returns original",
			aliases:   map[string]string{"a": "b", "b": "a"},
			requested: "a",
			want:      "a",
		},
		{
			name:      "empty alias ignored",
			aliases:   map[string]string{"claude-sonnet-4-6": ""},
			requested: "claude-sonnet-4-6",
			want:      "claude-sonnet-4-6",
		},
		{
			name:      "empty requested returns empty",
			aliases:   map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"},
			requested: "",
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveModel(tc.aliases, tc.requested)
			if got != tc.want {
				t.Errorf("resolveModel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBaseProviderResolveModel(t *testing.T) {
	bp := newBaseProvider(config.ProviderConfig{
		ModelAliases: map[string]string{
			"claude-sonnet-4-6": "deepseek-v4-flash",
		},
	})
	if got := bp.ResolveModel("claude-sonnet-4-6"); got != "deepseek-v4-flash" {
		t.Errorf("ResolveModel() = %q, want deepseek-v4-flash", got)
	}
	if got := bp.ResolveModel("unknown"); got != "unknown" {
		t.Errorf("ResolveModel() = %q, want unknown", got)
	}
}
