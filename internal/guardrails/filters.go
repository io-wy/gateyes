package guardrails

import (
	"context"
	"fmt"
	"strings"
)

// ContentFilter filters harmful or inappropriate content
type ContentFilter struct {
	config ContentFilterConfig
}

// NewContentFilter creates a new content filter
func NewContentFilter(config ContentFilterConfig) *ContentFilter {
	return &ContentFilter{config: config}
}

// Name returns the filter name
func (f *ContentFilter) Name() string {
	return "content_filter"
}

// Check checks content for violations
func (f *ContentFilter) Check(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error) {
	violations := make([]Violation, 0)
	lowerContent := strings.ToLower(content)

	// Check custom blocklist
	for _, blocked := range f.config.CustomBlocklist {
		if strings.Contains(lowerContent, strings.ToLower(blocked)) {
			violations = append(violations, Violation{
				Type:     "blocked_content",
				Severity: "high",
				Message:  "Content contains blocked term",
				Value:    blocked,
			})
		}
	}

	// Check for prompt injection patterns
	if f.config.BlockPromptInjection {
		injectionPatterns := []string{
			"ignore previous instructions",
			"ignore all previous",
			"disregard previous",
			"forget previous",
			"new instructions:",
			"system:",
			"admin:",
			"override:",
		}

		for _, pattern := range injectionPatterns {
			if strings.Contains(lowerContent, pattern) {
				violations = append(violations, Violation{
					Type:     "prompt_injection",
					Severity: "critical",
					Message:  "Potential prompt injection detected",
					Value:    pattern,
				})
			}
		}
	}

	if len(violations) > 0 {
		return &FilterResult{
			Passed:     false,
			Action:     "block",
			Message:    fmt.Sprintf("Content filter detected %d violations", len(violations)),
			Violations: violations,
		}, nil
	}

	return &FilterResult{Passed: true, Action: "allow"}, nil
}

// AnomalyFilter detects anomalous behavior
type AnomalyFilter struct {
	config AnomalyDetectionConfig
}

// NewAnomalyFilter creates a new anomaly filter
func NewAnomalyFilter(config AnomalyDetectionConfig) *AnomalyFilter {
	return &AnomalyFilter{config: config}
}

// Name returns the filter name
func (f *AnomalyFilter) Name() string {
	return "anomaly_detection"
}

// Check checks for anomalies
func (f *AnomalyFilter) Check(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error) {
	violations := make([]Violation, 0)

	// Check for suspicious patterns
	for _, pattern := range f.config.SuspiciousPatterns {
		if strings.Contains(strings.ToLower(content), strings.ToLower(pattern)) {
			violations = append(violations, Violation{
				Type:     "suspicious_pattern",
				Severity: "medium",
				Message:  "Suspicious pattern detected",
				Value:    pattern,
			})
		}
	}

	// Check token count if provided in metadata
	if tokenCount, ok := metadata["token_count"].(int); ok {
		if f.config.MaxTokensPerRequest > 0 && tokenCount > f.config.MaxTokensPerRequest {
			violations = append(violations, Violation{
				Type:     "token_limit_exceeded",
				Severity: "medium",
				Message:  fmt.Sprintf("Token count %d exceeds limit %d", tokenCount, f.config.MaxTokensPerRequest),
			})
		}
	}

	if len(violations) > 0 {
		return &FilterResult{
			Passed:     false,
			Action:     "warn",
			Message:    fmt.Sprintf("Anomaly detection found %d issues", len(violations)),
			Violations: violations,
		}, nil
	}

	return &FilterResult{Passed: true, Action: "allow"}, nil
}

// CustomRuleFilter implements custom user-defined rules
type CustomRuleFilter struct {
	rule CustomRule
}

// NewCustomRuleFilter creates a new custom rule filter
func NewCustomRuleFilter(rule CustomRule) (*CustomRuleFilter, error) {
	// Validate rule
	if rule.Pattern == "" {
		return nil, fmt.Errorf("rule pattern is required")
	}

	return &CustomRuleFilter{rule: rule}, nil
}

// Name returns the filter name
func (f *CustomRuleFilter) Name() string {
	return fmt.Sprintf("custom_rule_%s", f.rule.Name)
}

// Check checks content against custom rule
func (f *CustomRuleFilter) Check(ctx context.Context, content string, metadata map[string]interface{}) (*FilterResult, error) {
	// Simple pattern matching (can be enhanced with regex)
	if strings.Contains(strings.ToLower(content), strings.ToLower(f.rule.Pattern)) {
		violation := Violation{
			Type:     "custom_rule_violation",
			Severity: "medium",
			Message:  f.rule.Description,
			Value:    f.rule.Pattern,
		}

		action := f.rule.Action
		if action == "" {
			action = "warn"
		}

		return &FilterResult{
			Passed:     false,
			Action:     action,
			Message:    f.rule.Message,
			Violations: []Violation{violation},
		}, nil
	}

	return &FilterResult{Passed: true, Action: "allow"}, nil
}
