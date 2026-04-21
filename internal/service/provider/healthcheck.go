package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
)

type HealthChecker struct {
	cfg        config.HealthCheckConfig
	store      repository.ProviderRegistryStore
	manager    *Manager
	alert      *alert.AlertService
	logger     *slog.Logger
	mu         sync.Mutex
	failures   map[string]int
	lastStatus map[string]string
}

func NewHealthChecker(cfg config.HealthCheckConfig, store repository.ProviderRegistryStore, manager *Manager, alertSvc *alert.AlertService) *HealthChecker {
	return &HealthChecker{
		cfg:        cfg,
		store:      store,
		manager:    manager,
		alert:      alertSvc,
		logger:     slog.With("component", "provider_health_checker"),
		failures:   make(map[string]int),
		lastStatus: make(map[string]string),
	}
}

func (h *HealthChecker) Start(ctx context.Context) {
	if h == nil || !h.cfg.Enabled || h.manager == nil || h.store == nil {
		return
	}
	interval := time.Duration(h.cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	go h.loop(ctx, interval)
}

func (h *HealthChecker) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	h.runOnce(ctx)

	for {
		select {
		case <-ticker.C:
			h.runOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (h *HealthChecker) runOnce(ctx context.Context) {
	for _, instance := range h.manager.List() {
		h.checkProvider(ctx, instance)
	}
}

func (h *HealthChecker) checkProvider(ctx context.Context, instance Provider) {
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.timeout())
	defer cancel()

	req := &ResponseRequest{
		Model:           instance.Model(),
		Surface:         "responses",
		Messages:        []Message{{Role: "user", Content: TextBlocks("health check")}},
		MaxOutputTokens: 8,
	}
	req.Normalize()
	_, err := instance.CreateResponse(probeCtx, req)

	record, ok := h.manager.Registry(instance.Name())
	if !ok {
		return
	}
	nextStatus := ProviderHealthHealthy
	if err != nil {
		nextStatus = h.failureStatus(instance.Name())
	} else {
		h.resetFailures(instance.Name())
	}
	if nextStatus == "" {
		nextStatus = ProviderHealthHealthy
	}
	if record.HealthStatus == nextStatus {
		h.manager.Stats.SetStatus(instance.Name(), nextStatus)
		return
	}
	updated, updateErr := h.store.UpdateProviderRegistry(probeCtx, instance.Name(), repository.UpdateProviderRegistryParams{
		HealthStatus: &nextStatus,
	})
	if updateErr != nil {
		h.logger.Warn("failed to persist provider health status", "provider", instance.Name(), "error", updateErr)
		return
	}
	h.manager.ApplyRegistry([]repository.ProviderRegistryRecord{*updated})
	h.manager.Stats.SetStatus(instance.Name(), nextStatus)
	h.emitProviderStateChange(instance.Name(), record.HealthStatus, nextStatus, err)
}

func (h *HealthChecker) timeout() time.Duration {
	timeout := time.Duration(h.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		return 15 * time.Second
	}
	return timeout
}

func (h *HealthChecker) failureStatus(name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[name]++
	threshold := h.cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 2
	}
	if h.failures[name] >= threshold {
		return ProviderHealthUnhealthy
	}
	return ProviderHealthDegraded
}

func (h *HealthChecker) resetFailures(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[name] = 0
}

func (h *HealthChecker) emitProviderStateChange(name, previous, current string, err error) {
	h.mu.Lock()
	last := h.lastStatus[name]
	if previous == "" && last != "" {
		previous = last
	}
	h.lastStatus[name] = current
	h.mu.Unlock()

	if previous == current {
		return
	}
	h.logger.Info("provider health state changed", "provider", name, "previous", previous, "current", current, "error", err)
	if h.alert != nil {
		h.alert.NotifyProviderStateChanged(context.Background(), alert.ProviderStateChange{
			ProviderName: name,
			Previous:     previous,
			Current:      current,
			Error:        errorString(err),
		})
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (h *HealthChecker) ForceCheck(ctx context.Context) error {
	if h == nil || h.manager == nil {
		return fmt.Errorf("health checker is not initialized")
	}
	h.runOnce(ctx)
	return nil
}
