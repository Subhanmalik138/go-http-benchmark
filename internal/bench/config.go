// Package bench implements the core of httpload: a concurrent HTTP
// benchmarking engine that fires requests at a target URL, records the
// latency of every request, and summarises the run into statistics.
package bench

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config controls a benchmark run. Either Requests or Duration must be
// set; when both are set, the run stops as soon as either limit is hit.
type Config struct {
	// URL is the target to benchmark. Required.
	URL string
	// Method is the HTTP method to use, e.g. GET or POST. Defaults to GET.
	Method string
	// Requests is the total number of requests to send. 0 means unlimited
	// (only used together with Duration).
	Requests int
	// Concurrency is the number of workers sending requests in parallel.
	// Must be >= 1.
	Concurrency int
	// Duration caps the wall-clock length of the run. 0 means no cap.
	Duration time.Duration
	// Timeout is the per-request timeout. Defaults to 30s.
	Timeout time.Duration
	// Headers are extra request headers, in "Name: value" form.
	Headers []string
	// Body is an optional request body, sent with every request.
	Body string
}

// Validate checks a Config and returns a descriptive error for the first
// problem found.
func (c Config) Validate() error {
	if c.URL == "" {
		return errors.New("url is required")
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("url must include a host")
	}
	if c.Method == "" {
		c.Method = http.MethodGet
	}
	if c.Requests < 0 {
		return errors.New("requests must be >= 0")
	}
	if c.Requests == 0 && c.Duration <= 0 {
		return errors.New("either -n (requests) or -d (duration) must be set")
	}
	if c.Concurrency < 1 {
		return errors.New("concurrency must be >= 1")
	}
	if c.Timeout < 0 {
		return errors.New("timeout must be >= 0")
	}
	return nil
}

// ParsedHeaders turns the raw "Name: value" header strings into an
// http.Header. A header without a colon is rejected.
func (c Config) ParsedHeaders() (http.Header, error) {
	h := http.Header{}
	for _, raw := range c.Headers {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("invalid header %q: expected \"Name: value\"", raw)
		}
		h.Add(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	return h, nil
}

// NewRequest builds one request for the run, applying the configured
// method, headers and body.
func (c Config) NewRequest() (*http.Request, error) {
	var body io.Reader
	if c.Body != "" {
		body = strings.NewReader(c.Body)
	}
	method := c.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequest(method, c.URL, body)
	if err != nil {
		return nil, err
	}
	headers, err := c.ParsedHeaders()
	if err != nil {
		return nil, err
	}
	req.Header = headers
	return req, nil
}
