// Command vllm launches one or more local vLLM inference processes for
// gateyes development / cache-hit experiments.
//
// It is intentionally lightweight: it shells out to the `vllm` CLI that is
// expected to be available in PATH, assigns a contiguous port range, enables
// prefix caching when asked, and keeps all child processes alive until SIGINT
// or SIGTERM. Each instance exposes both the OpenAI-compatible API and the
// Prometheus /metrics endpoint on the same port.
//
// Example:
//
//	go run ./cmd/vllm --model Qwen/Qwen3-0.6B --instances 2 --base-port 8001 --enable-prefix-caching
//
// The command prints ready-to-paste provider snippets for configs/config.yaml.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	var (
		model             = flag.String("model", "Qwen/Qwen3-0.6B", "HuggingFace model name or local path")
		instances         = flag.Int("instances", 1, "number of vLLM processes to start")
		basePort          = flag.Int("base-port", 8001, "API/metrics port for the first instance")
		enablePrefixCache = flag.Bool("enable-prefix-caching", true, "enable vLLM prefix caching")
		apiKey            = flag.String("api-key", "sk-vllm-local", "API key for the vLLM instances")
		maxModelLen       = flag.Int("max-model-len", 32768, "--max-model-len passed to vllm serve")
		gpuMemoryUtil     = flag.Float64("gpu-memory-utilization", 0.9, "--gpu-memory-utilization passed to vllm serve")
		waitTimeout       = flag.Duration("wait-timeout", 120*time.Second, "max time to wait for each instance to become ready")
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if _, err := exec.LookPath("vllm"); err != nil {
		slog.Error("vllm CLI not found in PATH", "error", err)
		os.Exit(1)
	}

	if *instances <= 0 {
		slog.Error("instances must be > 0")
		os.Exit(1)
	}
	if *basePort <= 0 || *basePort+*instances-1 > 65535 {
		slog.Error("invalid port range", "base", *basePort, "instances", *instances)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	started := make([]instance, 0, *instances)
	var startMu sync.Mutex

	for i := 0; i < *instances; i++ {
		port := *basePort + i
		inst := instance{
			name:    fmt.Sprintf("vllm-%s-%d", sanitizeName(*model), port),
			port:    port,
			baseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", port),
			metricsURL: fmt.Sprintf("http://127.0.0.1:%d/metrics", port),
		}

		cmd := buildCommand(ctx, *model, port, *apiKey, *maxModelLen, *gpuMemoryUtil, *enablePrefixCache)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		wg.Add(1)
		go func(inst instance) {
			defer wg.Done()
			slog.Info("starting vllm instance", "name", inst.name, "port", inst.port)
			if err := cmd.Start(); err != nil {
				slog.Error("failed to start vllm", "name", inst.name, "error", err)
				return
			}
			startMu.Lock()
			started = append(started, inst)
			startMu.Unlock()

			<-ctx.Done()

			slog.Info("stopping vllm instance", "name", inst.name)
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				slog.Warn("failed to signal vllm", "name", inst.name, "error", err)
			}
			_ = cmd.Wait()
		}(inst)
	}

	// Wait for all instances to report ready, then print config snippets.
	readyCtx, readyCancel := context.WithTimeout(ctx, *waitTimeout)
	defer readyCancel()
	if err := waitForReady(readyCtx, started); err != nil {
		slog.Error("not all vllm instances became ready", "error", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}

	printConfig(*model, started, *apiKey)

	slog.Info("all vllm instances are ready; press Ctrl-C to stop")
	<-ctx.Done()
	wg.Wait()
	slog.Info("all vllm instances stopped")
}

type instance struct {
	name       string
	port       int
	baseURL    string
	metricsURL string
}

func buildCommand(ctx context.Context, model string, port int, apiKey string, maxModelLen int, gpuMemoryUtil float64, prefixCache bool) *exec.Cmd {
	args := []string{
		"serve", model,
		"--port", fmt.Sprintf("%d", port),
		"--api-key", apiKey,
		"--max-model-len", fmt.Sprintf("%d", maxModelLen),
		"--gpu-memory-utilization", fmt.Sprintf("%.2f", gpuMemoryUtil),
	}
	if prefixCache {
		args = append(args, "--enable-prefix-caching")
	}
	return exec.CommandContext(ctx, "vllm", args...)
}

func waitForReady(ctx context.Context, instances []instance) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(instances))
	for _, inst := range instances {
		wg.Add(1)
		go func(inst instance) {
			defer wg.Done()
			client := &http.Client{Timeout: 2 * time.Second}
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("%s: %w", inst.name, ctx.Err())
					return
				case <-ticker.C:
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.metricsURL, nil)
					if err != nil {
						errCh <- err
						return
					}
					resp, err := client.Do(req)
					if err == nil {
						_ = resp.Body.Close()
						if resp.StatusCode == http.StatusOK {
							slog.Info("vllm instance ready", "name", inst.name, "port", inst.port)
							return
						}
					}
				}
			}
		}(inst)
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) == len(instances) {
		return errors.Join(errs...)
	}
	return nil
}

func printConfig(model string, instances []instance, apiKey string) {
	fmt.Println()
	fmt.Println("# Paste the following into configs/config.yaml (or a separate file included via env):")
	fmt.Println("providers:")
	for _, inst := range instances {
		fmt.Printf("  - name: %s\n", inst.name)
		fmt.Println("    type: openai")
		fmt.Println("    vendor: vllm")
		fmt.Println("    endpoint: chat")
		fmt.Printf("    baseURL: %s\n", inst.baseURL)
		fmt.Printf("    apiKey: %s\n", apiKey)
		fmt.Printf("    model: %s\n", model)
		fmt.Println("    weight: 100")
		fmt.Println("    enabled: true")
		fmt.Printf("    metricsURL: %s\n", inst.metricsURL)
		fmt.Println("    capabilities:")
		fmt.Println("      chat: true")
		fmt.Println("      stream: true")
	}
	fmt.Println()
}

func sanitizeName(model string) string {
	return strings.ReplaceAll(strings.ReplaceAll(model, "/", "-"), "_", "-")
}
