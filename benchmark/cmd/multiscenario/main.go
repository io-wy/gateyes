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

type scenario struct {
	name    string
	method  string
	path    string
	body    string
	weight  int
}

type result struct {
	scenario   string
	statusCode int
	latency    time.Duration
	err        error
}

type scenarioStats struct {
	name      string
	requests  int64
	success   int64
	errors    int64
	latencies []time.Duration
	codes     map[int]int64
	min       time.Duration
	max       time.Duration
	avg       time.Duration
	p50       time.Duration
	p95       time.Duration
	p99       time.Duration
}

func main() {
	var (
		baseURL   = flag.String("url", "http://localhost:8083", "gateway base URL")
		apiKey    = flag.String("key", os.Getenv("GATEYES_API_KEY"), "API key")
		apiSecret = flag.String("secret", os.Getenv("GATEYES_API_SECRET"), "API secret")
		cc        = flag.Int("c", 50, "concurrency")
		duration  = flag.Duration("d", 60*time.Second, "total duration")
		warmup    = flag.Duration("warmup", 5*time.Second, "warmup duration")
	)
	flag.Parse()

	if *apiKey == "" || *apiSecret == "" {
		log.Fatal("API key and secret required")
	}

	scenarios := []scenario{
		{
			name:   "chat",
			method: "POST",
			path:   "/v1/chat/completions",
			body:   `{"model":"LongCat-Flash-Chat","messages":[{"role":"user","content":"Hello, how are you?"}],"max_tokens":50}`,
			weight: 70,
		},
		{
			name:   "responses",
			method: "POST",
			path:   "/v1/responses",
			body:   `{"model":"LongCat-Flash-Chat","input":"What is the capital of France?","max_output_tokens":50}`,
			weight: 20,
		},
		{
			name:   "embeddings",
			method: "POST",
			path:   "/v1/embeddings",
			body:   `{"model":"text-embedding-3-small","input":"hello world"}`,
			weight: 10,
		},
	}

	fmt.Println("=== Gateyes Multi-Scenario Load Test ===")
	fmt.Printf("URL:  %s\n", *baseURL)
	fmt.Printf("CC:   %d\n", *cc)
	fmt.Printf("Dur:  %s\n\n", *duration)

	client := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 128,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	if *warmup > 0 {
		fmt.Printf("warmup (%s)...\n", *warmup)
		ctx, cancel := context.WithTimeout(context.Background(), *warmup)
		_ = runMixed(ctx, client, *baseURL, *apiKey, *apiSecret, scenarios, *cc)
		cancel()
	}

	fmt.Printf("running...\n")
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	raw := runMixed(ctx, client, *baseURL, *apiKey, *apiSecret, scenarios, *cc)
	cancel()

	stats := aggregate(raw, scenarios)
	printScenarioReport(stats, float64(len(raw)) / duration.Seconds())
}

func runMixed(ctx context.Context, client *http.Client, baseURL, key, secret string, scenarios []scenario, concurrency int) []result {
	ch := make(chan result, 20000)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				s := pickScenario(scenarios)
				r := doRequest(client, baseURL+s.path, s.method, key, secret, s.body)
				r.scenario = s.name
				select {
				case ch <- r:
				case <-ctx.Done():
					return
				}
			}
		}(i)
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

func pickScenario(scenarios []scenario) scenario {
	total := 0
	for _, s := range scenarios {
		total += s.weight
	}
	target := time.Now().UnixNano() % int64(total)
	for _, s := range scenarios {
		target -= int64(s.weight)
		if target < 0 {
			return s
		}
	}
	return scenarios[0]
}

func doRequest(client *http.Client, url, method, key, secret, body string) result {
	start := time.Now()
	req, err := http.NewRequest(method, url, bytes.NewReader([]byte(body)))
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

func aggregate(results []result, scenarios []scenario) []scenarioStats {
	m := make(map[string]*scenarioStats)
	for _, s := range scenarios {
		m[s.name] = &scenarioStats{name: s.name, codes: make(map[int]int64)}
	}

	for _, r := range results {
		st, ok := m[r.scenario]
		if !ok {
			continue
		}
		st.requests++
		if r.err != nil || r.statusCode < 200 || r.statusCode >= 300 {
			st.errors++
		} else {
			st.success++
		}
		st.codes[r.statusCode]++
		st.latencies = append(st.latencies, r.latency)
	}

	var out []scenarioStats
	for _, s := range scenarios {
		st := m[s.name]
		if st.requests == 0 {
			continue
		}
		sort.Slice(st.latencies, func(i, j int) bool { return st.latencies[i] < st.latencies[j] })
		st.min = st.latencies[0]
		st.max = st.latencies[len(st.latencies)-1]
		var sum time.Duration
		for _, d := range st.latencies {
			sum += d
		}
		st.avg = sum / time.Duration(len(st.latencies))
		st.p50 = percentile(st.latencies, 0.50)
		st.p95 = percentile(st.latencies, 0.95)
		st.p99 = percentile(st.latencies, 0.99)
		out = append(out, *st)
	}
	return out
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func printScenarioReport(stats []scenarioStats, totalRPS float64) {
	fmt.Println("\n=== Multi-Scenario Report ===")
	fmt.Printf("Total RPS: %.1f\n\n", totalRPS)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Scenario\tReqs\tOK\tErr\tMin\tAvg\tP50\tP95\tP99\tMax\n")
	for _, s := range stats {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.name,
			s.requests,
			s.success,
			s.errors,
			s.min,
			s.avg,
			s.p50,
			s.p95,
			s.p99,
			s.max,
		)
	}
	w.Flush()

	fmt.Println("\n=== Status Codes ===")
	for _, s := range stats {
		fmt.Printf("%s: ", s.name)
		for code, count := range s.codes {
			fmt.Printf("%d=%d ", code, count)
		}
		fmt.Println()
	}
}
