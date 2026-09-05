package proto_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProtoToolchainIsPinnedAndGated(t *testing.T) {
	root := repositoryRoot(t)
	files := map[string]string{
		"Makefile":                 readFile(t, filepath.Join(root, "Makefile")),
		"buf.gen.yaml":             readFile(t, filepath.Join(root, "buf.gen.yaml")),
		".github/workflows/ci.yml": readFile(t, filepath.Join(root, ".github", "workflows", "ci.yml")),
	}

	required := map[string][]string{
		"Makefile": {
			"BUF_VERSION := 1.72.0",
			"PROTO_BASELINE ?= .git\\#branch=origin/main",
			"proto-check:",
			"git status --porcelain --untracked-files=all",
			"proto-lint:",
			"proto-breaking:",
			"pkg/plugin",
		},
		"buf.gen.yaml": {
			"buf.build/protocolbuffers/go:v1.36.11",
			"buf.build/grpc/go:v1.6.2",
			"- proto\n",
		},
		".github/workflows/ci.yml": {
			"make proto-check",
			"make proto-lint",
			"make proto-breaking",
		},
	}

	for name, needles := range required {
		for _, needle := range needles {
			if !strings.Contains(files[name], needle) {
				t.Errorf("%s must contain %q", name, needle)
			}
		}
	}

	for _, name := range []string{"Makefile", "buf.gen.yaml"} {
		content := files[name]
		if strings.Contains(content, "@latest") {
			t.Errorf("%s must not use an unpinned @latest tool", name)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate proto toolchain test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
