package ports

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestHandlersDoNotImportRepository(t *testing.T) {
	root := filepath.Join("..", "handler")
	for _, name := range []string{"handler.go", "admin.go"} {
		path := filepath.Join(root, name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range file.Imports {
			if imp.Path.Value == `"github.com/gateyes/gateway/internal/repository"` {
				t.Errorf("%s must depend on application ports, not repository", path)
			}
		}
	}
}
