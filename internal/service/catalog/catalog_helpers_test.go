package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestRenderTemplate(t *testing.T) {
	cases := []struct {
		name      string
		template  string
		vars      []repository.PromptTemplateVariable
		values    map[string]any
		want      string
		wantError error
	}{
		{
			name:     "simple substitution",
			template: "Hello {{name}}",
			vars:     []repository.PromptTemplateVariable{{Name: "name", Required: true}},
			values:   map[string]any{"name": "Gateyes"},
			want:     "Hello Gateyes",
		},
		{
			name:     "empty template",
			template: "",
			vars:     nil,
			values:   nil,
			want:     "",
		},
		{
			name:     "default value",
			template: "Hello {{name}}",
			vars:     []repository.PromptTemplateVariable{{Name: "name", Required: true, Default: "World"}},
			values:   map[string]any{},
			want:     "Hello World",
		},
		{
			name:      "missing required variable",
			template:  "Hello {{name}}",
			vars:      []repository.PromptTemplateVariable{{Name: "name", Required: true}},
			values:    map[string]any{},
			wantError: ErrPromptVariableMissing,
		},
		{
			name:     "whitespace in placeholder",
			template: "Hello {{  name  }}",
			vars:     []repository.PromptTemplateVariable{{Name: "name", Required: true}},
			values:   map[string]any{"name": "Gateyes"},
			want:     "Hello Gateyes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderTemplate(tc.template, tc.vars, tc.values)
			if tc.wantError != nil {
				if !errors.Is(err, tc.wantError) {
					t.Fatalf("error = %v, want %v", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCloneResponseRequest(t *testing.T) {
	if cloneResponseRequest(nil) != nil {
		t.Fatal("cloneResponseRequest(nil) should return nil")
	}
	original := &provider.ResponseRequest{
		Model: "gpt-4",
		Messages: []provider.Message{{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: "hello"}},
		}},
		Options: &provider.RequestOptions{System: "sys"},
	}
	cloned := cloneResponseRequest(original)
	if cloned.Model != "gpt-4" {
		t.Fatalf("cloned.Model = %q", cloned.Model)
	}
	if cloned.Options == nil || cloned.Options.System != "sys" {
		t.Fatal("Options not cloned")
	}
}

func TestCloneOutputFormat(t *testing.T) {
	if cloneOutputFormat(nil) != nil {
		t.Fatal("cloneOutputFormat(nil) should return nil")
	}
	original := &provider.OutputFormat{
		Type: "json_schema",
		Schema: map[string]any{"key": "value"},
		Raw:    map[string]any{"raw": true},
	}
	cloned := cloneOutputFormat(original)
	if cloned.Type != "json_schema" {
		t.Fatalf("cloned.Type = %q", cloned.Type)
	}
	if cloned.Schema["key"] != "value" {
		t.Fatal("Schema not cloned")
	}
	// Mutate original to verify deep copy
	original.Schema["key"] = "mutated"
	if cloned.Schema["key"] != "value" {
		t.Fatal("Schema should be deep copied")
	}
}

func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b"}, "a") {
		t.Fatal("expected true")
	}
	if containsString([]string{"a", "b"}, "c") {
		t.Fatal("expected false")
	}
	if containsString(nil, "a") {
		t.Fatal("expected false for nil")
	}
}

func TestSingleNonEmpty(t *testing.T) {
	if singleNonEmpty("") != nil {
		t.Fatal("singleNonEmpty(empty) should return nil")
	}
	if singleNonEmpty("  ") != nil {
		t.Fatal("singleNonEmpty(whitespace) should return nil")
	}
	got := singleNonEmpty("hello")
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("singleNonEmpty = %v", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "b", "") != "b" {
		t.Fatalf("firstNonEmpty = %q", firstNonEmpty("", "b", ""))
	}
	if firstNonEmpty("", "", "") != "" {
		t.Fatalf("firstNonEmpty(all empty) = %q", firstNonEmpty("", "", ""))
	}
}

func TestNormalizeStringList(t *testing.T) {
	if normalizeStringList(nil) != nil {
		t.Fatal("normalizeStringList(nil) should return nil")
	}
	if normalizeStringList([]string{"", "  "}) != nil {
		t.Fatal("normalizeStringList(all empty) should return nil")
	}
	got := normalizeStringList([]string{"a", "a", " b "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("normalizeStringList = %v", got)
	}
}

func TestMinPositive(t *testing.T) {
	if minPositive(0, 5) != 5 {
		t.Fatal("minPositive(0,5) should be 5")
	}
	if minPositive(5, 0) != 5 {
		t.Fatal("minPositive(5,0) should be 5")
	}
	if minPositive(3, 7) != 3 {
		t.Fatal("minPositive(3,7) should be 3")
	}
	if minPositive(7, 3) != 3 {
		t.Fatal("minPositive(7,3) should be 3")
	}
}

func TestMergeAllowModels(t *testing.T) {
	// intersection
	got := mergeAllowModels([]string{"a", "b"}, []string{"b", "c"})
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("mergeAllowModels = %v", got)
	}
	// empty base
	got = mergeAllowModels([]string{}, []string{"a"})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("mergeAllowModels(empty base) = %v", got)
	}
	// no intersection -> deny all
	got = mergeAllowModels([]string{"a"}, []string{"b"})
	if len(got) != 1 || got[0] != "__gateyes_deny_all__" {
		t.Fatalf("mergeAllowModels(no intersection) = %v", got)
	}
}

func TestMergeUniqueStrings(t *testing.T) {
	got := mergeUniqueStrings([]string{"a", "b"}, []string{"b", "c"})
	if len(got) != 3 {
		t.Fatalf("mergeUniqueStrings = %v", got)
	}
}

func TestPolicyHasRules(t *testing.T) {
	if policyHasRules(nil) {
		t.Fatal("policyHasRules(nil) should be false")
	}
	if policyHasRules(&repository.ServicePolicyConfig{}) {
		t.Fatal("policyHasRules(empty) should be false")
	}
	if !policyHasRules(&repository.ServicePolicyConfig{Request: &repository.GuardrailRuleSet{BlockTerms: []string{"bad"}}}) {
		t.Fatal("policyHasRules(with rules) should be true")
	}
}

func TestGuardrailRuleSetHasRules(t *testing.T) {
	if guardrailRuleSetHasRules(nil) {
		t.Fatal("guardrailRuleSetHasRules(nil) should be false")
	}
	if guardrailRuleSetHasRules(&repository.GuardrailRuleSet{}) {
		t.Fatal("guardrailRuleSetHasRules(empty) should be false")
	}
	if !guardrailRuleSetHasRules(&repository.GuardrailRuleSet{MaxInputChars: 10}) {
		t.Fatal("guardrailRuleSetHasRules(maxInputChars) should be true")
	}
}

func TestLoadPublishedServiceErrors(t *testing.T) {
	store := &mockStore{}
	svc := newMockCatalogService(store)

	// Service disabled
	store.service = &repository.ServiceRecord{
		ID: "svc-1", TenantID: "tenant-1", RequestPrefix: "test",
		Enabled: false,
	}
	_, err := svc.loadPublishedService(context.Background(), "tenant-1", "test")
	if !errors.Is(err, ErrServiceDisabled) {
		t.Fatalf("expected ErrServiceDisabled, got %v", err)
	}

	// Service not published
	store.service = &repository.ServiceRecord{
		ID: "svc-1", TenantID: "tenant-1", RequestPrefix: "test",
		Enabled: true, PublishedVersionID: "",
	}
	_, err = svc.loadPublishedService(context.Background(), "tenant-1", "test")
	if !errors.Is(err, ErrServiceNotPublished) {
		t.Fatalf("expected ErrServiceNotPublished, got %v", err)
	}
}

func TestPrepareRuntimeRequestModelOverrideDenied(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID: "svc-1", TenantID: "tenant-1", RequestPrefix: "prefix",
			DefaultProvider: "p1", DefaultModel: "model-a",
			Enabled: true, PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{Surfaces: []string{"responses"}},
		},
		version: &repository.ServiceVersionRecord{
			ID: "ver-1", ServiceID: "svc-1",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix: "prefix", DefaultProvider: "p1", DefaultModel: "model-a",
				Config: repository.ServiceConfig{Surfaces: []string{"responses"}},
			},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{
		TenantID: "tenant-1", APIKeyID: "key-1",
		APIKeyServices: []string{"prefix"}, APIKeyModels: []string{"model-a"}, Quota: -1,
	}
	// Client tries to override model
	_, _, err := svc.prepareRuntimeRequest(context.Background(), identity, "prefix", "responses", &provider.ResponseRequest{
		Model: "model-b",
	})
	if !errors.Is(err, ErrServiceAccessDenied) {
		t.Fatalf("expected ErrServiceAccessDenied for model override, got %v", err)
	}
}

func TestPrepareRuntimeRequestMissingDefaultModel(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID: "svc-1", TenantID: "tenant-1", RequestPrefix: "prefix",
			DefaultProvider: "p1", DefaultModel: "",
			Enabled: true, PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{Surfaces: []string{"responses"}},
		},
		version: &repository.ServiceVersionRecord{
			ID: "ver-1", ServiceID: "svc-1",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix: "prefix", DefaultProvider: "p1", DefaultModel: "",
				Config: repository.ServiceConfig{Surfaces: []string{"responses"}},
			},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{
		TenantID: "tenant-1", APIKeyID: "key-1",
		APIKeyServices: []string{"prefix"}, APIKeyModels: []string{}, Quota: -1,
	}
	_, _, err := svc.prepareRuntimeRequest(context.Background(), identity, "prefix", "responses", &provider.ResponseRequest{
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hi")}},
	})
	if !errors.Is(err, ErrServiceNotPublished) {
		t.Fatalf("expected ErrServiceNotPublished for missing default model, got %v", err)
	}
}

func TestPrepareRuntimeRequestSurfaceDenied(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID: "svc-1", TenantID: "tenant-1", RequestPrefix: "prefix",
			DefaultProvider: "p1", DefaultModel: "model-a",
			Enabled: true, PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{Surfaces: []string{"responses"}},
		},
		version: &repository.ServiceVersionRecord{
			ID: "ver-1", ServiceID: "svc-1",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix: "prefix", DefaultProvider: "p1", DefaultModel: "model-a",
				Config: repository.ServiceConfig{Surfaces: []string{"responses"}},
			},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{
		TenantID: "tenant-1", APIKeyID: "key-1",
		APIKeyServices: []string{"prefix"}, APIKeyModels: []string{"model-a"}, Quota: -1,
	}
	_, _, err := svc.prepareRuntimeRequest(context.Background(), identity, "prefix", "invoke", &provider.ResponseRequest{
		Model: "model-a",
	})
	if !errors.Is(err, ErrServiceSurfaceDenied) {
		t.Fatalf("expected ErrServiceSurfaceDenied, got %v", err)
	}
}

func TestPromoteStagedServiceVersion(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{ID: "svc-1"},
		version: &repository.ServiceVersionRecord{ID: "ver-2"},
	}
	svc := newMockCatalogService(store)
	s, v, err := svc.PromoteStagedServiceVersion(context.Background(), "tenant-1", "svc-1")
	if err != nil {
		t.Fatalf("PromoteStagedServiceVersion() error: %v", err)
	}
	if s == nil || v == nil {
		t.Fatal("expected service and version")
	}
}

func TestRollbackServiceVersion(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{ID: "svc-1"},
		version: &repository.ServiceVersionRecord{ID: "ver-1"},
	}
	svc := newMockCatalogService(store)
	s, v, err := svc.RollbackServiceVersion(context.Background(), "tenant-1", "svc-1", "ver-1")
	if err != nil {
		t.Fatalf("RollbackServiceVersion() error: %v", err)
	}
	if s == nil || v == nil {
		t.Fatal("expected service and version")
	}
}

func TestCheckSubscriptionSurface(t *testing.T) {
	store := &mockStore{
		subscriptions: []repository.ServiceSubscriptionRecord{
			{ApprovedAPIKeyID: "key-1", AllowedSurfaces: []string{"responses"}},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{TenantID: "tenant-1", APIKeyID: "key-1"}

	// Allowed surface
	if err := svc.checkSubscriptionSurface(context.Background(), identity, "svc-1", "responses"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// Denied surface
	if err := svc.checkSubscriptionSurface(context.Background(), identity, "svc-1", "invoke"); !errors.Is(err, ErrServiceSurfaceDenied) {
		t.Fatalf("expected ErrServiceSurfaceDenied, got %v", err)
	}

	// No identity => no check needed
	if err := svc.checkSubscriptionSurface(context.Background(), nil, "svc-1", "invoke"); err != nil {
		t.Fatalf("expected nil for nil identity, got %v", err)
	}

	// No matching subscription => no restriction
	store.subscriptions = nil
	if err := svc.checkSubscriptionSurface(context.Background(), identity, "svc-1", "invoke"); err != nil {
		t.Fatalf("expected nil for no subscriptions, got %v", err)
	}
}

func TestApplyRequestPolicyNilCases(t *testing.T) {
	svc := New(&Dependencies{})
	// nil policy
	if err := svc.applyRequestPolicy(nil, &provider.ResponseRequest{}); err != nil {
		t.Fatalf("nil policy should pass: %v", err)
	}
	// disabled policy
	if err := svc.applyRequestPolicy(&repository.ServicePolicyConfig{Enabled: false}, &provider.ResponseRequest{}); err != nil {
		t.Fatalf("disabled policy should pass: %v", err)
	}
	// nil rules
	if err := svc.applyRequestPolicy(&repository.ServicePolicyConfig{Enabled: true, Request: nil}, &provider.ResponseRequest{}); err != nil {
		t.Fatalf("nil rules should pass: %v", err)
	}
	// nil request
	if err := svc.applyRequestPolicy(&repository.ServicePolicyConfig{Enabled: true, Request: &repository.GuardrailRuleSet{}}, nil); err != nil {
		t.Fatalf("nil request should pass: %v", err)
	}
}

func TestApplyResponsePolicyNilCases(t *testing.T) {
	svc := New(&Dependencies{Store: &mockStore{}})
	identity := &repository.AuthIdentity{TenantID: "tenant-1"}
	runtime := &serviceRuntime{
		service: &repository.ServiceRecord{ID: "svc-1"},
		snapshot: repository.ServiceSnapshot{
			Config: repository.ServiceConfig{
				Policy: &repository.ServicePolicyConfig{Enabled: true, Response: &repository.GuardrailRuleSet{}},
			},
		},
	}
	// nil response
	if err := svc.applyResponsePolicy(context.Background(), identity, runtime, nil); err != nil {
		t.Fatalf("nil response should pass: %v", err)
	}
	// nil runtime
	if err := svc.applyResponsePolicy(context.Background(), identity, nil, &provider.Response{}); err != nil {
		t.Fatalf("nil runtime should pass: %v", err)
	}
	// nil policy
	runtime.snapshot.Config.Policy = nil
	if err := svc.applyResponsePolicy(context.Background(), identity, runtime, &provider.Response{}); err != nil {
		t.Fatalf("nil policy should pass: %v", err)
	}
}

func TestCheckBlockedContent(t *testing.T) {
	// nil rules
	if err := checkBlockedContent(nil, "text"); err != nil {
		t.Fatalf("nil rules should pass: %v", err)
	}
	// empty text
	if err := checkBlockedContent(&repository.GuardrailRuleSet{BlockTerms: []string{"bad"}}, ""); err != nil {
		t.Fatalf("empty text should pass: %v", err)
	}
	// blocked term (case insensitive)
	if err := checkBlockedContent(&repository.GuardrailRuleSet{BlockTerms: []string{"BAD"}}, "bad word"); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation, got %v", err)
	}
	// blocked regex
	if err := checkBlockedContent(&repository.GuardrailRuleSet{BlockRegex: []string{`\d{3}-\d{2}-\d{4}`}}, "ssn 123-45-6789"); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for regex, got %v", err)
	}
	// invalid regex skipped
	if err := checkBlockedContent(&repository.GuardrailRuleSet{BlockRegex: []string{`[invalid`}}, "text"); err != nil {
		t.Fatalf("invalid regex should be skipped: %v", err)
	}
	// empty pattern skipped
	if err := checkBlockedContent(&repository.GuardrailRuleSet{BlockRegex: []string{""}}, "text"); err != nil {
		t.Fatalf("empty pattern should be skipped: %v", err)
	}
}

func TestRedactText(t *testing.T) {
	if redactText("hello secret world", []string{"secret"}) != "hello [REDACTED] world" {
		t.Fatalf("redactText = %q", redactText("hello secret world", []string{"secret"}))
	}
	if redactText("hello", []string{""}) != "hello" {
		t.Fatal("empty term should not change text")
	}
}

func TestGeneratePlaceholderKey(t *testing.T) {
	if generatePlaceholderKey("abc-def") != "bootstrap-abcdef" {
		t.Fatalf("generatePlaceholderKey = %q", generatePlaceholderKey("abc-def"))
	}
}

func TestCloneServicePolicy(t *testing.T) {
	if cloneServicePolicy(nil) != nil {
		t.Fatal("cloneServicePolicy(nil) should return nil")
	}
	original := &repository.ServicePolicyConfig{
		Enabled: true,
		Request: &repository.GuardrailRuleSet{BlockTerms: []string{"bad"}},
	}
	cloned := cloneServicePolicy(original)
	if !cloned.Enabled {
		t.Fatal("cloned.Enabled should be true")
	}
	original.Request.BlockTerms[0] = "mutated"
	if cloned.Request.BlockTerms[0] != "bad" {
		t.Fatal("cloned should be deep copy")
	}
}

func TestCloneGuardrailRuleSet(t *testing.T) {
	if cloneGuardrailRuleSet(nil) != nil {
		t.Fatal("cloneGuardrailRuleSet(nil) should return nil")
	}
	original := &repository.GuardrailRuleSet{BlockTerms: []string{"bad"}}
	cloned := cloneGuardrailRuleSet(original)
	original.BlockTerms[0] = "mutated"
	if cloned.BlockTerms[0] != "bad" {
		t.Fatal("cloned should be deep copy")
	}
}

func TestMergeServicePoliciesNilCases(t *testing.T) {
	if mergeServicePolicies(nil, nil) != nil {
		t.Fatal("mergeServicePolicies(nil,nil) should return nil")
	}
	a := &repository.ServicePolicyConfig{Enabled: true, Request: &repository.GuardrailRuleSet{BlockTerms: []string{"a"}}}
	merged := mergeServicePolicies(nil, a)
	if merged == nil || !merged.Enabled {
		t.Fatal("mergeServicePolicies(nil, a) should return a")
	}
}

func TestMergeGuardrailRuleSetsNilCases(t *testing.T) {
	if mergeGuardrailRuleSets(nil, nil) != nil {
		t.Fatal("mergeGuardrailRuleSets(nil,nil) should return nil")
	}
	a := &repository.GuardrailRuleSet{BlockTerms: []string{"a"}}
	merged := mergeGuardrailRuleSets(nil, a)
	if merged == nil || len(merged.BlockTerms) != 1 {
		t.Fatal("mergeGuardrailRuleSets(nil, a) should return a")
	}
	merged = mergeGuardrailRuleSets(a, nil)
	if merged == nil || len(merged.BlockTerms) != 1 {
		t.Fatal("mergeGuardrailRuleSets(a, nil) should return a")
	}
}

func TestResolveEffectivePolicyNilService(t *testing.T) {
	store := &mockStore{}
	svc := newMockCatalogService(store)
	cfg := &repository.ServicePolicyConfig{Enabled: true}
	got, err := svc.resolveEffectivePolicy(context.Background(), nil, cfg)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got == nil || !got.Enabled {
		t.Fatal("expected cloned policy")
	}
}
