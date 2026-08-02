package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/service/platform"
)

var watchedKinds = []string{
	"GateyesGateway",
	"ModelEndpoint",
	"RoutePolicy",
	"BudgetPolicy",
	"InferenceAutoscalePolicy",
	"InferenceService",
}

type operatorConfig struct {
	AdminURL        string
	Token           string
	Namespace       string
	Kubeconfig      string
	Kubernetes      bool
	DryRun          bool
	Once            bool
	SyncInterval    time.Duration
	Loader          snapshotLoader
	SyncClient      platform.AdminSyncClient
	WorkloadApplier workloadApplier
}

type snapshotLoader interface {
	Load(context.Context) (platform.ResourceSnapshot, error)
}

func main() {
	cfg := parseFlags(os.Args[1:])
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(context.Background(), cfg, os.Stdout); err != nil {
		slog.Error("operator stopped", "error", err)
		os.Exit(1)
	}
}

func parseFlags(args []string) operatorConfig {
	fs := flag.NewFlagSet("gateyes-operator", flag.ExitOnError)
	cfg := operatorConfig{}
	fs.StringVar(&cfg.AdminURL, "admin-url", "http://gateyes:8028", "Gateyes Admin API base URL")
	fs.StringVar(&cfg.Token, "token", "", "Gateyes admin bearer token or <key>:<secret>")
	fs.StringVar(&cfg.Namespace, "namespace", "", "namespace to watch; empty means all namespaces when Kubernetes watch is enabled")
	fs.StringVar(&cfg.Kubeconfig, "kubeconfig", "", "optional kubeconfig path; empty uses in-cluster config")
	fs.BoolVar(&cfg.Kubernetes, "kubernetes", true, "load Gateyes CRDs from Kubernetes")
	fs.BoolVar(&cfg.DryRun, "dry-run", true, "calculate sync plans without mutating Gateyes runtime state")
	fs.BoolVar(&cfg.Once, "once", false, "run one reconciliation tick and exit")
	fs.DurationVar(&cfg.SyncInterval, "sync-interval", 30*time.Second, "control-plane sync interval")
	_ = fs.Parse(args)
	return cfg
}

func run(ctx context.Context, cfg operatorConfig, out io.Writer) error {
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = 30 * time.Second
	}
	if strings.TrimSpace(cfg.Token) == "" {
		cfg.Token = tokenFromEnv()
	}
	if strings.TrimSpace(cfg.AdminURL) == "" {
		return fmt.Errorf("admin-url is required")
	}
	if cfg.Kubernetes && cfg.Loader == nil {
		loader, err := newKubernetesSnapshotLoader(cfg.Kubeconfig, cfg.Namespace)
		if err != nil {
			return err
		}
		cfg.Loader = loader
	}
	if !cfg.DryRun && cfg.SyncClient == nil {
		client, err := newAdminSyncClient(cfg.AdminURL, cfg.Token)
		if err != nil {
			return err
		}
		cfg.SyncClient = client
	}
	if !cfg.DryRun && cfg.Kubernetes && cfg.WorkloadApplier == nil {
		applier, err := newKubernetesWorkloadApplier(cfg.Kubeconfig)
		if err != nil {
			return err
		}
		cfg.WorkloadApplier = applier
	}

	if err := reconcileOnce(ctx, cfg, out); err != nil {
		return err
	}
	if cfg.Once {
		return nil
	}

	ticker := time.NewTicker(cfg.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := reconcileOnce(ctx, cfg, out); err != nil {
				return err
			}
		}
	}
}

func reconcileOnce(ctx context.Context, cfg operatorConfig, out io.Writer) error {
	mode := "apply"
	if cfg.DryRun {
		mode = "dry-run"
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "*"
	}
	if cfg.Loader == nil {
		_, err := fmt.Fprintf(out, "gateyes-operator tick mode=%s namespace=%s admin_url=%s watched=%s\n",
			mode,
			namespace,
			cfg.AdminURL,
			strings.Join(watchedKinds, ","),
		)
		return err
	}

	snapshot, err := cfg.Loader.Load(ctx)
	if err != nil {
		return err
	}
	plan, planErr := platform.BuildSyncPlan(snapshot, defaultNamespace(cfg.Namespace))
	_, writeErr := fmt.Fprintf(out, "gateyes-operator tick mode=%s namespace=%s admin_url=%s providers=%d route_policies=%d budgets=%d autoscale_policies=%d workload_deployments=%d workload_services=%d autoscale_decisions=%d\n",
		mode,
		namespace,
		cfg.AdminURL,
		len(plan.Providers),
		len(snapshot.RoutePolicies),
		len(plan.Budgets),
		len(plan.AutoscalePolicies),
		len(plan.Workloads.Deployments),
		len(plan.Workloads.Services),
		len(plan.Workloads.AutoscaleDecisions),
	)
	if writeErr != nil {
		return writeErr
	}
	if planErr != nil {
		return planErr
	}
	if cfg.DryRun {
		return nil
	}
	if cfg.SyncClient == nil {
		return fmt.Errorf("sync client is required when dry-run=false")
	}
	if len(plan.Workloads.Deployments) > 0 || len(plan.Workloads.Services) > 0 {
		if cfg.WorkloadApplier == nil {
			return fmt.Errorf("workload applier is required when dry-run=false and inference workloads are planned")
		}
		if err := cfg.WorkloadApplier.Apply(ctx, plan.Workloads); err != nil {
			return err
		}
	}
	return platform.ApplySyncPlan(plan, cfg.SyncClient)
}

func defaultNamespace(namespace string) string {
	if strings.TrimSpace(namespace) == "" {
		return "default"
	}
	return namespace
}

func tokenFromEnv() string {
	if token := strings.TrimSpace(os.Getenv("GATEYES_OPERATOR_TOKEN")); token != "" {
		return token
	}
	key := strings.TrimSpace(os.Getenv("GATEYES_ADMIN_BOOTSTRAP_KEY"))
	secret := strings.TrimSpace(os.Getenv("GATEYES_ADMIN_BOOTSTRAP_SECRET"))
	if key == "" || secret == "" {
		return ""
	}
	return key + ":" + secret
}
