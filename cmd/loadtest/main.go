package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/middleware"
	"gateyes/internal/router"

	"github.com/gin-gonic/gin"
)

const (
	virtualKeyToken = "vk-loadtest"
	requestPath     = "/v1/chat/completions"
)

var (
	defaultPayload = []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)
	responseBody   = []byte(`{"id":"chatcmpl-load","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`)
)

type options struct {
	concurrencyList []int
	duration        time.Duration
	warmup          time.Duration
	enableAuth      bool
	enableMetrics   bool
	enableRetry     bool
	maxRetries      int
	requestTimeout  time.Duration
	upstreamLatency time.Duration
}

type loadResult struct {
	concurrency int
	elapsed     time.Duration
	total       int64
	success     int64
	failure     int64
	rps         float64
	p50         time.Duration
	p95         time.Duration
	p99         time.Duration
	max         time.Duration
	statuses    map[int]int64
	errors      map[string]int64
}

type workerResult struct {
	total     int64
	success   int64
	failure   int64
	latencies []time.Duration
	statuses  map[int]int64
	errors    map[string]int64
}

func main() {
	opts, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid flags: %v\n", err)
		os.Exit(1)
	}

	gin.SetMode(gin.ReleaseMode)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	upstream := startMockUpstream(opts.upstreamLatency)
	defer upstream.Close()

	baseURL, shutdownGateway, err := startGateway(upstream.URL, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start gateway: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownGateway(ctx)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	if err := waitForHealth(client, baseURL+"/healthz", 5*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "gateway not ready: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Target: %s%s\n", baseURL, requestPath)
	fmt.Printf(
		"Auth=%t Metrics=%t Retry=%t MaxRetries=%d Duration=%s Warmup=%s UpstreamLatency=%s\n",
		opts.enableAuth,
		opts.enableMetrics,
		opts.enableRetry,
		opts.maxRetries,
		opts.duration,
		opts.warmup,
		opts.upstreamLatency,
	)

	results := make([]loadResult, 0, len(opts.concurrencyList))
	for _, concurrency := range opts.concurrencyList {
		fmt.Printf("Running concurrency=%d ...\n", concurrency)

		transport := &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        concurrency * 4,
			MaxIdleConnsPerHost: concurrency * 4,
			MaxConnsPerHost:     concurrency * 4,
			IdleConnTimeout:     90 * time.Second,
		}
		scenarioClient := &http.Client{
			Transport: transport,
			Timeout:   opts.requestTimeout,
		}

		if opts.warmup > 0 {
			_, err := runLoad(
				scenarioClient,
				baseURL+requestPath,
				concurrency,
				opts.warmup,
				opts.enableAuth,
				false,
			)
			if err != nil {
				transport.CloseIdleConnections()
				fmt.Fprintf(os.Stderr, "warmup failed for concurrency=%d: %v\n", concurrency, err)
				os.Exit(1)
			}
		}

		result, err := runLoad(
			scenarioClient,
			baseURL+requestPath,
			concurrency,
			opts.duration,
			opts.enableAuth,
			true,
		)
		transport.CloseIdleConnections()
		if err != nil {
			fmt.Fprintf(os.Stderr, "load run failed for concurrency=%d: %v\n", concurrency, err)
			os.Exit(1)
		}
		result.concurrency = concurrency
		results = append(results, result)
	}

	printSummary(results)
}

func parseFlags() (options, error) {
	var (
		concurrencyRaw string
		duration       time.Duration
		warmup         time.Duration
		enableAuth     bool
		enableMetrics  bool
		enableRetry    bool
		maxRetries     int
		requestTimeout time.Duration
		upstreamDelay  time.Duration
	)

	flag.StringVar(&concurrencyRaw, "concurrency", "32,64,128", "comma-separated worker counts")
	flag.DurationVar(&duration, "duration", 15*time.Second, "measurement duration")
	flag.DurationVar(&warmup, "warmup", 3*time.Second, "warmup duration")
	flag.BoolVar(&enableAuth, "auth", true, "enable virtual-key auth during test")
	flag.BoolVar(&enableMetrics, "metrics", true, "enable metrics middleware during test")
	flag.BoolVar(&enableRetry, "retry", false, "enable provider retry on retryable status")
	flag.IntVar(&maxRetries, "max-retries", 1, "max retry count when retry enabled")
	flag.DurationVar(&requestTimeout, "request-timeout", 5*time.Second, "HTTP client timeout per request")
	flag.DurationVar(&upstreamDelay, "upstream-latency", 0, "extra latency added by mock upstream")
	flag.Parse()

	concurrencyList, err := parseConcurrencyList(concurrencyRaw)
	if err != nil {
		return options{}, err
	}
	if duration <= 0 {
		return options{}, errors.New("duration must be > 0")
	}
	if warmup < 0 {
		return options{}, errors.New("warmup must be >= 0")
	}
	if requestTimeout <= 0 {
		return options{}, errors.New("request-timeout must be > 0")
	}
	if maxRetries < 0 {
		return options{}, errors.New("max-retries must be >= 0")
	}

	return options{
		concurrencyList: concurrencyList,
		duration:        duration,
		warmup:          warmup,
		enableAuth:      enableAuth,
		enableMetrics:   enableMetrics,
		enableRetry:     enableRetry,
		maxRetries:      maxRetries,
		requestTimeout:  requestTimeout,
		upstreamLatency: upstreamDelay,
	}, nil
}

func parseConcurrencyList(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	values := make([]int, 0, len(parts))
	seen := map[int]struct{}{}
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid concurrency value %q", value)
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		values = append(values, n)
	}
	if len(values) == 0 {
		return nil, errors.New("at least one concurrency value is required")
	}
	sort.Ints(values)
	return values, nil
}

func startMockUpstream(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
}

func startGateway(upstreamURL string, opts options) (string, func(context.Context) error, error) {
	cfg := config.DefaultConfig()
	cfg.Metrics.Enabled = opts.enableMetrics
	cfg.Cache.Enabled = false
	cfg.RateLimit.Enabled = false
	cfg.Quota.Enabled = false
	cfg.Policy.Enabled = false
	cfg.Auth.Enabled = opts.enableAuth
	cfg.Auth.Keys = nil
	cfg.Auth.VirtualKeys = nil
	if opts.enableAuth {
		cfg.Auth.VirtualKeys = map[string]config.VirtualKeyConfig{
			virtualKeyToken: {
				Enabled:         true,
				Providers:       []string{"openai-primary", "openai-backup"},
				DefaultProvider: "openai-primary",
				Routing: config.RoutingConfig{
					Strategy: "least-latency",
					Fallback: []string{"openai-backup"},
				},
			},
		}
	}

	cfg.Gateway.OpenAIPathPrefix = "/v1"
	cfg.Gateway.ProviderHeader = "X-Gateyes-Provider"
	cfg.Gateway.ProviderQuery = "provider"
	cfg.Gateway.DefaultProvider = "openai-primary"
	cfg.Gateway.Routing.Enabled = true
	cfg.Gateway.Routing.Strategy = "least-latency"
	cfg.Gateway.Routing.Fallback = []string{"openai-backup"}
	cfg.Gateway.Routing.Retry.Enabled = opts.enableRetry
	cfg.Gateway.Routing.Retry.MaxRetries = opts.maxRetries
	cfg.Gateway.Routing.Retry.InitialDelay = config.Duration{Duration: 20 * time.Millisecond}
	cfg.Gateway.Routing.Retry.MaxDelay = config.Duration{Duration: 200 * time.Millisecond}
	cfg.Gateway.Routing.Retry.Multiplier = 2
	cfg.Gateway.Routing.CustomRules = nil
	cfg.Gateway.AgentToProdUpstream = ""

	cfg.Providers = map[string]config.ProviderConfig{
		"openai-primary": {
			BaseURL:    upstreamURL,
			WSBaseURL:  "",
			APIKey:     "sk-loadtest-primary",
			AuthHeader: "Authorization",
			AuthScheme: "Bearer",
		},
		"openai-backup": {
			BaseURL:    upstreamURL,
			WSBaseURL:  "",
			APIKey:     "sk-loadtest-backup",
			AuthHeader: "Authorization",
			AuthScheme: "Bearer",
		},
	}

	metrics := middleware.NewMetrics("gateyes_loadtest")
	engine, err := router.New(&cfg, metrics)
	if err != nil {
		return "", nil, err
	}

	server := &http.Server{
		Handler:      engine,
		ReadTimeout:  cfg.Server.ReadTimeout.Duration,
		WriteTimeout: cfg.Server.WriteTimeout.Duration,
		IdleTimeout:  cfg.Server.IdleTimeout.Duration,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}

	go func() {
		_ = server.Serve(ln)
	}()

	baseURL := "http://" + ln.Addr().String()
	shutdown := func(ctx context.Context) error {
		defer func() { _ = ln.Close() }()
		return server.Shutdown(ctx)
	}

	return baseURL, shutdown, nil
}

func waitForHealth(client *http.Client, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("health check timeout")
}

func runLoad(
	client *http.Client,
	targetURL string,
	concurrency int,
	duration time.Duration,
	enableAuth bool,
	collectLatencies bool,
) (loadResult, error) {
	if concurrency <= 0 {
		return loadResult{}, errors.New("concurrency must be > 0")
	}
	if duration <= 0 {
		return loadResult{}, errors.New("duration must be > 0")
	}

	startSignal := make(chan struct{})
	stopSignal := make(chan struct{})
	timer := time.AfterFunc(duration, func() {
		close(stopSignal)
	})
	defer timer.Stop()

	resultCh := make(chan workerResult, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := workerResult{
				latencies: make([]time.Duration, 0, 1024),
				statuses:  make(map[int]int64),
				errors:    make(map[string]int64),
			}
			<-startSignal

			for {
				select {
				case <-stopSignal:
					resultCh <- worker
					return
				default:
				}

				req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(defaultPayload))
				if err != nil {
					worker.failure++
					worker.errors["request_build"]++
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				if enableAuth {
					req.Header.Set("Authorization", "Bearer "+virtualKeyToken)
				}

				start := time.Now()
				resp, err := client.Do(req)
				latency := time.Since(start)

				worker.total++
				if collectLatencies {
					worker.latencies = append(worker.latencies, latency)
				}

				if err != nil {
					worker.failure++
					worker.errors[classifyError(err)]++
					continue
				}

				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				worker.statuses[resp.StatusCode]++

				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					worker.success++
				} else {
					worker.failure++
				}
			}
		}()
	}

	start := time.Now()
	close(startSignal)
	wg.Wait()
	close(resultCh)
	elapsed := time.Since(start)

	merged := loadResult{
		elapsed:  elapsed,
		statuses: make(map[int]int64),
		errors:   make(map[string]int64),
	}
	allLatencies := make([]time.Duration, 0, 4096)
	for item := range resultCh {
		merged.total += item.total
		merged.success += item.success
		merged.failure += item.failure
		for code, count := range item.statuses {
			merged.statuses[code] += count
		}
		for key, count := range item.errors {
			merged.errors[key] += count
		}
		if collectLatencies && len(item.latencies) > 0 {
			allLatencies = append(allLatencies, item.latencies...)
		}
	}

	if elapsed > 0 {
		merged.rps = float64(merged.total) / elapsed.Seconds()
	}
	if len(allLatencies) > 0 {
		sort.Slice(allLatencies, func(i, j int) bool {
			return allLatencies[i] < allLatencies[j]
		})
		merged.p50 = percentile(allLatencies, 0.50)
		merged.p95 = percentile(allLatencies, 0.95)
		merged.p99 = percentile(allLatencies, 0.99)
		merged.max = allLatencies[len(allLatencies)-1]
	}

	return merged, nil
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func classifyError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	msg := err.Error()
	if len(msg) > 80 {
		msg = msg[:80]
	}
	return msg
}

func printSummary(results []loadResult) {
	fmt.Println()
	fmt.Println("Concurrency | Requests | Success | Failure |   RPS   |   p50   |   p95   |   p99   |   max")
	fmt.Println("------------|----------|---------|---------|---------|---------|---------|---------|---------")
	for _, r := range results {
		fmt.Printf(
			"%11d | %8d | %7d | %7d | %7.0f | %7s | %7s | %7s | %7s\n",
			r.concurrency,
			r.total,
			r.success,
			r.failure,
			r.rps,
			formatDuration(r.p50),
			formatDuration(r.p95),
			formatDuration(r.p99),
			formatDuration(r.max),
		)
	}

	for _, r := range results {
		if r.failure == 0 {
			continue
		}
		fmt.Printf("\nConcurrency=%d failure details:\n", r.concurrency)
		if len(r.statuses) > 0 {
			codes := make([]int, 0, len(r.statuses))
			for code := range r.statuses {
				codes = append(codes, code)
			}
			sort.Ints(codes)
			for _, code := range codes {
				fmt.Printf("  status_%d=%d\n", code, r.statuses[code])
			}
		}
		if len(r.errors) > 0 {
			keys := make([]string, 0, len(r.errors))
			for key := range r.errors {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Printf("  error_%s=%d\n", key, r.errors[key])
			}
		}
	}
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fus", float64(d)/float64(time.Microsecond))
	}
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}
