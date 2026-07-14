// Raw HTTP/1.1 message parsing and rendering tests: the .http-file dialect
// spoken by proxies and REST clients must survive a parse/render cycle
// without losing header order, the body, or the authority.
package rawhttp

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/reqmin/internal/request"
)

const sample = "POST /search?q=x&lang=en HTTP/1.1\r\n" +
	"Host: api.example.test\r\n" +
	"Authorization: Bearer tok\r\n" +
	"Content-Length: 9\r\n" +
	"\r\n" +
	"q=x&extra"

func TestParseOriginFormRequest(t *testing.T) {
	req, err := Parse(sample, "http")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if req.Method != "POST" || req.Host != "api.example.test" || req.Path != "/search" {
		t.Errorf("parsed %s %s %s", req.Method, req.Host, req.Path)
	}
	if got := request.EncodePairs(req.Query); got != "q=x&lang=en" {
		t.Errorf("query = %q", got)
	}
	if string(req.Body) != "q=x&extra" {
		t.Errorf("body = %q", req.Body)
	}
	// Content-Length is recomputed at send time; keeping the stale value
	// would corrupt every shrunken candidate.
	if _, ok := req.HeaderGet("Content-Length"); ok {
		t.Fatal("Content-Length must not survive parsing")
	}
}

func TestParseSchemeSelection(t *testing.T) {
	// Origin-form targets take the caller's scheme...
	req, err := Parse("GET / HTTP/1.1\nHost: example.test\n", "https")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if req.URL() != "https://example.test/" {
		t.Errorf("origin-form URL = %q", req.URL())
	}
	// ...but absolute-form targets carry their own scheme and win.
	req, err = Parse("GET https://example.test/x HTTP/1.1\n", "http")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if req.URL() != "https://example.test/x" {
		t.Errorf("absolute-form URL = %q", req.URL())
	}
}

func TestParseAbsoluteFormWithDivergentHostHeader(t *testing.T) {
	req, err := Parse("GET http://127.0.0.1:9/ HTTP/1.1\nHost: virtual.example.test\n", "http")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if req.Host != "127.0.0.1:9" || req.ExplicitHost != "virtual.example.test" {
		t.Errorf("host = %q, explicit = %q", req.Host, req.ExplicitHost)
	}
	// The explicit Host must also win when rendering back.
	if !strings.Contains(Render(req), "Host: virtual.example.test\n") {
		t.Errorf("rendered:\n%s", Render(req))
	}
}

func TestParseRequestLineWithoutProtocolDefaults(t *testing.T) {
	req, err := Parse("GET /x\nHost: example.test\n", "http")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if req.Proto != "HTTP/1.1" {
		t.Errorf("proto = %q", req.Proto)
	}
}

func TestParseTrimsSingleTrailingNewlineFromBody(t *testing.T) {
	req, err := Parse("POST / HTTP/1.1\nHost: h.example.test\n\npayload\n", "http")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(req.Body) != "payload" {
		t.Errorf("body = %q, want file-final newline stripped", req.Body)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty":                "",
		"garbage request line": "NOT-A-REQUEST\n",
		"origin form no host":  "GET /x HTTP/1.1\n\n",
		"bad protocol":         "GET / SPDY/9\nHost: h.example.test\n",
		"folded header":        "GET / HTTP/1.1\nHost: h.example.test\nX-A: 1\n more\n",
		"header without colon": "GET / HTTP/1.1\nHost h.example.test\n",
		"relative-ish target":  "GET x/y HTTP/1.1\nHost: h.example.test\n",
		"too many line fields": "GET / HTTP/1.1 extra\nHost: h.example.test\n",
	}
	for name, text := range cases {
		if _, err := Parse(text, "http"); err == nil {
			t.Errorf("%s: Parse succeeded, want error", name)
		}
	}
}

func TestRenderRoundTrip(t *testing.T) {
	req, err := Parse(sample, "http")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rendered := Render(req)
	if !strings.Contains(rendered, "Content-Length: 9\n") {
		t.Errorf("rendered output must recompute Content-Length:\n%s", rendered)
	}
	back, err := Parse(rendered, "http")
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if back.URL() != req.URL() || string(back.Body) != string(req.Body) {
		t.Errorf("round trip mismatch: %q/%q vs %q/%q", back.URL(), back.Body, req.URL(), req.Body)
	}
	if len(back.Headers) != len(req.Headers) {
		t.Errorf("header count changed: %d -> %d", len(req.Headers), len(back.Headers))
	}
}
