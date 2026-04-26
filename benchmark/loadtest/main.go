package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"text/tabwriter"
	"time"
)

type result struct {
	statusCode int
	latency    time.Duration
	err        error
}

type levelConfig struct {
	concurrency int
	duration    time.Duration
}

type levelResult struct {
	config    levelConfig
	total     int64
	success   int64
	errors    int64
	latencies []time.Duration
	codes     map[int]int64
	throughput float64
	min       time.Duration
	max       time.Duration
	avg       time.Duration
	p50       time.Duration
	p95       time.Duration
	p99       time.Duration
}

func main() {
	var (
		url       = flag.String("url", "http://localhost:8083/v1/chat/completions", "target URL")
		apiKey    = flag.String("key", os.Getenv("GATEYES_API_KEY"), "API key")
		apiSecret = flag.String("secret", os.Getenv("GATEYES_API_SECRET"), "API secret")
		duration  = flag.Duration("d", 30*time.Second, "duration per level")
		warmup    = flag.Duration("warmup", 3*time.Second, "warmup before each level")
		payload   = flag.String("body", `{"model":"glm-5.1","messages":[{"role":"user","content":"Say hello in 10 words"}],"max_tokens":50}`, "request body")
	)
	flag.Parse()

	if *apiKey == "" || *apiSecret == "" {
		log.Fatal("API key and secret required. Use -key/-secret or env GATEYES_API_KEY/GATEYES_API_SECRET")
	}

	levels := []levelConfig{
		{concurrency: 1, duration: *duration},
		{concurrency: 10, duration: *duration},
		{concurrency: 50, duration: *duration},
		{concurrency: 100, duration: *duration},
	}

	fmt.Println("=== Gateyes Load Test ===")
	fmt.Printf("Target:  %s\n", *url)
	fmt.Printf("Duration/level: %s\n", *duration)
	fmt.Printf("Warmup:  %s\n\n", *warmup)

	client := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 128,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	var results []levelResult
	for _, lvl := range levels {
		results = append(results, runLevel(client, *url, *apiKey, *apiSecret, *payload, lvl, *warmup))
	}

	printReport(results)
}

func runLevel(client *http.Client, url, key, secret, payload string, lvl levelConfig, warmup time.Duration) levelResult {
	fmt.Printf("[Level] concurrency=%d duration=%s\n", lvl.concurrency, lvl.duration)

	if warmup > 0 {
		fmt.Printf("  warmup (%s)...\n", warmup)
		ctx, cancel := context.WithTimeout(context.Background(), warmup)
		_ = runWorkers(ctx, client, url, key, secret, payload, lvl.concurrency)
		cancel()
	}

	fmt.Printf("  running...\n")
	ctx, cancel := context.WithTimeout(context.Background(), lvl.duration)
	raw := runWorkers(ctx, client, url, key, secret, payload, lvl.concurrency)
	cancel()

	return summarize(lvl, raw, lvl.duration)
}

func runWorkers(ctx context.Context, client *http.Client, url, key, secret, payload string, concurrency int) []result {
	ch := make(chan result, 10000)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				r := request(client, url, key, secret, payload)
				select {
				case ch <- r:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var out []result
	for r := range ch {
		out = append(out, r)
	}
	return out
}

func request(client *http.Client, url, key, secret, payload string) result {
	start := time.Now()
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(payload)))
	if err != nil {
		return result{err: err, latency: time.Since(start)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key+":"+secret)

	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return result{err: err, latency: latency}
	}
	defer resp.Body.Close()

	return result{statusCode: resp.StatusCode, latency: latency}
}

func summarize(lvl levelConfig, results []result, duration time.Duration) levelResult {
	if len(results) == 0 {
		return levelResult{config: lvl, codes: map[int]int64{}}
	}

	s := levelResult{
		config:    lvl,
		total:     int64(len(results)),
		codes:     make(map[int]int64),
		min:       results[0].latency,
		max:       results[0].latency,
		latencies: make([]time.Duration, 0, len(results)),
	}

	var sum time.Duration
	for _, r := range results {
		if r.err != nil || r.statusCode < 200 || r.statusCode >= 300 {
			s.errors++
		} else {
			s.success++
		}
		s.codes[r.statusCode]++
		s.latencies = append(s.latencies, r.latency)
		sum += r.latency
		if r.latency < s.min {
			s.min = r.latency
		}
		if r.latency > s.max {
			s.max = r.latency
		}
	}

	s.throughput = float64(s.total) / duration.Seconds()
	s.avg = sum / time.Duration(len(results))

	sort.Slice(s.latencies, func(i, j int) bool { return s.latencies[i] < s.latencies[j] })
	s.p50 = percentile(s.latencies, 0.50)
	s.p95 = percentile(s.latencies, 0.95)
	s.p99 = percentile(s.latencies, 0.99)
	return s
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func printReport(results []levelResult) {
	fmt.Println("\n=== Load Test Report ===")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "CC\tTotal\tOK\tErr\tRPS\tMin\tAvg\tP50\tP95\tP99\tMax\n")
	for _, r := range results {
		fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%.1f\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.config.concurrency,
			r.total,
			r.success,
			r.errors,
			r.throughput,
			r.min,
			r.avg,
			r.p50,
			r.p95,
			r.p99,
			r.max,
		)
	}
	w.Flush()

	fmt.Println("\n=== Status Codes ===")
	for _, r := range results {
		fmt.Printf("CC=%d: ", r.config.concurrency)
		for code, count := range r.codes {
			fmt.Printf("%d=%d ", code, count)
		}
		fmt.Println()
	}
}
