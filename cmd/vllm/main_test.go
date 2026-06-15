package main

import (
	"context"
	"strings"
	"testing"
)

func TestBuildCommandArgs(t *testing.T) {
	cmd := buildCommand(context.Background(), "Qwen/Qwen3-0.6B", 8001, "sk-test", 4096, 0.85, true)
	got := strings.Join(cmd.Args, " ")
	want := "vllm serve Qwen/Qwen3-0.6B --port 8001 --api-key sk-test --max-model-len 4096 --gpu-memory-utilization 0.85 --enable-prefix-caching"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildCommandWithoutPrefixCache(t *testing.T) {
	cmd := buildCommand(context.Background(), "Qwen/Qwen3-0.6B", 8002, "sk-test", 4096, 0.85, false)
	got := strings.Join(cmd.Args, " ")
	if strings.Contains(got, "--enable-prefix-caching") {
		t.Fatalf("did not expect --enable-prefix-caching in %q", got)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Qwen/Qwen3-0.6B", "Qwen-Qwen3-0.6B"},
		{"meta-llama/Llama_3.1", "meta-llama-Llama-3.1"},
	}
	for _, c := range cases {
		if got := sanitizeName(c.in); got != c.want {
			t.Fatalf("sanitizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
