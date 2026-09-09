package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/gateyes/gateway/"

func TestDependencyDirection(t *testing.T) {
	rules := []struct {
		dir       string
		forbidden []string
	}{
		{"../ports", []string{"internal/application", "internal/handler", "internal/transport"}},
		{"../application", []string{"internal/handler", "internal/transport"}},
		{"../transport", []string{"internal/repository/sqlstore", "internal/app/gateway"}},
		{"../handler", []string{"internal/repository/sqlstore", "internal/app/gateway"}},
	}

	for _, rule := range rules {
		rule := rule
		t.Run(filepath.Base(rule.dir), func(t *testing.T) {
			walkImports(t, rule.dir, func(path, imported string) {
				for _, prefix := range rule.forbidden {
					if strings.HasPrefix(imported, modulePath+prefix) {
						t.Errorf("%s imports forbidden dependency %s", path, imported)
					}
				}
			})
		})
	}
}

func TestGatewayCompositionRootUsesInferenceOrchestrator(t *testing.T) {
	path := filepath.Join("..", "app", "gateway", "app.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var found bool
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "NewOrchestrated" {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		found = ok && ident.Name == "inference"
		return !found
	})
	if !found {
		t.Fatal("gateway composition root must construct inference.NewOrchestrated")
	}
}

func TestGatewayCompositionRootUsesAdministrationAdapters(t *testing.T) {
	path := filepath.Join("..", "app", "gateway", "app.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	wanted := map[string]bool{"NewConsole": false, "NewCatalog": false, "NewRuntimeConfig": false}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if ok && ident.Name == "administration" {
			if _, exists := wanted[selector.Sel.Name]; exists {
				wanted[selector.Sel.Name] = true
			}
		}
		return true
	})
	for constructor, found := range wanted {
		if !found {
			t.Errorf("gateway composition root must construct administration.%s", constructor)
		}
	}
}

func TestAdminHandlerDoesNotConstructConsoleService(t *testing.T) {
	path := filepath.Join("..", "handler", "admin.go")
	walkImports(t, path, func(path, imported string) {
		if imported == modulePath+"internal/service/adminconsole" {
			t.Errorf("%s must receive the console application port from the composition root", path)
		}
	})
}

func walkImports(t *testing.T, root string, visit func(path, imported string)) {
	t.Helper()
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	if !info.IsDir() {
		visitFileImports(t, root, visit)
		return
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		visitFileImports(t, path, visit)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func visitFileImports(t *testing.T, path string, visit func(path, imported string)) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", path, err)
		}
		visit(path, imported)
	}
}
