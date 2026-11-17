package bench

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Result records the outcome of a single request.
type Result struct {
	// Latency is the time from sending the request to reading the full body.
	Latency time.Duration
	// StatusCode is the HTTP status code, or 0 when the request failed
	// before a response arrived.
	StatusCode int
	// Bytes is the number of response body bytes read.
	Bytes int64
	// Err is non-nil when the request failed (timeout, refused, ...).
	Err error
}

// Runner executes a benchmark run: a pool of workers sends requests until
// the request count or the duration limit is reached.
type Runner struct {
	cfg    Config
	client *http.Client
}

// NewRunner builds a Runner for cfg. The HTTP client is tuned for the
// benchmark: keep-alive connections are reused up to the worker count so
// connection setup does not dominate the measurement.
func NewRunner(cfg Config) *Runner {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	transport := &http.Transport{
		MaxIdleConns:        cfg.Concurrency,
		MaxIdleConnsPerHost: cfg.Concurrency,
	}
	return &Runner{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Follow at most 10 redirects, like a normal client.
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// Run fires requests until the configured limit is hit and returns the
// collected results in completion order.
func (r *Runner) Run(ctx context.Context) []Result {
	cfg := r.cfg

	// A deadline applies when a duration is configured.
	if cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	}

	jobs := make(chan struct{})
	results := make(chan Result, cfg.Concurrency*2)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				results <- r.doRequest(ctx)
			}
		}()
	}

	// Feed jobs until the request budget is exhausted or the context ends.
	go func() {
		defer close(jobs)
		sent := 0
		for cfg.Requests == 0 || sent < cfg.Requests {
			select {
			case <-ctx.Done():
				return
			case jobs <- struct{}{}:
				sent++
			}
		}
	}()

	// Close the results channel once every worker has finished.
	go func() {
		wg.Wait()
		close(results)
	}()

	var out []Result
	for res := range results {
		out = append(out, res)
	}
	return out
}

// doRequest sends one request and measures it. The body is fully read and
// discarded so the connection can be reused and Bytes reflects the real
// payload size.
func (r *Runner) doRequest(ctx context.Context) Result {
	req, err := r.cfg.NewRequest()
	if err != nil {
		return Result{Err: err}
	}
	req = req.WithContext(ctx)

	start := time.Now()
	resp, err := r.client.Do(req)
	if err != nil {
		return Result{Latency: time.Since(start), Err: err}
	}
	defer resp.Body.Close()
	n, _ := io.Copy(io.Discard, resp.Body)
	return Result{
		Latency:    time.Since(start),
		StatusCode: resp.StatusCode,
		Bytes:      n,
	}
}
