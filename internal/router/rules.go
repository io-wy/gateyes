package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// CustomRule defines a user-defined routing rule
type CustomRule struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Priority    int             `json:"priority"` // Higher priority rules are evaluated first
	Conditions  []RuleCondition `json:"conditions"`
	Action      RuleAction      `json:"action"`
	Enabled     bool            `json:"enabled"`
}

// RuleCondition defines a condition that must be met
type RuleCondition struct {
	Type     string `json:"type"`     // "header", "path", "method", "query", "body", "time", "user"
	Field    string `json:"field"`    // Field name (e.g., header name, query param)
	Operator string `json:"operator"` // "equals", "contains", "regex", "gt", "lt", "in"
	Value    string `json:"value"`
}

// RuleAction defines what to do when conditions match
type RuleAction struct {
	Type     string                 `json:"type"` // "route", "reject", "modify"
	Provider string                 `json:"provider,omitempty"`
	Status   int                    `json:"status,omitempty"`
	Message  string                 `json:"message,omitempty"`
	Modify   map[string]interface{} `json:"modify,omitempty"`
}

// RuleEngine evaluates custom rules
type RuleEngine struct {
	rules []*CustomRule
}

// NewRuleEngine creates a new rule engine
func NewRuleEngine(rules []*CustomRule) *RuleEngine {
	// Sort rules by priority (higher first)
	sortedRules := make([]*CustomRule, len(rules))
	copy(sortedRules, rules)

	// Simple bubble sort by priority
	for i := 0; i < len(sortedRules); i++ {
		for j := i + 1; j < len(sortedRules); j++ {
			if sortedRules[j].Priority > sortedRules[i].Priority {
				sortedRules[i], sortedRules[j] = sortedRules[j], sortedRules[i]
			}
		}
	}

	return &RuleEngine{
		rules: sortedRules,
	}
}

// RuleContext contains request context for rule evaluation
type RuleContext struct {
	Method  string
	Path    string
	Headers map[string]string
	Query   map[string]string
	Body    []byte
	User    string
}

// Evaluate evaluates all rules and returns the first matching action
func (re *RuleEngine) Evaluate(ctx context.Context, ruleCtx *RuleContext) (*RuleAction, error) {
	for _, rule := range re.rules {
		if !rule.Enabled {
			continue
		}

		if re.matchesRule(rule, ruleCtx) {
			slog.Info("custom rule matched",
				"rule", rule.Name,
				"action", rule.Action.Type,
			)
			return &rule.Action, nil
		}
	}

	return nil, nil // No matching rule
}

// matchesRule checks if all conditions of a rule are met
func (re *RuleEngine) matchesRule(rule *CustomRule, ctx *RuleContext) bool {
	if len(rule.Conditions) == 0 {
		return false
	}

	for _, condition := range rule.Conditions {
		if !re.matchesCondition(&condition, ctx) {
			return false
		}
	}

	return true
}

// matchesCondition checks if a single condition is met
func (re *RuleEngine) matchesCondition(condition *RuleCondition, ctx *RuleContext) bool {
	var actualValue string

	// Get the actual value based on condition type
	switch condition.Type {
	case "header":
		actualValue = ctx.Headers[condition.Field]
	case "path":
		actualValue = ctx.Path
	case "method":
		actualValue = ctx.Method
	case "query":
		actualValue = ctx.Query[condition.Field]
	case "body":
		actualValue = string(ctx.Body)
	case "user":
		actualValue = ctx.User
	default:
		slog.Warn("unknown condition type", "type", condition.Type)
		return false
	}

	// Evaluate the condition based on operator
	return re.evaluateOperator(condition.Operator, actualValue, condition.Value)
}

// evaluateOperator evaluates a condition operator
func (re *RuleEngine) evaluateOperator(operator, actual, expected string) bool {
	switch operator {
	case "equals":
		return actual == expected
	case "not_equals":
		return actual != expected
	case "contains":
		return strings.Contains(actual, expected)
	case "not_contains":
		return !strings.Contains(actual, expected)
	case "starts_with":
		return strings.HasPrefix(actual, expected)
	case "ends_with":
		return strings.HasSuffix(actual, expected)
	case "regex":
		matched, err := regexp.MatchString(expected, actual)
		if err != nil {
			slog.Error("regex match failed", "error", err)
			return false
		}
		return matched
	case "in":
		// Expected is a comma-separated list
		values := strings.Split(expected, ",")
		for _, v := range values {
			if strings.TrimSpace(v) == actual {
				return true
			}
		}
		return false
	case "not_in":
		values := strings.Split(expected, ",")
		for _, v := range values {
			if strings.TrimSpace(v) == actual {
				return false
			}
		}
		return true
	case "exists":
		return actual != ""
	case "not_exists":
		return actual == ""
	default:
		slog.Warn("unknown operator", "operator", operator)
		return false
	}
}

// LoadRulesFromJSON loads rules from JSON
func LoadRulesFromJSON(data []byte) ([]*CustomRule, error) {
	var rules []*CustomRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("failed to parse rules: %w", err)
	}
	return rules, nil
}

// ValidateRule validates a custom rule
func ValidateRule(rule *CustomRule) error {
	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}

	if len(rule.Conditions) == 0 {
		return fmt.Errorf("rule must have at least one condition")
	}

	for i, condition := range rule.Conditions {
		if err := validateCondition(&condition); err != nil {
			return fmt.Errorf("condition %d: %w", i, err)
		}
	}

	if err := validateAction(&rule.Action); err != nil {
		return fmt.Errorf("action: %w", err)
	}

	return nil
}

// validateCondition validates a rule condition
func validateCondition(condition *RuleCondition) error {
	validTypes := []string{"header", "path", "method", "query", "body", "user", "time"}
	if !contains(validTypes, condition.Type) {
		return fmt.Errorf("invalid condition type: %s", condition.Type)
	}

	validOperators := []string{
		"equals", "not_equals", "contains", "not_contains",
		"starts_with", "ends_with", "regex", "in", "not_in",
		"exists", "not_exists",
	}
	if !contains(validOperators, condition.Operator) {
		return fmt.Errorf("invalid operator: %s", condition.Operator)
	}

	return nil
}

// validateAction validates a rule action
func validateAction(action *RuleAction) error {
	validTypes := []string{"route", "reject", "modify"}
	if !contains(validTypes, action.Type) {
		return fmt.Errorf("invalid action type: %s", action.Type)
	}

	if action.Type == "route" && action.Provider == "" {
		return fmt.Errorf("route action requires provider")
	}

	if action.Type == "reject" && action.Status == 0 {
		action.Status = 403 // Default to forbidden
	}

	return nil
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

// Example rules for documentation
var ExampleRules = `[
  {
    "name": "Route GPT-4 to Azure",
    "description": "Route all GPT-4 requests to Azure OpenAI",
    "priority": 100,
    "enabled": true,
    "conditions": [
      {
        "type": "body",
        "field": "",
        "operator": "contains",
        "value": "gpt-4"
      }
    ],
    "action": {
      "type": "route",
      "provider": "azure-openai"
    }
  },
  {
    "name": "Block expensive models for free users",
    "description": "Reject requests to expensive models from free tier users",
    "priority": 200,
    "enabled": true,
    "conditions": [
      {
        "type": "header",
        "field": "X-User-Tier",
        "operator": "equals",
        "value": "free"
      },
      {
        "type": "body",
        "field": "",
        "operator": "regex",
        "value": "(gpt-4|claude-opus)"
      }
    ],
    "action": {
      "type": "reject",
      "status": 403,
      "message": "Upgrade to access premium models"
    }
  },
  {
    "name": "Route by region",
    "description": "Route EU users to EU providers",
    "priority": 50,
    "enabled": true,
    "conditions": [
      {
        "type": "header",
        "field": "X-User-Region",
        "operator": "in",
        "value": "EU,UK,DE,FR"
      }
    ],
    "action": {
      "type": "route",
      "provider": "openai-eu"
    }
  }
]`
