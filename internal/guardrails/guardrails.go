package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// Guardrails provides security and safety checks for Agent requests/responses
type Guardrails struct {
	config  GuardrailsConfig
	filters []Filter
	mu      sync.RWMutex
}

// GuardrailsConfig defines guardrails configuration
type GuardrailsConfig struct {
	Enabled           bool
	PIIDetection      PIIDetectionConfig
	ContentFilter     ContentFilterConfig
	ResponseValidator ResponseValidatorConfig
	AnomalyDetection  AnomalyDetectionConfig
	RateLimiting      RateLimitingConfig
	CustomRules       []CustomRule
}

// PIIDetectionConfig defines PII detection settings
type PIIDetectionConfig struct {
	Enabled    bool
	Redact     bool     // Redact or reject
	Patterns   []string // Custom regex patterns
	EntityTypes []string // email, phone, ssn, credit_card, etc.
}

// ContentFilterConfig defines content filtering settings
type ContentFilterConfig struct {
	Enabled         bool
	BlockProfanity  bool
	BlockToxicity   bool
	BlockPromptInjection bool
	CustomBlocklist []string
	ToxicityThreshold float64 // 0.0 to 1.0
}

// ResponseValidatorConfig defines response validation settings
type ResponseValidatorConfig struct {
	Enabled           bool
	MaxTokens         int
	MaxResponseSize   int64 // bytes
	ValidateJSON      bool
	ValidateStructure bool
	RequiredFields    []string
}

// AnomalyDetectionConfig defines anomaly detection settings
type AnomalyDetectionConfig struct {
	Enabled                bool
	MaxRequestsPerMinute   int
	MaxTokensPerRequest    int
	SuspiciousPatterns     []string
	BlockRepeatedRequests  bool
	BlockRapidRequests     bool
}

// RateLimitingConfig defines per-agent rate limiting
type RateLimitingConfig struct {
	Enabled           bool
	RequestsPerMinute int
	TokensPerMinute   int
	BurstSize         int
}

// CustomRule defines a custom guardrail rule
type CustomRule struct {
	Name        string
	Description string
	Type        string // "request", "response", "both"
	Pattern     string
	Action      string // "block", "redact", "warn", "log"
	Message     string
}

// Filter interface for guardrail filters
type Filter interface {
	Name() string
	Check(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error)
}

// FilterResult represents the result of a filter check
type FilterResult struct {
	Passed      bool
	Action      string // "allow", "block", "redact", "warn"
	Message     string
	Redacted    string // Redacted content if action is "redact"
	Violations  []Violation
	Metadata    map[string]interface{}
}

// Violation represents a specific violation found
type Violation struct {
	Type     string
	Severity string // "low", "medium", "high", "critical"
	Message  string
	Location string
	Value    string
}

// NewGuardrails creates a new guardrails instance
func NewGuardrails(config GuardrailsConfig) (*Guardrails, error) {
	g := &Guardrails{
		config:  config,
		filters: make([]Filter, 0),
	}

	// Initialize filters based on configuration
	if config.PIIDetection.Enabled {
		g.filters = append(g.filters, NewPIIFilter(config.PIIDetection))
	}

	if config.ContentFilter.Enabled {
		g.filters = append(g.filters, NewContentFilter(config.ContentFilter))
	}

	if config.AnomalyDetection.Enabled {
		g.filters = append(g.filters, NewAnomalyFilter(config.AnomalyDetection))
	}

	// Add custom rule filters
	for _, rule := range config.CustomRules {
		filter, err := NewCustomRuleFilter(rule)
		if err != nil {
			slog.Warn("failed to create custom rule filter",
				"rule", rule.Name,
				"error", err,
			)
			continue
		}
		g.filters = append(g.filters, filter)
	}

	slog.Info("guardrails initialized",
		"filters", len(g.filters),
		"pii_detection", config.PIIDetection.Enabled,
		"content_filter", config.ContentFilter.Enabled,
	)

	return g, nil
}

// CheckRequest validates an incoming request
func (g *Guardrails) CheckRequest(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error) {
	if !g.config.Enabled {
		return &FilterResult{Passed: true, Action: "allow"}, nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	aggregatedResult := &FilterResult{
		Passed:     true,
		Action:     "allow",
		Violations: make([]Violation, 0),
		Metadata:   make(map[string]interface{}),
	}

	redactedContent := content

	// Run all filters
	for _, filter := range g.filters {
		result, err := filter.Check(ctx, redactedContent, metadata)
		if err != nil {
			slog.Error("filter check failed",
				"filter", filter.Name(),
				"error", err,
			)
			continue
		}

		// Aggregate results
		if !result.Passed {
			aggregatedResult.Passed = false
			aggregatedResult.Violations = append(aggregatedResult.Violations, result.Violations...)

			// Determine most restrictive action
			if result.Action == "block" {
				aggregatedResult.Action = "block"
				aggregatedResult.Message = result.Message
				break // Stop processing on block
			} else if result.Action == "redact" && aggregatedResult.Action != "block" {
				aggregatedResult.Action = "redact"
				redactedContent = result.Redacted
			} else if result.Action == "warn" && aggregatedResult.Action == "allow" {
				aggregatedResult.Action = "warn"
			}
		}

		// Merge metadata
		for k, v := range result.Metadata {
			aggregatedResult.Metadata[k] = v
		}
	}

	if aggregatedResult.Action == "redact" {
		aggregatedResult.Redacted = redactedContent
	}

	// Log violations
	if len(aggregatedResult.Violations) > 0 {
		slog.Warn("guardrail violations detected",
			"action", aggregatedResult.Action,
			"violations", len(aggregatedResult.Violations),
		)
	}

	return aggregatedResult, nil
}

// CheckResponse validates an outgoing response
func (g *Guardrails) CheckResponse(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error) {
	if !g.config.Enabled {
		return &FilterResult{Passed: true, Action: "allow"}, nil
	}

	// Similar to CheckRequest but for responses
	return g.CheckRequest(ctx, content, metadata)
}

// ValidateResponseStructure validates response structure
func (g *Guardrails) ValidateResponseStructure(response []byte) error {
	if !g.config.Enabled || !g.config.ResponseValidator.Enabled {
		return nil
	}

	// Check size
	if g.config.ResponseValidator.MaxResponseSize > 0 {
		if int64(len(response)) > g.config.ResponseValidator.MaxResponseSize {
			return fmt.Errorf("response size %d exceeds limit %d",
				len(response), g.config.ResponseValidator.MaxResponseSize)
		}
	}

	// Validate JSON if required
	if g.config.ResponseValidator.ValidateJSON {
		var data map[string]interface{}
		if err := json.Unmarshal(response, &data); err != nil {
			return fmt.Errorf("invalid JSON response: %w", err)
		}

		// Check required fields
		for _, field := range g.config.ResponseValidator.RequiredFields {
			if _, ok := data[field]; !ok {
				return fmt.Errorf("missing required field: %s", field)
			}
		}
	}

	return nil
}

// GetStats returns guardrails statistics
func (g *Guardrails) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return map[string]interface{}{
		"enabled":       g.config.Enabled,
		"filters_count": len(g.filters),
		"filters":       g.getFilterNames(),
	}
}

// getFilterNames returns names of all active filters
func (g *Guardrails) getFilterNames() []string {
	names := make([]string, len(g.filters))
	for i, filter := range g.filters {
		names[i] = filter.Name()
	}
	return names
}

// PIIFilter detects and redacts PII
type PIIFilter struct {
	config   PIIDetectionConfig
	patterns map[string]*regexp.Regexp
}

// NewPIIFilter creates a new PII filter
func NewPIIFilter(config PIIDetectionConfig) *PIIFilter {
	filter := &PIIFilter{
		config:   config,
		patterns: make(map[string]*regexp.Regexp),
	}

	// Default PII patterns
	defaultPatterns := map[string]string{
		"email":       `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
		"phone":       `\b(\+\d{1,3}[-.]?)?\(?\d{3}\)?[-.]?\d{3}[-.]?\d{4}\b`,
		"ssn":         `\b\d{3}-\d{2}-\d{4}\b`,
		"credit_card": `\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`,
		"ip_address":  `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
	}

	// Compile patterns
	for name, pattern := range defaultPatterns {
		if contains(config.EntityTypes, name) || len(config.EntityTypes) == 0 {
			re, err := regexp.Compile(pattern)
			if err != nil {
				slog.Error("failed to compile PII pattern",
					"type", name,
					"error", err,
				)
				continue
			}
			filter.patterns[name] = re
		}
	}

	// Add custom patterns
	for i, pattern := range config.Patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			slog.Error("failed to compile custom PII pattern",
				"index", i,
				"error", err,
			)
			continue
		}
		filter.patterns[fmt.Sprintf("custom_%d", i)] = re
	}

	return filter
}

// Name returns the filter name
func (f *PIIFilter) Name() string {
	return "pii_detection"
}

// Check checks for PII in content
func (f *PIIFilter) Check(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error) {
	violations := make([]Violation, 0)
	redacted := content

	// Check each pattern
	for piiType, pattern := range f.patterns {
		matches := pattern.FindAllString(content, -1)
		if len(matches) > 0 {
			for _, match := range matches {
				violations = append(violations, Violation{
					Type:     "pii_detected",
					Severity: "high",
					Message:  fmt.Sprintf("Detected %s", piiType),
					Value:    match,
				})

				// Redact if configured
				if f.config.Redact {
					redacted = strings.ReplaceAll(redacted, match, "[REDACTED]")
				}
			}
		}
	}

	if len(violations) > 0 {
		action := "warn"
		if f.config.Redact {
			action = "redact"
		}

		return &FilterResult{
			Passed:     false,
			Action:     action,
			Message:    fmt.Sprintf("Detected %d PII violations", len(violations)),
			Redacted:   redacted,
			Violations: violations,
		}, nil
	}

	return &FilterResult{Passed: true, Action: "allow"}, nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
