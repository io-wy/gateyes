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
	pattern    *regexp.Regexp
	message    string
	suggestion string
	category   string
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
	pattern  *regexp.Regexp
	message  string
	category string
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
	ignoreDirs := []string{"vendor", "third_party", "tmp", "scripts", ".claude"}

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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".pb.go") {
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

	byCat := make(map[string]int)
	var failures []Violation
	var warnings []Violation
	for _, v := range violations {
		byCat[v.Category]++
		if strings.HasPrefix(v.Category, "warning-") {
			warnings = append(warnings, v)
			continue
		}
		failures = append(failures, v)
	}

	if len(failures) == 0 && len(warnings) == 0 {
		fmt.Println("✓ All quality checks passed")
		os.Exit(0)
	}

	if len(failures) > 0 {
		fmt.Printf("✗ Found %d quality issue(s):\n\n", len(failures))
		for _, v := range failures {
			printViolation("QUALITY", v)
		}
	}
	if len(warnings) > 0 {
		fmt.Printf("⚠ Found %d quality warning(s):\n\n", len(warnings))
		for _, v := range warnings {
			printViolation("WARNING", v)
		}
	}

	fmt.Println("--- Summary ---")
	for cat, count := range byCat {
		fmt.Printf("  %s: %d\n", cat, count)
	}
	if len(failures) > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}

func printViolation(prefix string, v Violation) {
	fmt.Printf("%s: %s:%d [%s]\n", prefix, v.File, v.Line, v.Category)
	fmt.Printf("  %s\n", v.Message)
	if v.Fix != "" {
		fmt.Printf("  Fix: %s\n", v.Fix)
	}
	fmt.Println()
}

func checkFile(path string) []Violation {
	var violations []Violation

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

	content, err := os.ReadFile(path)
	if err != nil {
		return violations
	}
	text := string(content)

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

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, text, parser.AllErrors)
	if err != nil {
		return violations
	}

	ast.Inspect(f, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if assignmentCapturesError(assign) {
			return true
		}
		for i, lhs := range assign.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok || ident.Name != "_" || i != len(assign.Lhs)-1 {
				continue
			}
			pos := fset.Position(assign.Pos())
			violations = append(violations, Violation{
				File:     path,
				Line:     pos.Line,
				Category: "warning-error-swallowing",
				Message:  "Last return value assigned to _; this often swallows an error",
				Fix:      "Capture and handle the error explicitly when the ignored value is an error.",
			})
		}
		return true
	})

	return violations
}

func assignmentCapturesError(assign *ast.AssignStmt) bool {
	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if ok && ident.Name == "err" {
			return true
		}
	}
	return false
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
