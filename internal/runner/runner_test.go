// Runner tests against an in-process loopback server: wire fidelity (no
// injected headers, no followed redirects), memoization, and the budget.
package runner

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/JaydenCJ/reqmin/internal/curl"
	"github.com/JaydenCJ/reqmin/internal/request"
)

func parseReq(t *testing.T, cmd string) *request.Request {
	t.Helper()
	req, _, err := curl.Parse(cmd)
	if err != nil {
		t.Fatalf("curl.Parse: %v", err)
	}
	return req
}

func TestDoSendsExactlyTheCandidateHeaders(t *testing.T) {
	var got http.Header
	var gotUA []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotUA = r.Header["User-Agent"]
		w.WriteHeader(204)
	}))
	defer srv.Close()

	run := New(DefaultClient(0), 0)
	_, err := run.Do(parseReq(t, `curl `+srv.URL+`/x -H 'X-Only: yes'`))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.Get("X-Only") != "yes" {
		t.Errorf("candidate header missing: %v", got)
	}
	// The transport must not smuggle defaults into the minimization: a
	// removed User-Agent stays removed rather than becoming Go's default.
	if len(gotUA) != 0 {
		t.Errorf("User-Agent leaked onto the wire: %q", gotUA)
	}
}

func TestDefaultClientDoesNotInjectAcceptEncodingOrFollowRedirects(t *testing.T) {
	var hops int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			atomic.AddInt32(&hops, 1)
			if r.Header.Get("Accept-Encoding") != "" {
				t.Errorf("Accept-Encoding injected: %q", r.Header.Get("Accept-Encoding"))
			}
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		atomic.AddInt32(&hops, 100) // must never be reached
	}))
	defer srv.Close()

	run := New(DefaultClient(0), 0)
	res, err := run.Do(parseReq(t, "curl "+srv.URL+"/start"))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.Status != http.StatusFound {
		t.Errorf("status = %d, want the raw 302 (redirects are results, not detours)", res.Status)
	}
	if atomic.LoadInt32(&hops) != 1 {
		t.Errorf("hops = %d, redirect was followed", hops)
	}
}

func TestExplicitHostReachesTheWire(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
	}))
	defer srv.Close()

	run := New(srv.Client(), 0)
	req := parseReq(t, "curl "+srv.URL+"/ -H 'Host: virtual.example.test'")
	if _, err := run.Do(req); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotHost != "virtual.example.test" {
		t.Errorf("Host on the wire = %q", gotHost)
	}
}

func TestIdenticalCandidatesAreCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte("body"))
	}))
	defer srv.Close()

	run := New(srv.Client(), 0)
	req := parseReq(t, "curl "+srv.URL+"/same -H 'A: 1'")
	for i := 0; i < 3; i++ {
		res, err := run.Do(req)
		if err != nil {
			t.Fatalf("Do #%d: %v", i, err)
		}
		if string(res.Body) != "body" {
			t.Fatalf("body = %q", res.Body)
		}
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("server saw %d requests, want 1", hits)
	}
	if run.Sent() != 1 || run.CacheHits() != 2 {
		t.Errorf("sent=%d cacheHits=%d, want 1/2", run.Sent(), run.CacheHits())
	}
	// A candidate that differs in any header value must miss the cache.
	if _, err := run.Do(parseReq(t, "curl "+srv.URL+"/same -H 'A: 2'")); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Errorf("server saw %d requests, want 2 after a differing candidate", hits)
	}
}

func TestBudgetStopsNewRequestsButServesCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	run := New(srv.Client(), 1)
	cached := parseReq(t, "curl "+srv.URL+"/a")
	if _, err := run.Do(cached); err != nil {
		t.Fatalf("first Do: %v", err)
	}
	if _, err := run.Do(parseReq(t, "curl "+srv.URL+"/b")); !errors.Is(err, ErrBudget) {
		t.Fatalf("err = %v, want ErrBudget", err)
	}
	// A cached candidate must still be answerable after exhaustion.
	if _, err := run.Do(cached); err != nil {
		t.Fatalf("cached Do after budget: %v", err)
	}
}

func TestBodyAndMethodReachTheWire(t *testing.T) {
	var gotMethod, gotBody, gotCL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		gotCL = r.Header.Get("Content-Length")
	}))
	defer srv.Close()

	run := New(srv.Client(), 0)
	if _, err := run.Do(parseReq(t, `curl -X PUT `+srv.URL+`/x --data-raw 'k=v&x=y'`)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotMethod != "PUT" || gotBody != "k=v&x=y" || gotCL != "7" {
		t.Errorf("wire = %s %q CL=%s", gotMethod, gotBody, gotCL)
	}
}
