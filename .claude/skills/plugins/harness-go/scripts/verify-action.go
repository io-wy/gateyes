package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config mirrors lint-deps config.
type Config struct {
	Layers     []LayerRule `json:"layers"`
	Module     string      `json:"module"`
	IgnoreDirs []string    `json:"ignoreDirs"`
}

type LayerRule struct {
	Pattern string `json:"pattern"`
	Layer   int    `json:"layer"`
	Name    string `json:"name"`
}

func main() {
	if len(os.Args) < 4 || os.Args[1] != "--action" {
		fmt.Fprintf(os.Stderr, "Usage: go run scripts/verify-action.go --action \"<description>\" <module-path> [config-file]\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  go run scripts/verify-action.go --action \"create file internal/types/user.go\" github.com/example/proj\n")
		fmt.Fprintf(os.Stderr, "  go run scripts/verify-action.go --action \"import internal/core from internal/transport/http/handler\" github.com/example/proj\n")
		os.Exit(1)
	}

	action := os.Args[2]
	module := os.Args[3]
	configPath := "harness.json"
	if len(os.Args) >= 5 {
		configPath = os.Args[4]
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		cfg = defaultConfig(module)
	}

	result := verify(action, module, cfg)
	fmt.Println(result.Message)
	if !result.Valid {
		os.Exit(1)
	}
}

type Result struct {
	Valid   bool
	Message string
}

func verify(action, module string, cfg *Config) Result {
	action = strings.ToLower(action)

	// Case 1: Create file
	if strings.Contains(action, "create") || strings.Contains(action, "add file") {
		return verifyCreateFile(action, cfg)
	}

	// Case 2: Import
	if strings.Contains(action, "import") {
		return verifyImport(action, cfg)
	}

	// Case 3: Modify signature
	if strings.Contains(action, "modify signature") || strings.Contains(action, "change signature") {
		return Result{Valid: true, Message: "✓ VALID: Signature changes require manual call-site scan. Run: grep -rn \"funcName\" internal/"}
	}

	return Result{Valid: true, Message: "✓ VALID: No structural constraints apply to this action"}
}

func verifyCreateFile(action string, cfg *Config) Result {
	// Extract file path from action description
	// e.g. "create file internal/types/user.go" or "add file internal/transport/http/handler/auth.go"
	var filePath string
	words := strings.Fields(action)
	for i, w := range words {
		if (w == "file" || w == "path") && i+1 < len(words) {
			filePath = words[i+1]
			break
		}
		// Also detect paths directly (words containing "/")
		if strings.Contains(w, "/") && strings.HasSuffix(w, ".go") {
			filePath = w
			break
		}
	}

	if filePath == "" {
		return Result{Valid: true, Message: "✓ VALID: Could not extract file path from action. Manual review recommended."}
	}

	dir := filepath.Dir(filePath)
	layer, name := matchLayer(dir, cfg)

	if layer < 0 {
		return Result{
			Valid:   true,
			Message: fmt.Sprintf("✓ VALID: %s does not match any known layer. No layer constraints apply.", filePath),
		}
	}

	// Check naming convention
	base := filepath.Base(filePath)
	if !isValidGoFilename(base) {
		return Result{
			Valid:   false,
			Message: fmt.Sprintf("✗ INVALID: File name '%s' does not follow naming convention.\n  Fix: Use snake_case for file names (e.g., user_profile.go)", base),
		}
	}

	msg := fmt.Sprintf("✓ VALID: %s belongs to %s (Layer %d). ", filePath, name, layer)
	if layer == 0 {
		msg += "Layer 0: pure type definitions, no internal dependencies allowed."
	} else {
		msg += fmt.Sprintf("Can import layers < %d.", layer)
	}
	return Result{Valid: true, Message: msg}
}

func verifyImport(action string, cfg *Config) Result {
	// Parse "import X from Y" or "Y imports X"
	var importer, imported string

	words := strings.Fields(action)
	for i, w := range words {
		if w == "from" && i > 0 {
			importer = words[i+1]
			imported = words[i-1]
			break
		}
		if w == "import" && i+1 < len(words) {
			imported = words[i+1]
		}
		if w == "imports" && i+1 < len(words) {
			imported = words[i+1]
			importer = words[i-1]
		}
	}

	if importer == "" || imported == "" {
		return Result{Valid: true, Message: "✓ VALID: Could not parse importer/imported. Manual review recommended."}
	}

	fromLayer, fromName := matchLayer(importer, cfg)
	toLayer, toName := matchLayer(imported, cfg)

	if fromLayer < 0 || toLayer < 0 {
		return Result{
			Valid:   true,
			Message: fmt.Sprintf("✓ VALID: One or both packages are external. No internal layer constraints apply."),
		}
	}

	if fromLayer == 0 {
		return Result{
			Valid: false,
			Message: fmt.Sprintf("✗ INVALID: %s (%s, Layer %d) cannot import %s (%s, Layer %d).\n"+
				"  Rule: Layer 0 packages must have NO internal dependencies.\n"+
				"  Fix: Move config-dependent logic to a higher layer, or pass the value as parameter.",
				importer, fromName, fromLayer, imported, toName, toLayer),
		}
	}

	if toLayer >= fromLayer {
		return Result{
			Valid: false,
			Message: fmt.Sprintf("✗ INVALID: %s (%s, Layer %d) cannot import %s (%s, Layer %d).\n"+
				"  Rule: Layer %d can only import layers < %d.\n"+
				"  Fix: Introduce an interface in a lower layer, or restructure to avoid the dependency.",
				importer, fromName, fromLayer, imported, toName, toLayer, fromLayer, fromLayer),
		}
	}

	return Result{
		Valid:   true,
		Message: fmt.Sprintf("✓ VALID: %s (Layer %d) imports %s (Layer %d). Direction is correct.", importer, fromLayer, imported, toLayer),
	}
}

func matchLayer(pkgPath string, cfg *Config) (int, string) {
	for _, rule := range cfg.Layers {
		if strings.Contains(pkgPath, rule.Pattern) {
			return rule.Layer, rule.Name
		}
	}
	return -1, "unknown"
}

func isValidGoFilename(name string) bool {
	// Must be snake_case, end with .go
	if !strings.HasSuffix(name, ".go") {
		return false
	}
	base := strings.TrimSuffix(name, ".go")
	// No uppercase, no spaces
	for _, r := range base {
		if r >= 'A' && r <= 'Z' {
			return false
		}
		if r == ' ' {
			return false
		}
	}
	return true
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
		IgnoreDirs: []string{"vendor", "third_party", "tmp", "scripts", ".claude"},
	}
}
