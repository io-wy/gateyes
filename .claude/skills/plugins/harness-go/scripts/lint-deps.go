package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// LayerRule defines the layer mapping for a package path pattern.
type LayerRule struct {
	Pattern string `json:"pattern"`
	Layer   int    `json:"layer"`
	Name    string `json:"name"`
}

// Config is loaded from harness.json.
type Config struct {
	Layers      []LayerRule `json:"layers"`
	Module      string      `json:"module"`
	IgnoreDirs  []string    `json:"ignoreDirs"`
	AllowCyclic []string    `json:"allowCyclic"` // packages allowed to break layer rules
}

// Violation records a layer rule breach.
type Violation struct {
	File       string
	Importer   string
	Imported   string
	FromLayer  int
	ToLayer    int
	FromName   string
	ToName     string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: go run scripts/lint-deps.go <module-path> [config-file]\n")
		fmt.Fprintf(os.Stderr, "   or: go run scripts/lint-deps.go --init  (create harness.json template)\n")
		os.Exit(1)
	}

	if os.Args[1] == "--init" {
		initTemplate()
		return
	}

	module := os.Args[1]
	configPath := "harness.json"
	if len(os.Args) >= 3 {
		configPath = os.Args[2]
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: %v, using built-in defaults\n", err)
		cfg = defaultConfig(module)
	}

	violations, err := scan(module, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: scan failed: %v\n", err)
		os.Exit(1)
	}

	if len(violations) == 0 {
		fmt.Println("✓ No layer violations found")
		os.Exit(0)
	}

	fmt.Printf("✗ Found %d layer violation(s):\n\n", len(violations))
	for _, v := range violations {
		printViolation(v)
	}
	os.Exit(1)
}

func initTemplate() {
	tmpl := `{
  "module": "github.com/your-org/your-project",
  "layers": [
    {"pattern": "internal/types", "layer": 0, "name": "types"},
    {"pattern": "internal/domain", "layer": 0, "name": "domain"},
    {"pattern": "pkg/types", "layer": 0, "name": "types"},
    {"pattern": "internal/utils", "layer": 1, "name": "utils"},
    {"pattern": "pkg/utils", "layer": 1, "name": "utils"},
    {"pattern": "internal/app/config", "layer": 2, "name": "config"},
    {"pattern": "internal/core", "layer": 3, "name": "core"},
    {"pattern": "internal/service", "layer": 3, "name": "service"},
    {"pattern": "internal/usecase", "layer": 3, "name": "usecase"},
    {"pattern": "internal/transport/http/handler", "layer": 4, "name": "handler"},
    {"pattern": "internal/api", "layer": 4, "name": "api"},
    {"pattern": "cmd", "layer": 4, "name": "cmd"},
    {"pattern": "internal/cli", "layer": 4, "name": "cli"}
  ],
  "ignoreDirs": ["vendor", "third_party", "tmp", "scripts", ".claude"],
  "allowCyclic": []
}
`
	if err := os.WriteFile("harness.json", []byte(tmpl), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write harness.json: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Created harness.json template. Edit it to match your project structure.")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func defaultConfig(module string) *Config {
	return &Config{
		Module: module,
		Layers: []LayerRule{
			{Pattern: "types", Layer: 0, Name: "types"},
			{Pattern: "domain", Layer: 0, Name: "domain"},
			{Pattern: "utils", Layer: 1, Name: "utils"},
			{Pattern: "config", Layer: 2, Name: "config"},
			{Pattern: "core", Layer: 3, Name: "core"},
			{Pattern: "service", Layer: 3, Name: "service"},
			{Pattern: "usecase", Layer: 3, Name: "usecase"},
			{Pattern: "handler", Layer: 4, Name: "handler"},
			{Pattern: "api", Layer: 4, Name: "api"},
			{Pattern: "cmd", Layer: 4, Name: "cmd"},
			{Pattern: "cli", Layer: 4, Name: "cli"},
		},
		IgnoreDirs:  []string{"vendor", "third_party", "tmp", "scripts", ".claude"},
		AllowCyclic: []string{},
	}
}

func scan(module string, cfg *Config) ([]Violation, error) {
	var violations []Violation

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			for _, ignore := range cfg.IgnoreDirs {
				if strings.Contains(path, ignore) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // skip unparseable files
		}

		importerPath := extractInternalPath(path, module)
		importerLayer, importerName := matchLayer(importerPath, cfg)

		for _, imp := range f.Imports {
			imported := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(imported, module) {
				continue // external dependency, skip
			}

			importedPath := strings.TrimPrefix(imported, module)
			importedPath = strings.TrimPrefix(importedPath, "/")
			importedLayer, importedName := matchLayer(importedPath, cfg)

			// Layer 0 must have NO internal dependencies
			if importerLayer == 0 && importedLayer >= 0 && importedPath != "" {
				violations = append(violations, Violation{
					File:     path,
					Importer: importerPath,
					Imported: importedPath,
					FromLayer: importerLayer,
					ToLayer:   importedLayer,
					FromName:  importerName,
					ToName:    importedName,
				})
				continue
			}

			// Higher layer cannot import lower-or-equal layer (within internal packages)
			if importerLayer > 0 && importedLayer >= 0 && importedLayer >= importerLayer {
				// Same-layer imports are allowed only if explicitly allowed
				if importedLayer == importerLayer {
					continue // same-layer allowed by default
				}
				violations = append(violations, Violation{
					File:      path,
					Importer:  importerPath,
					Imported:  importedPath,
					FromLayer: importerLayer,
					ToLayer:   importedLayer,
					FromName:  importerName,
					ToName:    importedName,
				})
			}
		}

		return nil
	})

	return violations, err
}

// extractInternalPath turns a file path like "internal/transport/http/handler/user.go" into
// the package path relative to module root.
func extractInternalPath(filePath, module string) string {
	dir := filepath.Dir(filePath)
	dir = strings.ReplaceAll(dir, string(os.PathSeparator), "/")
	return dir
}

// matchLayer finds the layer for a given package path.
func matchLayer(pkgPath string, cfg *Config) (int, string) {
	for _, rule := range cfg.Layers {
		if strings.Contains(pkgPath, rule.Pattern) {
			return rule.Layer, rule.Name
		}
	}
	return -1, "unknown"
}

func printViolation(v Violation) {
	if v.FromLayer == 0 {
		fmt.Printf("LAYER VIOLATION: %s\n", v.File)
		fmt.Printf("  %s (%s, Layer %d) imports %s (%s, Layer %d)\n",
			v.Importer, v.FromName, v.FromLayer, v.Imported, v.ToName, v.ToLayer)
		fmt.Printf("  Rule: Layer 0 packages must have NO internal dependencies.\n")
		fmt.Printf("  Fix:  Move config-dependent logic to a higher layer, or pass the value as parameter.\n\n")
		return
	}

	fmt.Printf("LAYER VIOLATION: %s\n", v.File)
	fmt.Printf("  %s (%s, Layer %d) imports %s (%s, Layer %d)\n",
		v.Importer, v.FromName, v.FromLayer, v.Imported, v.ToName, v.ToLayer)
	fmt.Printf("  Rule: Layer %d can only import layers < %d. Higher layers cannot import lower/equal layers.\n",
		v.FromLayer, v.FromLayer)
	fmt.Printf("  Fix:  Introduce an interface in a lower layer, or restructure to avoid the dependency.\n\n")
}
