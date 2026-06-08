package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxFileLines = 500

var forbiddenPatterns = []struct {
	pattern     *regexp.Regexp
	message     string
	suggestion  string
	category    string
}{
	{
		pattern:    regexp.MustCompile(`fmt\.Println\(`),
		message:    "Forbidden API: fmt.Println() detected",
		suggestion: "Use structured logging (slog/zap) with requestID instead",
		category:   "logging",
	},
	{
		pattern:    regexp.MustCompile(`fmt\.Printf\(`),
		message:    "Forbidden API: fmt.Printf() detected",
		suggestion: "Use structured logging (slog/zap) with requestID instead",
		category:   "logging",
	},
	{
		pattern:    regexp.MustCompile(`log\.Println\(`),
		message:    "Forbidden API: log.Println() detected",
		suggestion: "Use slog/zap with structured fields instead",
		category:   "logging",
	},
	{
		pattern:    regexp.MustCompile(`log\.Printf\(`),
		message:    "Forbidden API: log.Printf() detected",
		suggestion: "Use slog/zap with structured fields instead",
		category:   "logging",
	},
}

var hardcodedPatterns = []struct {
	pattern    *regexp.Regexp
	message    string
	category   string
}{
	{
		pattern:  regexp.MustCompile(`"http://localhost:[0-9]+"`),
		message:  "Hardcoded URL detected",
		category: "hardcoded-url",
	},
	{
		pattern:  regexp.MustCompile(`Timeout:\s*[0-9]+\s*\*\s*time\.(Second|Minute)`),
		message:  "Hardcoded timeout detected",
		category: "hardcoded-timeout",
	},
}

// Violation records a quality issue.
type Violation struct {
	File     string
	Line     int
	Category string
	Message  string
	Fix      string
}

func main() {
	ignoreDirs := []string{"vendor", "third_party", "tmp", "scripts", ".claude", "_test.go"}

	var violations []Violation

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			for _, ignore := range ignoreDirs {
				if strings.Contains(path, ignore) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		v := checkFile(path)
		violations = append(violations, v...)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: walk failed: %v\n", err)
		os.Exit(1)
	}

	// Group by category for summary
	byCat := make(map[string]int)
	for _, v := range violations {
		byCat[v.Category]++
	}

	if len(violations) == 0 {
		fmt.Println("✓ All quality checks passed")
		os.Exit(0)
	}

	fmt.Printf("✗ Found %d quality issue(s):\n\n", len(violations))
	for _, v := range violations {
		fmt.Printf("QUALITY: %s:%d [%s]\n", v.File, v.Line, v.Category)
		fmt.Printf("  %s\n", v.Message)
		if v.Fix != "" {
			fmt.Printf("  Fix: %s\n", v.Fix)
		}
		fmt.Println()
	}

	fmt.Println("--- Summary ---")
	for cat, count := range byCat {
		fmt.Printf("  %s: %d\n", cat, count)
	}
	os.Exit(1)
}

func checkFile(path string) []Violation {
	var violations []Violation

	// Check file size
	lines, err := countLines(path)
	if err == nil && lines > maxFileLines {
		violations = append(violations, Violation{
			File:     path,
			Line:     lines,
			Category: "file-too-large",
			Message:  fmt.Sprintf("File has %d lines (max %d)", lines, maxFileLines),
			Fix:      "Extract utilities into separate files. Keep files under 500 lines.",
		})
	}

	// Check forbidden patterns via text scanning (catches commented-out code too)
	content, err := os.ReadFile(path)
	if err != nil {
		return violations
	}
	text := string(content)

	// Check forbidden API calls
	for _, fp := range forbiddenPatterns {
		matches := fp.pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			line := 1 + strings.Count(text[:m[0]], "\n")
			violations = append(violations, Violation{
				File:     path,
				Line:     line,
				Category: fp.category,
				Message:  fp.message,
				Fix:      fp.suggestion,
			})
		}
	}

	// Check hardcoded values via text scanning
	for _, hp := range hardcodedPatterns {
		matches := hp.pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			line := 1 + strings.Count(text[:m[0]], "\n")
			violations = append(violations, Violation{
				File:     path,
				Line:     line,
				Category: hp.category,
				Message:  hp.message,
				Fix:      "Extract to const or config struct",
			})
		}
	}

	// AST-based checks
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, text, parser.AllErrors)
	if err != nil {
		return violations
	}

	// Check for naked returns of errors
	ast.Inspect(f, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range assign.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "_" {
					pos := fset.Position(assign.Pos())
					violations = append(violations, Violation{
						File:     path,
						Line:     pos.Line,
						Category: "error-swallowing",
						Message:  "Error value assigned to _ (swallowed)",
						Fix:      "Handle the error explicitly: if err != nil { return fmt.Errorf(...): %w, err) }",
					})
				}
			}
		}
		return true
	})

	return violations
}

func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
