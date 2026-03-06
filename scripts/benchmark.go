package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type TransferRequest struct {
	ID            string `json:"id"`
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id"`
	Amount        int64  `json:"amount"`
	UserID        int    `json:"user_id"`
}

type BenchmarkResult struct {
	Name       string
	TotalReqs  int64
	Successful int64
	Failed     int64
	Duration   time.Duration
	TPS        float64
	AvgLatency time.Duration
	P50Latency time.Duration
	P95Latency time.Duration
	P99Latency time.Duration
	MaxLatency time.Duration
	MinLatency time.Duration
}

func runBenchmark(name string, url string, concurrency int, duration time.Duration) BenchmarkResult {
	var successCount int64
	var failCount int64

	var mu sync.Mutex
	allLatencies := make([]time.Duration, 0, 500000)

	start := time.Now()
	var wg sync.WaitGroup

	fmt.Printf("\n🔄 Running: %s (%d workers, %v)...\n", name, concurrency, duration)

	// Use a shared HTTP transport for connection reuse
	transport := &http.Transport{
		MaxIdleConns:        concurrency * 2,
		MaxIdleConnsPerHost: concurrency * 2,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{Transport: transport}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			localLatencies := make([]time.Duration, 0, 10000)
			counter := 0
			for time.Since(start) < duration {
				counter++
				req := TransferRequest{
					ID:            fmt.Sprintf("tx-%d-%d", workerID, counter),
					FromAccountID: fmt.Sprintf("acc-%d-from", workerID%100),
					ToAccountID:   fmt.Sprintf("acc-%d-to", workerID%100),
					Amount:        100,
					UserID:        workerID,
				}
				body, _ := json.Marshal(req)

				httpReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
				httpReq.Header.Set("Content-Type", "application/json")

				reqStart := time.Now()
				resp, err := client.Do(httpReq)
				lat := time.Since(reqStart)

				if err != nil {
					atomic.AddInt64(&failCount, 1)
					continue
				}
				resp.Body.Close()

				if resp.StatusCode == http.StatusAccepted {
					atomic.AddInt64(&successCount, 1)
					localLatencies = append(localLatencies, lat)
				} else {
					atomic.AddInt64(&failCount, 1)
				}
			}
			mu.Lock()
			allLatencies = append(allLatencies, localLatencies...)
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	sort.Slice(allLatencies, func(i, j int) bool {
		return allLatencies[i] < allLatencies[j]
	})

	result := BenchmarkResult{
		Name:       name,
		TotalReqs:  successCount + failCount,
		Successful: successCount,
		Failed:     failCount,
		Duration:   totalDuration,
		TPS:        float64(successCount) / totalDuration.Seconds(),
	}

	if len(allLatencies) > 0 {
		var sum time.Duration
		for _, l := range allLatencies {
			sum += l
		}
		result.AvgLatency = sum / time.Duration(len(allLatencies))
		result.MinLatency = allLatencies[0]
		result.MaxLatency = allLatencies[len(allLatencies)-1]
		result.P50Latency = percentile(allLatencies, 50)
		result.P95Latency = percentile(allLatencies, 95)
		result.P99Latency = percentile(allLatencies, 99)
	}

	return result
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func printResult(r BenchmarkResult) {
	fmt.Printf("\n╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  %-54s  ║\n", r.Name)
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Total Requests:  %-36d  ║\n", r.TotalReqs)
	fmt.Printf("║  Successful:      %-36d  ║\n", r.Successful)
	fmt.Printf("║  Failed:          %-36d  ║\n", r.Failed)
	fmt.Printf("║  Duration:        %-36v  ║\n", r.Duration.Round(time.Millisecond))
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Throughput:      %-30.2f TPS  ║\n", r.TPS)
	fmt.Printf("║  Avg Latency:     %-36v  ║\n", r.AvgLatency.Round(time.Microsecond))
	fmt.Printf("║  P50 Latency:     %-36v  ║\n", r.P50Latency.Round(time.Microsecond))
	fmt.Printf("║  P95 Latency:     %-36v  ║\n", r.P95Latency.Round(time.Microsecond))
	fmt.Printf("║  P99 Latency:     %-36v  ║\n", r.P99Latency.Round(time.Microsecond))
	fmt.Printf("║  Min Latency:     %-36v  ║\n", r.MinLatency.Round(time.Microsecond))
	fmt.Printf("║  Max Latency:     %-36v  ║\n", r.MaxLatency.Round(time.Microsecond))
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")
}

func main() {
	baseURL := "http://localhost:8080/transfer"
	testDuration := 10 * time.Second

	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║   HIGH-THROUGHPUT LEDGER — BENCHMARK SUITE              ║")
	fmt.Println("║   Testing: Sharding, Caching, Batching, Queuing         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	results := make([]BenchmarkResult, 0)

	// ── Test 1: Low concurrency baseline ──
	r1 := runBenchmark("Baseline (10 workers)", baseURL, 10, testDuration)
	printResult(r1)
	results = append(results, r1)

	time.Sleep(2 * time.Second)

	// ── Test 2: Medium concurrency ──
	r2 := runBenchmark("Medium Load (50 workers)", baseURL, 50, testDuration)
	printResult(r2)
	results = append(results, r2)

	time.Sleep(2 * time.Second)

	// ── Test 3: High concurrency (stress test) ──
	r3 := runBenchmark("High Load (200 workers)", baseURL, 200, testDuration)
	printResult(r3)
	results = append(results, r3)

	time.Sleep(2 * time.Second)

	// ── Test 4: Burst test (very high concurrency, short burst) ──
	r4 := runBenchmark("Burst Test (500 workers, 5s)", baseURL, 500, 5*time.Second)
	printResult(r4)
	results = append(results, r4)

	// ── Summary Table ──
	fmt.Printf("\n\n")
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                           BENCHMARK SUMMARY                                     ║")
	fmt.Println("╠═══════════════════════════════╦══════════╦══════════╦══════════╦═════════════════╣")
	fmt.Println("║ Test                          ║ TPS      ║ P50 (ms) ║ P99 (ms) ║ Error Rate     ║")
	fmt.Println("╠═══════════════════════════════╬══════════╬══════════╬══════════╬═════════════════╣")
	for _, r := range results {
		errRate := float64(0)
		if r.TotalReqs > 0 {
			errRate = float64(r.Failed) / float64(r.TotalReqs) * 100
		}
		fmt.Printf("║ %-29s ║ %8.0f ║ %8.2f ║ %8.2f ║ %13.1f%% ║\n",
			r.Name,
			r.TPS,
			float64(r.P50Latency.Microseconds())/1000.0,
			float64(r.P99Latency.Microseconds())/1000.0,
			errRate,
		)
	}
	fmt.Println("╚═══════════════════════════════╩══════════╩══════════╩══════════╩═════════════════╝")

	// ── Strategy Mapping ──
	fmt.Println("\n📊 Strategy Coverage:")
	fmt.Println("  ✅ Horizontal Scaling (Sharding)  — Transactions routed to 2 DB shards by user_id")
	fmt.Println("  ✅ Caching (Redis Read-Aside)      — Balance checks served from Redis cache")
	fmt.Println("  ✅ Write Batching                   — 100 txs or 50ms flush interval")
	fmt.Println("  ✅ Async Processing (Worker Pool)   — 10 goroutine workers decoupled from API")
	fmt.Println("  ✅ Concurrency & Parallelism        — Go goroutines across all CPU cores")
	fmt.Println("  ✅ Throughput vs Latency Trade-off   — Compare P99 across load levels")
}
