// Package runner sends candidate requests and memoizes the responses.
//
// Two properties matter for a minimizer. First, fidelity: the wire request
// must be exactly the candidate — so the transport never adds Accept-Encoding
// (DisableCompression), never follows redirects (the status code is often the
// oracle), and suppresses Go's default User-Agent when the candidate has
// none. Second, frugality: delta debugging revisits configurations, so
// identical candidates are answered from a cache and a hard request budget
// caps what a run may cost the target server.
package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JaydenCJ/reqmin/internal/request"
)

// ErrBudget is returned once the run has sent its maximum number of
// requests; the minimization stops and reports its best result so far.
var ErrBudget = errors.New("request budget exhausted")

// maxBodyBytes bounds how much of a response body is retained for the
// oracle (4 MiB is far beyond any sane reproduction predicate).
const maxBodyBytes = 4 << 20

// Result is the slice of a response the oracle can see.
type Result struct {
	Status int
	Header http.Header
	Body   []byte
}

// Runner sends requests through one http.Client with a memo table.
type Runner struct {
	client    *http.Client
	max       int
	sent      int
	cacheHits int
	cache     map[string]*Result
}

// New wraps client with a budget of max requests. A nil client gets the
// default transport with a 10-second timeout.
func New(client *http.Client, max int) *Runner {
	if client == nil {
		client = DefaultClient(10 * time.Second)
	}
	return &Runner{client: client, max: max, cache: map[string]*Result{}}
}

// DefaultClient builds the transport reqmin needs for faithful replay.
func DefaultClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:              http.ProxyFromEnvironment,
			DisableCompression: true, // never inject Accept-Encoding
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // 3xx is a result, not a detour
		},
	}
}

// Sent reports how many requests actually hit the network.
func (r *Runner) Sent() int { return r.sent }

// CacheHits reports how many candidates were answered from the memo table.
func (r *Runner) CacheHits() int { return r.cacheHits }

// Do sends req (or answers it from the cache) and returns the response
// slice the oracle evaluates. Cache hits do not consume budget.
func (r *Runner) Do(req *request.Request) (*Result, error) {
	key := signature(req)
	if res, ok := r.cache[key]; ok {
		r.cacheHits++
		return res, nil
	}
	if r.max > 0 && r.sent >= r.max {
		return nil, ErrBudget
	}
	r.sent++

	hreq, err := build(req)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	res := &Result{Status: resp.StatusCode, Header: resp.Header, Body: body}
	r.cache[key] = res
	return res, nil
}

// build converts the model into an *http.Request that reproduces it
// faithfully on the wire.
func build(req *request.Request) (*http.Request, error) {
	hreq, err := http.NewRequest(req.Method, req.URL(), bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	hasUA := false
	for _, h := range req.Headers {
		hreq.Header.Add(h.Name, h.Value)
		if strings.EqualFold(h.Name, "User-Agent") {
			hasUA = true
		}
	}
	if !hasUA {
		// The empty string tells net/http to omit User-Agent entirely
		// instead of sending its Go-http-client default.
		hreq.Header.Set("User-Agent", "")
	}
	if req.ExplicitHost != "" {
		hreq.Host = req.ExplicitHost
	}
	return hreq, nil
}

// signature is the memo key: everything that can reach the wire.
func signature(req *request.Request) string {
	var b strings.Builder
	b.WriteString(req.Method)
	b.WriteByte('\n')
	b.WriteString(req.URL())
	b.WriteByte('\n')
	b.WriteString(req.ExplicitHost)
	b.WriteByte('\n')
	for _, h := range req.Headers {
		b.WriteString(h.Name)
		b.WriteByte(':')
		b.WriteString(h.Value)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.Write(req.Body)
	return b.String()
}
