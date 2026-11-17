package bench

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"missing url", Config{Requests: 10, Concurrency: 1}, "url is required"},
		{"bad scheme", Config{URL: "ftp://example.com", Requests: 10, Concurrency: 1}, "scheme must be http or https"},
		{"no host", Config{URL: "https://", Requests: 10, Concurrency: 1}, "url must include a host"},
		{"no limit", Config{URL: "https://example.com", Concurrency: 1}, "either -n (requests) or -d (duration)"},
		{"zero concurrency", Config{URL: "https://example.com", Requests: 10, Concurrency: 0}, "concurrency must be >= 1"},
		{"negative requests", Config{URL: "https://example.com", Requests: -1, Concurrency: 1}, "requests must be >= 0"},
		{"valid requests", Config{URL: "https://example.com", Requests: 10, Concurrency: 2}, ""},
		{"valid duration", Config{URL: "http://localhost:8080", Duration: time.Second, Concurrency: 2}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
			}
		})
	}
}

func TestParsedHeaders(t *testing.T) {
	cfg := Config{Headers: []string{"X-Api-Key: secret", "Accept: application/json"}}
	h, err := cfg.ParsedHeaders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := h.Get("X-Api-Key"); got != "secret" {
		t.Errorf("X-Api-Key = %q, want %q", got, "secret")
	}
	if got := h.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want %q", got, "application/json")
	}

	bad := Config{Headers: []string{"NoColonHere"}}
	if _, err := bad.ParsedHeaders(); err == nil {
		t.Error("expected error for header without a colon")
	}
}

func TestNewRequestAppliesMethodHeadersAndBody(t *testing.T) {
	cfg := Config{
		URL:     "http://example.com/echo",
		Method:  "POST",
		Headers: []string{"Content-Type: application/json"},
		Body:    `{"ping":true}`,
	}
	req, err := cfg.NewRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestComputeStatsPercentiles(t *testing.T) {
	// 10 requests at 10..100ms: nearest-rank p50 = 50ms, p90 = 90ms,
	// p99 = 100ms.
	var results []Result
	for i := 1; i <= 10; i++ {
		results = append(results, Result{
			Latency:    time.Duration(i*10) * time.Millisecond,
			StatusCode: 200,
			Bytes:      100,
		})
	}
	s := ComputeStats(results, time.Second)
	if s.Total != 10 || s.Success != 10 || s.Failed != 0 {
		t.Errorf("counts = %d/%d/%d, want 10/10/0", s.Total, s.Success, s.Failed)
	}
	if s.Min != 10*time.Millisecond || s.Max != 100*time.Millisecond {
		t.Errorf("min/max = %v/%v, want 10ms/100ms", s.Min, s.Max)
	}
	if s.Mean != 55*time.Millisecond {
		t.Errorf("mean = %v, want 55ms", s.Mean)
	}
	if s.P50 != 50*time.Millisecond {
		t.Errorf("p50 = %v, want 50ms", s.P50)
	}
	if s.P90 != 90*time.Millisecond {
		t.Errorf("p90 = %v, want 90ms", s.P90)
	}
	if s.P95 != 100*time.Millisecond {
		t.Errorf("p95 = %v, want 100ms", s.P95)
	}
	if s.P99 != 100*time.Millisecond {
		t.Errorf("p99 = %v, want 100ms", s.P99)
	}
	if s.TotalBytes != 1000 {
		t.Errorf("total bytes = %d, want 1000", s.TotalBytes)
	}
	if s.RequestsPerSec != 10 {
		t.Errorf("rps = %v, want 10", s.RequestsPerSec)
	}
	if s.StatusCodes[200] != 10 {
		t.Errorf("status 200 count = %d, want 10", s.StatusCodes[200])
	}
}

func TestComputeStatsFailuresAndNon2xx(t *testing.T) {
	results := []Result{
		{Latency: 5 * time.Millisecond, StatusCode: 200, Bytes: 10},
		{Latency: 5 * time.Millisecond, StatusCode: 404, Bytes: 20},
		{Latency: 5 * time.Millisecond, StatusCode: 500, Bytes: 30},
		{Latency: time.Millisecond, Err: errors.New("connection refused")},
	}
	s := ComputeStats(results, time.Second)
	if s.Total != 4 || s.Success != 1 || s.Failed != 1 {
		t.Errorf("counts = %d/%d/%d, want 4/1/1", s.Total, s.Success, s.Failed)
	}
	if s.StatusCodes[404] != 1 || s.StatusCodes[500] != 1 {
		t.Errorf("status codes = %v, want 404:1 and 500:1", s.StatusCodes)
	}
	if s.ErrorCounts["connection refused"] != 1 {
		t.Errorf("errors = %v, want connection refused:1", s.ErrorCounts)
	}
	// Latencies are computed over the single successful request.
	if s.Mean != 5*time.Millisecond || s.P50 != 5*time.Millisecond {
		t.Errorf("mean/p50 = %v/%v, want 5ms/5ms", s.Mean, s.P50)
	}
	if s.TotalBytes != 60 {
		t.Errorf("total bytes = %d, want 60", s.TotalBytes)
	}
}

func TestComputeStatsEmpty(t *testing.T) {
	s := ComputeStats(nil, time.Second)
	if s.Total != 0 || s.Mean != 0 || s.P99 != 0 || s.RequestsPerSec != 0 {
		t.Errorf("empty stats not zero: %+v", s)
	}
}

func TestRunnerHitsServer(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	cfg := Config{URL: srv.URL, Requests: 50, Concurrency: 5}
	r := NewRunner(cfg)
	results := r.Run(context.Background())
	if len(results) != 50 {
		t.Fatalf("got %d results, want 50", len(results))
	}
	if int(hits.Load()) != 50 {
		t.Errorf("server saw %d hits, want 50", hits.Load())
	}
	s := ComputeStats(results, time.Second)
	if s.Success != 50 || s.Failed != 0 {
		t.Errorf("success/failed = %d/%d, want 50/0", s.Success, s.Failed)
	}
	if s.TotalBytes != 50*5 {
		t.Errorf("total bytes = %d, want 250", s.TotalBytes)
	}
}

func TestRunnerCountsErrors(t *testing.T) {
	// Port 1 on localhost is (practically) never listening: every
	// request must fail, and the runner must report them, not hang.
	cfg := Config{URL: "http://127.0.0.1:1", Requests: 10, Concurrency: 2, Timeout: 2 * time.Second}
	results := NewRunner(cfg).Run(context.Background())
	if len(results) != 10 {
		t.Fatalf("got %d results, want 10", len(results))
	}
	s := ComputeStats(results, time.Second)
	if s.Failed != 10 {
		t.Errorf("failed = %d, want 10", s.Failed)
	}
	if len(s.ErrorCounts) == 0 {
		t.Error("expected at least one error kind to be recorded")
	}
}

func TestRunnerDurationCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := Config{URL: srv.URL, Duration: 300 * time.Millisecond, Concurrency: 4}
	start := time.Now()
	results := NewRunner(cfg).Run(context.Background())
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("duration-capped run took %v, expected to stop near 300ms", elapsed)
	}
	if len(results) == 0 {
		t.Error("expected at least one request to complete")
	}
}

func TestRunnerSendsHeadersAndMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Errorf("X-Test = %q, want yes", got)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := Config{
		URL: srv.URL, Method: "POST", Requests: 5, Concurrency: 2,
		Headers: []string{"X-Test: yes"}, Body: "{}",
	}
	results := NewRunner(cfg).Run(context.Background())
	s := ComputeStats(results, time.Second)
	if s.StatusCodes[201] != 5 {
		t.Errorf("status 201 count = %d, want 5", s.StatusCodes[201])
	}
}
