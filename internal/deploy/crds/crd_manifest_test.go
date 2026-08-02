package crds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateyesCRDManifestSurface(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "helm", "gateyes", "crds", "gateyes.io_crds.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CRD manifest: %v", err)
	}
	manifest := string(data)
	docs := splitYAMLDocuments(manifest)
	if len(docs) != 6 {
		t.Fatalf("CRD document count = %d, want 6", len(docs))
	}

	want := map[string]string{
		"GateyesGateway":           "gateyesgateways.gateyes.io",
		"ModelEndpoint":            "modelendpoints.gateyes.io",
		"RoutePolicy":              "routepolicies.gateyes.io",
		"BudgetPolicy":             "budgetpolicies.gateyes.io",
		"InferenceAutoscalePolicy": "inferenceautoscalepolicies.gateyes.io",
		"InferenceService":         "inferenceservices.gateyes.io",
	}
	for kind, metadataName := range want {
		doc := findDocumentForKind(docs, kind)
		if doc == "" {
			t.Fatalf("missing CRD kind %s", kind)
		}
		assertContains(t, doc, "apiVersion: apiextensions.k8s.io/v1", kind)
		assertContains(t, doc, "kind: CustomResourceDefinition", kind)
		assertContains(t, doc, "  name: "+metadataName, kind)
		assertContains(t, doc, "  group: gateyes.io", kind)
		assertContains(t, doc, "  scope: Namespaced", kind)
		assertContains(t, doc, "    kind: "+kind, kind)
		assertContains(t, doc, "    - name: v1alpha1", kind)
		assertContains(t, doc, "      served: true", kind)
		assertContains(t, doc, "      storage: true", kind)
		assertContains(t, doc, "        status: {}", kind)
		assertContains(t, doc, "            status:", kind)
		assertContains(t, doc, "                conditions:", kind)
		for _, field := range []string{"type:", "status:", "reason:", "message:", "lastTransitionTime:"} {
			assertContains(t, doc, field, kind)
		}
	}
}

func splitYAMLDocuments(manifest string) []string {
	parts := strings.Split(manifest, "\n---\n")
	docs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			docs = append(docs, part)
		}
	}
	return docs
}

func findDocumentForKind(docs []string, kind string) string {
	for _, doc := range docs {
		if strings.Contains(doc, "    kind: "+kind+"\n") {
			return doc
		}
	}
	return ""
}

func assertContains(t *testing.T, doc string, needle string, kind string) {
	t.Helper()
	if !strings.Contains(doc, needle) {
		t.Fatalf("%s CRD missing %q", kind, needle)
	}
}
