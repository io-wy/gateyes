package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AuditResult holds the overall score and breakdown.
type AuditResult struct {
	Score       int
	Category    string
	Breakdown   map[string]int
	Suggestions []string
}

func main() {
	module := ""
	if len(os.Args) > 1 {
		module = os.Args[1]
	}

	result := runAudit(module)

	fmt.Printf("=== Harness Audit Report ===\n\n")
	fmt.Printf("Score: %d/100 (%s)\n\n", result.Score, result.Category)

	fmt.Println("Breakdown:")
	for k, v := range result.Breakdown {
		fmt.Printf("  %-25s %3d/20\n", k+":", v)
	}

	if len(result.Suggestions) > 0 {
		fmt.Println("\nSuggestions:")
		for _, s := range result.Suggestions {
			fmt.Printf("  - %s\n", s)
		}
	}

	// Exit 1 if score is low to fail CI
	if result.Score < 50 {
		os.Exit(1)
	}
}

func runAudit(module string) AuditResult {
	result := AuditResult{
		Breakdown:   make(map[string]int),
		Suggestions: []string{},
	}

	// 1. Documentation (20 pts)
	docScore := 0
	if fileExists("CLAUDE.md") || fileExists("AGENTS.md") {
		docScore += 10
	} else {
		result.Suggestions = append(result.Suggestions, "Create CLAUDE.md or AGENTS.md with project constraints")
	}
	if dirExists("docs") {
		docScore += 5
	} else {
		result.Suggestions = append(result.Suggestions, "Create docs/ directory for architecture and development guides")
	}
	if fileExists("README.md") {
		docScore += 5
	}
	result.Breakdown["Documentation"] = docScore

	// 2. Layer Config (20 pts)
	layerScore := 0
	if fileExists("harness.json") {
		layerScore += 15
		// Check if config is valid
		if _, err := os.ReadFile("harness.json"); err == nil {
			layerScore += 5
		}
	} else {
		result.Suggestions = append(result.Suggestions, "Create harness.json with layer mapping (run: go run .claude/skills/harness-go/scripts/lint-deps.go --init)")
	}
	result.Breakdown["Layer Config"] = layerScore

	// 3. Lint Scripts (20 pts)
	lintScore := 0
	if fileExists(".claude/skills/harness-go/scripts/lint-deps.go") {
		lintScore += 10
	} else {
		result.Suggestions = append(result.Suggestions, "Add .claude/skills/harness-go/scripts/lint-deps.go for layer dependency checking")
	}
	if fileExists(".claude/skills/harness-go/scripts/lint-quality.go") {
		lintScore += 10
	} else {
		result.Suggestions = append(result.Suggestions, "Add .claude/skills/harness-go/scripts/lint-quality.go for code quality checking")
	}
	result.Breakdown["Lint Scripts"] = lintScore

	// 4. Verify Infrastructure (20 pts)
	verifyScore := 0
	if dirExists("scripts/verify") {
		verifyScore += 10
		// Count verify scripts
		count := countFiles("scripts/verify")
		if count > 0 {
			verifyScore += 5
		}
		if count >= 3 {
			verifyScore += 5
		}
	} else {
		result.Suggestions = append(result.Suggestions, "Create scripts/verify/ with end-to-end verification scripts")
	}
	result.Breakdown["Verify Infra"] = verifyScore

	// 5. Test Coverage (20 pts)
	testScore := 0
	if hasGoTests(".") {
		testScore += 10
	} else {
		result.Suggestions = append(result.Suggestions, "Add Go test files (_test.go) to your packages")
	}
	if fileExists("Makefile") {
		content, _ := os.ReadFile("Makefile")
		if strings.Contains(string(content), "test") {
			testScore += 5
		}
		if strings.Contains(string(content), "lint") {
			testScore += 5
		}
	} else {
		result.Suggestions = append(result.Suggestions, "Create Makefile with test and lint targets")
	}
	result.Breakdown["Test Coverage"] = testScore

	// Calculate total
	total := 0
	for _, v := range result.Breakdown {
		total += v
	}
	result.Score = total

	// Category
	switch {
	case total >= 80:
		result.Category = "Healthy"
	case total >= 50:
		result.Category = "Needs Improvement"
	case total >= 20:
		result.Category = "Basic"
	default:
		result.Category = "Barely Started"
	}

	return result
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func countFiles(dir string) int {
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func hasGoTests(root string) bool {
	found := false
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
