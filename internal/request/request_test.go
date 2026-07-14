// Request model tests: lossless pair handling is the foundation every
// other package builds on — dropping neighbors must never re-encode or
// reorder what survives.
package request

import (
	"reflect"
	"testing"
)

func TestParsePairsKeepsRawEncodingAndOrder(t *testing.T) {
	pairs := ParsePairs("b=%2Fx%2F&a=1&flag&empty=")
	raws := make([]string, len(pairs))
	for i, p := range pairs {
		raws[i] = p.Raw
	}
	want := []string{"b=%2Fx%2F", "a=1", "flag", "empty="}
	if !reflect.DeepEqual(raws, want) {
		t.Fatalf("raws = %q, want %q", raws, want)
	}
	if got := EncodePairs(pairs); got != "b=%2Fx%2F&a=1&flag&empty=" {
		t.Fatalf("EncodePairs = %q", got)
	}
	// Empty segments carry nothing and are skipped.
	if got := len(ParsePairs("a=1&&b=2&")); got != 2 {
		t.Fatalf("pair count = %d, want 2", got)
	}
	if ParsePairs("") != nil {
		t.Fatal("empty input must yield no pairs")
	}
	// Keys decode percent-encoding for display; invalid encoding falls
	// back to the raw text instead of erroring.
	if got := (Pair{Raw: "user%20id=42"}).Key(); got != "user id" {
		t.Fatalf("Key = %q", got)
	}
	if got := (Pair{Raw: "plain"}).Key(); got != "plain" {
		t.Fatalf("valueless Key = %q", got)
	}
	if got := (Pair{Raw: "bad%zz=1"}).Key(); got != "bad%zz" {
		t.Fatalf("invalid-encoding Key = %q", got)
	}
}

func TestSetURLAndRebuild(t *testing.T) {
	var r Request
	if err := r.SetURL("https://api.example.test:8443/v1/items?q=a%2Fb&x"); err != nil {
		t.Fatalf("SetURL: %v", err)
	}
	if r.Scheme != "https" || r.Host != "api.example.test:8443" || r.Path != "/v1/items" {
		t.Errorf("parsed %s %s %s", r.Scheme, r.Host, r.Path)
	}
	if got := r.URL(); got != "https://api.example.test:8443/v1/items?q=a%2Fb&x" {
		t.Errorf("URL = %q", got)
	}
	// A URL without a path normalizes to "/".
	if err := r.SetURL("http://example.test"); err != nil {
		t.Fatalf("SetURL: %v", err)
	}
	if r.URL() != "http://example.test/" {
		t.Errorf("URL = %q", r.URL())
	}
}

func TestSetURLRejectsBadInput(t *testing.T) {
	var r Request
	for _, raw := range []string{"ftp://example.test/", "http://", "://nope", "http://\x7f bad"} {
		if err := r.SetURL(raw); err == nil {
			t.Errorf("SetURL(%q): want error", raw)
		}
	}
}

func TestHeaderGetIsCaseInsensitiveFirstMatch(t *testing.T) {
	r := Request{Headers: []Header{{Name: "X-Dup", Value: "first"}, {Name: "x-dup", Value: "second"}}}
	if v, ok := r.HeaderGet("X-DUP"); !ok || v != "first" {
		t.Fatalf("HeaderGet = %q, %v", v, ok)
	}
	if _, ok := r.HeaderGet("Missing"); ok {
		t.Fatal("missing header reported present")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	orig := Request{Method: "GET", Headers: []Header{{Name: "A", Value: "1"}}, Query: []Pair{{Raw: "x=1"}}, Body: []byte("b")}
	c := orig.Clone()
	c.Headers[0].Value = "mutated"
	c.Query[0].Raw = "mutated"
	c.Body[0] = 'z'
	if orig.Headers[0].Value != "1" || orig.Query[0].Raw != "x=1" || string(orig.Body) != "b" {
		t.Fatalf("clone shares memory with the original: %+v", orig)
	}
}

func TestParseCookiesSplitsAndTrims(t *testing.T) {
	got := ParseCookies("sid=abc; theme=dark;;  flag ")
	want := []string{"sid=abc", "theme=dark", "flag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cookies = %q, want %q", got, want)
	}
	if CookieName("sid=abc") != "sid" || CookieName("bare") != "bare" {
		t.Fatal("CookieName misparsed")
	}
}
