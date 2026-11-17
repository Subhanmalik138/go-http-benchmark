package bench

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stats summarises a completed benchmark run.
type Stats struct {
	// Total is the number of requests attempted.
	Total int `json:"total_requests"`
	// Success counts responses with a 2xx status code.
	Success int `json:"success_requests"`
	// Failed counts requests that errored before a response arrived.
	Failed int `json:"failed_requests"`
	// Duration is the wall-clock time of the whole run.
	Duration time.Duration `json:"duration"`
	// RequestsPerSec is Total / Duration in seconds.
	RequestsPerSec float64 `json:"requests_per_sec"`
	// TotalBytes is the sum of all response body bytes.
	TotalBytes int64 `json:"total_bytes"`
	// Mean is the average latency over successful requests.
	Mean time.Duration `json:"mean_latency"`
	// Min and Max are the fastest and slowest successful latencies.
	Min time.Duration `json:"min_latency"`
	Max time.Duration `json:"max_latency"`
	// StdDev is the standard deviation of successful latencies.
	StdDev time.Duration `json:"stddev_latency"`
	// P50, P90, P95 and P99 are latency percentiles over successful
	// requests, using the nearest-rank method.
	P50 time.Duration `json:"p50_latency"`
	P90 time.Duration `json:"p90_latency"`
	P95 time.Duration `json:"p95_latency"`
	P99 time.Duration `json:"p99_latency"`
	// StatusCodes maps each observed HTTP status code to its count.
	StatusCodes map[int]int `json:"status_codes"`
	// ErrorCounts maps each distinct error string to its count.
	ErrorCounts map[string]int `json:"errors"`
}

// ComputeStats aggregates results into a Stats value. Latency figures are
// computed over successful requests only; a run with no successes reports
// zero latencies.
func ComputeStats(results []Result, duration time.Duration) Stats {
	s := Stats{
		Total:       len(results),
		Duration:    duration,
		StatusCodes: map[int]int{},
		ErrorCounts: map[string]int{},
	}
	var latencies []time.Duration
	var sum time.Duration
	for _, r := range results {
		if r.Err != nil {
			s.Failed++
			s.ErrorCounts[errorKind(r.Err)]++
			continue
		}
		s.StatusCodes[r.StatusCode]++
		s.TotalBytes += r.Bytes
		if r.StatusCode >= 200 && r.StatusCode < 300 {
			s.Success++
			latencies = append(latencies, r.Latency)
			sum += r.Latency
		}
	}
	if len(latencies) == 0 {
		return s
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	s.Min = latencies[0]
	s.Max = latencies[len(latencies)-1]
	s.Mean = sum / time.Duration(len(latencies))
	s.P50 = percentile(latencies, 50)
	s.P90 = percentile(latencies, 90)
	s.P95 = percentile(latencies, 95)
	s.P99 = percentile(latencies, 99)

	var variance float64
	for _, l := range latencies {
		d := float64(l - s.Mean)
		variance += d * d
	}
	s.StdDev = time.Duration(math.Sqrt(variance / float64(len(latencies))))

	if duration > 0 {
		s.RequestsPerSec = float64(s.Total) / duration.Seconds()
	}
	return s
}

// percentile returns the nearest-rank percentile of a sorted slice.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	// Nearest-rank: ceil(p/100 * N), 1-indexed.
	rank := int(math.Ceil(float64(p) / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// errorKind reduces an error to a short, countable label so the report
// groups "connection refused" separately from "timeout" instead of
// printing a distinct line per URL.
func errorKind(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "no such host"):
		return "dns error"
	case strings.Contains(msg, "connection reset"):
		return "connection reset"
	default:
		return "request error"
	}
}

// String renders the human-readable report printed at the end of a run.
func (s Stats) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Requests:      %d (%d ok, %d failed)\n", s.Total, s.Success, s.Failed)
	fmt.Fprintf(&b, "Duration:      %s\n", s.Duration.Round(time.Millisecond))
	fmt.Fprintf(&b, "Requests/sec:  %.2f\n", s.RequestsPerSec)
	fmt.Fprintf(&b, "Data received: %s\n", humanBytes(s.TotalBytes))
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "Latency (successful requests):")
	fmt.Fprintf(&b, "  min    %s\n", s.Min)
	fmt.Fprintf(&b, "  mean   %s\n", s.Mean)
	fmt.Fprintf(&b, "  stddev %s\n", s.StdDev)
	fmt.Fprintf(&b, "  p50    %s\n", s.P50)
	fmt.Fprintf(&b, "  p90    %s\n", s.P90)
	fmt.Fprintf(&b, "  p95    %s\n", s.P95)
	fmt.Fprintf(&b, "  p99    %s\n", s.P99)
	fmt.Fprintf(&b, "  max    %s\n", s.Max)
	if len(s.StatusCodes) > 0 {
		fmt.Fprintln(&b, "")
		fmt.Fprintln(&b, "Status codes:")
		codes := make([]int, 0, len(s.StatusCodes))
		for code := range s.StatusCodes {
			codes = append(codes, code)
		}
		sort.Ints(codes)
		for _, code := range codes {
			fmt.Fprintf(&b, "  %d  %d\n", code, s.StatusCodes[code])
		}
	}
	if len(s.ErrorCounts) > 0 {
		fmt.Fprintln(&b, "")
		fmt.Fprintln(&b, "Errors:")
		for kind, count := range s.ErrorCounts {
			fmt.Fprintf(&b, "  %s: %d\n", kind, count)
		}
	}
	return b.String()
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
