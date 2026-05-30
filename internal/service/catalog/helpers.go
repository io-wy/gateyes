package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/provider"
)

func snapshotFromService(record repository.ServiceRecord) repository.ServiceSnapshot {
	return repository.ServiceSnapshot{
		Name:            record.Name,
		RequestPrefix:   record.RequestPrefix,
		Description:     record.Description,
		DefaultProvider: record.DefaultProvider,
		DefaultModel:    record.DefaultModel,
		Enabled:         record.Enabled,
		Config:          record.Config,
	}
}

func cloneResponseRequest(req *provider.ResponseRequest) *provider.ResponseRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.Messages = req.InputMessages()
	cloned.Input = cloned.Messages
	cloned.OutputFormat = cloneOutputFormat(req.OutputFormat)
	cloned.Options = provider.CloneRequestOptions(req.Options)
	return &cloned
}

func cloneOutputFormat(value *provider.OutputFormat) *provider.OutputFormat {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Schema != nil {
		cloned.Schema = map[string]any{}
		for key, item := range value.Schema {
			cloned.Schema[key] = item
		}
	}
	if value.Raw != nil {
		cloned.Raw = map[string]any{}
		for key, item := range value.Raw {
			cloned.Raw[key] = item
		}
	}
	return &cloned
}

func renderTemplate(template string, variables []repository.PromptTemplateVariable, values map[string]any) (string, error) {
	if template == "" {
		return "", nil
	}
	result := template
	for _, variable := range variables {
		value, ok := values[variable.Name]
		if !ok || fmt.Sprint(value) == "" {
			if variable.Required && variable.Default == "" {
				return "", fmt.Errorf("%w: %s", ErrPromptVariableMissing, variable.Name)
			}
			value = variable.Default
		}
		placeholder := regexp.MustCompile(`\{\{\s*` + regexp.QuoteMeta(variable.Name) + `\s*\}\}`)
		result = placeholder.ReplaceAllString(result, fmt.Sprint(value))
	}
	return result, nil
}

func checkBlockedContent(rules *repository.GuardrailRuleSet, text string) error {
	if rules == nil || text == "" {
		return nil
	}
	for _, term := range rules.BlockTerms {
		if term != "" && strings.Contains(strings.ToLower(text), strings.ToLower(term)) {
			return fmt.Errorf("%w: blocked term matched", ErrPolicyViolation)
		}
	}
	for _, pattern := range rules.BlockRegex {
		if pattern == "" {
			continue
		}
		matched, err := regexp.MatchString(pattern, text)
		if err == nil && matched {
			return fmt.Errorf("%w: blocked regex matched", ErrPolicyViolation)
		}
	}
	return nil
}

func redactText(text string, terms []string) string {
	result := text
	for _, term := range terms {
		if term == "" {
			continue
		}
		result = strings.ReplaceAll(result, term, "[REDACTED]")
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func singleNonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{strings.TrimSpace(value)}
}

func mergeServicePolicies(base, overlay *repository.ServicePolicyConfig) *repository.ServicePolicyConfig {
	if base == nil && overlay == nil {
		return nil
	}
	merged := cloneServicePolicy(base)
	if merged == nil {
		merged = &repository.ServicePolicyConfig{}
	}
	if overlay != nil {
		merged.Request = mergeGuardrailRuleSets(merged.Request, overlay.Request)
		merged.Response = mergeGuardrailRuleSets(merged.Response, overlay.Response)
		merged.Enabled = merged.Enabled || overlay.Enabled
	}
	if merged.Request == nil && merged.Response == nil && !merged.Enabled {
		return nil
	}
	if policyHasRules(merged) {
		merged.Enabled = true
	}
	return merged
}

func cloneServicePolicy(policy *repository.ServicePolicyConfig) *repository.ServicePolicyConfig {
	if policy == nil {
		return nil
	}
	return &repository.ServicePolicyConfig{
		Enabled:  policy.Enabled,
		Request:  cloneGuardrailRuleSet(policy.Request),
		Response: cloneGuardrailRuleSet(policy.Response),
	}
}

func mergeGuardrailRuleSets(base, overlay *repository.GuardrailRuleSet) *repository.GuardrailRuleSet {
	if base == nil && overlay == nil {
		return nil
	}
	if base == nil {
		return cloneGuardrailRuleSet(overlay)
	}
	if overlay == nil {
		return cloneGuardrailRuleSet(base)
	}
	merged := cloneGuardrailRuleSet(base)
	merged.AllowModels = mergeAllowModels(base.AllowModels, overlay.AllowModels)
	merged.BlockModels = mergeUniqueStrings(base.BlockModels, overlay.BlockModels)
	merged.BlockTerms = mergeUniqueStrings(base.BlockTerms, overlay.BlockTerms)
	merged.BlockRegex = mergeUniqueStrings(base.BlockRegex, overlay.BlockRegex)
	merged.RedactTerms = mergeUniqueStrings(base.RedactTerms, overlay.RedactTerms)
	merged.MaxInputChars = minPositive(base.MaxInputChars, overlay.MaxInputChars)
	merged.MaxOutputChars = minPositive(base.MaxOutputChars, overlay.MaxOutputChars)
	if !guardrailRuleSetHasRules(merged) {
		return nil
	}
	return merged
}

func cloneGuardrailRuleSet(rules *repository.GuardrailRuleSet) *repository.GuardrailRuleSet {
	if rules == nil {
		return nil
	}
	return &repository.GuardrailRuleSet{
		AllowModels:    append([]string(nil), rules.AllowModels...),
		BlockModels:    append([]string(nil), rules.BlockModels...),
		BlockTerms:     append([]string(nil), rules.BlockTerms...),
		BlockRegex:     append([]string(nil), rules.BlockRegex...),
		RedactTerms:    append([]string(nil), rules.RedactTerms...),
		MaxInputChars:  rules.MaxInputChars,
		MaxOutputChars: rules.MaxOutputChars,
	}
}

func mergeAllowModels(base, overlay []string) []string {
	base = normalizeStringList(base)
	overlay = normalizeStringList(overlay)
	if len(base) == 0 {
		return overlay
	}
	if len(overlay) == 0 {
		return base
	}
	allowed := make(map[string]struct{}, len(overlay))
	for _, item := range overlay {
		allowed[item] = struct{}{}
	}
	result := make([]string, 0, len(base))
	for _, item := range base {
		if _, ok := allowed[item]; ok {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		return []string{"__gateyes_deny_all__"}
	}
	return result
}

func mergeUniqueStrings(base, overlay []string) []string {
	base = normalizeStringList(base)
	overlay = normalizeStringList(overlay)
	if len(base) == 0 {
		return overlay
	}
	if len(overlay) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(overlay))
	result := make([]string, 0, len(base)+len(overlay))
	for _, items := range [][]string{base, overlay} {
		for _, item := range items {
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func minPositive(a, b int) int {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func policyHasRules(policy *repository.ServicePolicyConfig) bool {
	if policy == nil {
		return false
	}
	return guardrailRuleSetHasRules(policy.Request) || guardrailRuleSetHasRules(policy.Response)
}

func guardrailRuleSetHasRules(rules *repository.GuardrailRuleSet) bool {
	if rules == nil {
		return false
	}
	return len(normalizeStringList(rules.AllowModels)) > 0 ||
		len(normalizeStringList(rules.BlockModels)) > 0 ||
		len(normalizeStringList(rules.BlockTerms)) > 0 ||
		len(normalizeStringList(rules.BlockRegex)) > 0 ||
		len(normalizeStringList(rules.RedactTerms)) > 0 ||
		rules.MaxInputChars > 0 ||
		rules.MaxOutputChars > 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func generatePlaceholderKey(seed string) string {
	return "bootstrap-" + strings.ReplaceAll(seed, "-", "")
}

func firstSoftAlertScope(scopes []budget.ScopeResult) string {
	for _, s := range scopes {
		if s.Policy == repository.BudgetPolicySoftAlert {
			return s.Scope
		}
	}
	return "unknown"
}
