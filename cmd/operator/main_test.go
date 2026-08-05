package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/platform"
)

func TestParseFlags(t *testing.T) {
	cfg := parseFlags([]string{
		"-admin-url", "http://gateyes-admin:8028",
		"-token", "admin:secret",
		"-namespace", "llm",
		"-kubeconfig", "/tmp/kubeconfig",
		"-kubernetes=false",
		"-dry-run=false",
		"-once",
		"-sync-interval", "5s",
	})
	if cfg.AdminURL != "http://gateyes-admin:8028" || cfg.Token != "admin:secret" || cfg.Namespace != "llm" || cfg.Kubeconfig != "/tmp/kubeconfig" {
		t.Fatalf("parseFlags() = %+v", cfg)
	}
	if cfg.Kubernetes || cfg.DryRun || !cfg.Once || cfg.SyncInterval != 5*time.Second {
		t.Fatalf("parseFlags bool/duration = %+v", cfg)
	}
}

func TestRunOncePrintsWatchedKinds(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), operatorConfig{
		AdminURL:     "http://gateyes:8028",
		Namespace:    "llm",
		DryRun:       true,
		Once:         true,
		SyncInterval: time.Second,
	}, &out)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	got := out.String()
	for _, want := range []string{"mode=dry-run", "namespace=llm", "GateyesGateway", "ModelEndpoint", "InferenceService"} {
		if !strings.Contains(got, want) {
			t.Fatalf("run output = %q, missing %q", got, want)
		}
	}
}

func TestRunAppliesLoadedSnapshot(t *testing.T) {
	var out bytes.Buffer
	client := &recordingAdminSyncClient{}
	applier := &recordingWorkloadApplier{}
	statusWriter := &recordingStatusWriter{}
	replicas := 2
	err := run(context.Background(), operatorConfig{
		AdminURL:     "http://gateyes:8028",
		Namespace:    "llm",
		DryRun:       false,
		Once:         true,
		SyncInterval: time.Second,
		Loader: staticSnapshotLoader{snapshot: platform.ResourceSnapshot{
			ModelEndpoints: []platform.ModelEndpoint{{
				Metadata: platform.ObjectMeta{Name: "qwen"},
				Spec: platform.ModelEndpointSpec{
					Type:    "vllm",
					BaseURL: "http://qwen.llm.svc:8000/v1",
					Model:   "Qwen/Qwen3",
				},
			}},
			InferenceServices: []platform.InferenceService{{
				Metadata: platform.ObjectMeta{Name: "served-qwen", Namespace: "llm"},
				Spec: platform.InferenceServiceSpec{
					Runtime:  "vllm",
					Model:    "Qwen/Qwen3",
					Replicas: &replicas,
				},
			}},
		}},
		SyncClient:      client,
		WorkloadApplier: applier,
		StatusWriter:    statusWriter,
	}, &out)
	if err != nil {
		t.Fatalf("run with loaded snapshot: %v", err)
	}
	if applier.deployments != 1 || applier.services != 1 {
		t.Fatalf("applied workloads = %+v, want one deployment and service", applier)
	}
	if client.providers != 2 {
		t.Fatalf("synced providers = %d, want explicit and exposed providers", client.providers)
	}
	if !strings.Contains(out.String(), "providers=2") || !strings.Contains(out.String(), "workload_deployments=1") {
		t.Fatalf("run output = %q, want provider and workload counts", out.String())
	}
	if statusWriter.updates != 1 {
		t.Fatalf("status updates = %d, want 1", statusWriter.updates)
	}
}

func TestRunRequiresAdminURL(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), operatorConfig{AdminURL: "", Once: true}, &out)
	if err == nil || !strings.Contains(err.Error(), "admin-url is required") {
		t.Fatalf("run error = %v, want admin-url required", err)
	}
}

func TestRunReconcilesOnEventSource(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	loader := &countingSnapshotLoader{cancelAfter: 2, cancel: cancel}
	events := make(chan struct{}, 1)
	events <- struct{}{}
	err := run(ctx, operatorConfig{
		AdminURL:     "http://gateyes:8028",
		DryRun:       true,
		SyncInterval: time.Hour,
		Loader:       loader,
		EventSource:  staticEventSource{events: events},
	}, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if loader.loads != 2 {
		t.Fatalf("loads = %d, want initial reconcile plus event reconcile", loader.loads)
	}
}

type countingSnapshotLoader struct {
	loads       int
	cancelAfter int
	cancel      context.CancelFunc
}

func (l *countingSnapshotLoader) Load(context.Context) (platform.ResourceSnapshot, error) {
	l.loads++
	if l.cancelAfter > 0 && l.loads >= l.cancelAfter {
		l.cancel()
	}
	return platform.ResourceSnapshot{}, nil
}

type staticEventSource struct {
	events <-chan struct{}
}

func (s staticEventSource) Start(context.Context) (<-chan struct{}, error) {
	return s.events, nil
}

func TestTokenFromEnvUsesExplicitOrBootstrapCredentials(t *testing.T) {
	t.Setenv("GATEYES_OPERATOR_TOKEN", "operator-token")
	t.Setenv("GATEYES_ADMIN_BOOTSTRAP_KEY", "admin-key")
	t.Setenv("GATEYES_ADMIN_BOOTSTRAP_SECRET", "admin-secret")
	if got := tokenFromEnv(); got != "operator-token" {
		t.Fatalf("tokenFromEnv() = %q, want explicit token", got)
	}

	t.Setenv("GATEYES_OPERATOR_TOKEN", "")
	if got := tokenFromEnv(); got != "admin-key:admin-secret" {
		t.Fatalf("tokenFromEnv() = %q, want bootstrap token", got)
	}
}

type staticSnapshotLoader struct {
	snapshot platform.ResourceSnapshot
	err      error
}

func (l staticSnapshotLoader) Load(context.Context) (platform.ResourceSnapshot, error) {
	return l.snapshot, l.err
}

type recordingAdminSyncClient struct {
	providers int
	routers   int
	budgets   int
}

func (c *recordingAdminSyncClient) SyncProvider(platform.ProviderSyncTarget) error {
	c.providers++
	return nil
}

func (c *recordingAdminSyncClient) SyncRouter(config.RouterConfig) error {
	c.routers++
	return nil
}

func (c *recordingAdminSyncClient) SyncBudget(platform.BudgetSyncTarget) error {
	c.budgets++
	return nil
}

type recordingWorkloadApplier struct {
	deployments int
	services    int
}

func (a *recordingWorkloadApplier) Apply(_ context.Context, plan platform.InferenceWorkloadPlan) error {
	a.deployments += len(plan.Deployments)
	a.services += len(plan.Services)
	return nil
}

type recordingStatusWriter struct {
	updates int
}

func (w *recordingStatusWriter) Update(context.Context, platform.ResourceSnapshot, platform.SyncPlan, error) error {
	w.updates++
	return nil
}
