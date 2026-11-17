// Command httpload benchmarks an HTTP endpoint: it fires concurrent
// requests at a URL and reports latency percentiles, throughput, and the
// status-code breakdown.
//
// Usage:
//
//	httpload -url https://example.com -n 1000 -c 50
//	httpload -url https://example.com -d 30s -c 20 -json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Subhanmalik138/go-http-benchmark/internal/bench"
)

type headerList []string

func (h *headerList) String() string     { return strings.Join(*h, ", ") }
func (h *headerList) Set(v string) error { *h = append(*h, v); return nil }

func main() {
	var (
		url         = flag.String("url", "", "Target URL to benchmark (required)")
		method      = flag.String("method", "GET", "HTTP method")
		requests    = flag.Int("n", 0, "Total number of requests to send")
		concurrency = flag.Int("c", 10, "Number of concurrent workers")
		duration    = flag.Duration("d", 0, "Maximum run duration, e.g. 30s (stops the run when reached)")
		timeout     = flag.Duration("timeout", 30*time.Second, "Per-request timeout")
		body        = flag.String("body", "", "Request body sent with every request")
		jsonOut     = flag.Bool("json", false, "Print the report as JSON instead of a table")
	)
	var headers headerList
	flag.Var(&headers, "H", "Extra request header, \"Name: value\" (repeatable)")
	flag.Parse()

	cfg := bench.Config{
		URL:         *url,
		Method:      *method,
		Requests:    *requests,
		Concurrency: *concurrency,
		Duration:    *duration,
		Timeout:     *timeout,
		Headers:     headers,
		Body:        *body,
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "httpload: %v\n\n", err)
		flag.Usage()
		os.Exit(2)
	}
	if _, err := cfg.ParsedHeaders(); err != nil {
		fmt.Fprintf(os.Stderr, "httpload: %v\n", err)
		os.Exit(2)
	}

	// Ctrl-C cancels the run cleanly; partial results are still reported.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Printf("Benchmarking %s %s with %d workers", cfg.Method, cfg.URL, cfg.Concurrency)
	if cfg.Requests > 0 {
		fmt.Printf(", %d requests", cfg.Requests)
	}
	if cfg.Duration > 0 {
		fmt.Printf(", up to %s", cfg.Duration)
	}
	fmt.Println("...")

	start := time.Now()
	results := bench.NewRunner(cfg).Run(ctx)
	elapsed := time.Since(start)

	stats := bench.ComputeStats(results, elapsed)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(stats); err != nil {
			fmt.Fprintf(os.Stderr, "httpload: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Println()
	fmt.Print(stats.String())
}
