// Parser and renderer tests: the flag subset browsers and API clients emit
// must map onto the request model faithfully, and Render must produce a
// command that Parse reads back identically (the round-trip is what users
// paste into their terminal after minimization).
package curl

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JaydenCJ/reqmin/internal/request"
)

func mustParse(t *testing.T, cmd string) *request.Request {
	t.Helper()
	req, _, err := Parse(cmd)
	if err != nil {
		t.Fatalf("Parse(%q): %v", cmd, err)
	}
	return req
}

func TestParseMinimalGET(t *testing.T) {
	req := mustParse(t, "curl http://api.example.test/v1/users?id=7")
	if req.Method != "GET" {
		t.Errorf("method = %q, want GET", req.Method)
	}
	if got := req.URL(); got != "http://api.example.test/v1/users?id=7" {
		t.Errorf("URL = %q", got)
	}
	if len(req.Query) != 1 || req.Query[0].Key() != "id" {
		t.Errorf("query = %+v, want one pair id", req.Query)
	}
	// curl treats scheme-less URLs as http.
	if got := mustParse(t, `curl example.test/health`).URL(); got != "http://example.test/health" {
		t.Errorf("scheme-less URL = %q", got)
	}
}

func TestParseHeadersKeepOrderAndDuplicates(t *testing.T) {
	req := mustParse(t, `curl url -H 'B: 2' -H 'A: 1' -H 'B: 3'`)
	want := []request.Header{{Name: "B", Value: "2"}, {Name: "A", Value: "1"}, {Name: "B", Value: "3"}}
	if !reflect.DeepEqual(req.Headers, want) {
		t.Fatalf("headers = %+v, want %+v", req.Headers, want)
	}
}

func TestParseDataImpliesPOSTAndFormContentType(t *testing.T) {
	req := mustParse(t, `curl http://example.test/login -d 'user=a' -d 'pass=b'`)
	if req.Method != "POST" {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if string(req.Body) != "user=a&pass=b" {
		t.Errorf("body = %q, want joined data", req.Body)
	}
	if ct, _ := req.HeaderGet("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q, want curl's implicit form default", ct)
	}
	// An explicit Content-Type must never be overridden by the default.
	req = mustParse(t, `curl url -H 'Content-Type: application/json' -d '{"a":1}'`)
	if ct, _ := req.HeaderGet("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want the explicit one", ct)
	}
}

func TestParseMethodSelection(t *testing.T) {
	if m := mustParse(t, `curl -X PUT url -d payload`).Method; m != "PUT" {
		t.Errorf("explicit method = %q, want PUT", m)
	}
	if m := mustParse(t, `curl -I http://example.test/`).Method; m != "HEAD" {
		t.Errorf("-I method = %q, want HEAD", m)
	}
}

func TestParseGetModeMovesDataToQuery(t *testing.T) {
	req := mustParse(t, `curl -G http://example.test/search -d 'q=x' -d 'page=2'`)
	if req.Method != "GET" || len(req.Body) != 0 {
		t.Fatalf("want bodyless GET, got %s with body %q", req.Method, req.Body)
	}
	if got := req.URL(); got != "http://example.test/search?q=x&page=2" {
		t.Errorf("URL = %q", got)
	}
}

func TestParseDataUrlencodeForms(t *testing.T) {
	// curl supports "content", "=content", and "name=content".
	req := mustParse(t, `curl -G url --data-urlencode 'q=a b&c' --data-urlencode '=x y'`)
	if got := request.EncodePairs(req.Query); got != "q=a+b%26c&x+y" {
		t.Errorf("query = %q", got)
	}
}

func TestParseConvenienceFlagsBecomeHeaders(t *testing.T) {
	req := mustParse(t, `curl -u alice:secret -A 'agent/1.0' -e 'http://ref.example.test/' -b 'sid=1; theme=dark' url`)
	// -u is base64("alice:secret") as Basic auth.
	if v, _ := req.HeaderGet("Authorization"); v != "Basic YWxpY2U6c2VjcmV0" {
		t.Errorf("authorization = %q", v)
	}
	if v, _ := req.HeaderGet("User-Agent"); v != "agent/1.0" {
		t.Errorf("user-agent = %q", v)
	}
	if v, _ := req.HeaderGet("Referer"); v != "http://ref.example.test/" {
		t.Errorf("referer = %q", v)
	}
	if v, _ := req.HeaderGet("Cookie"); v != "sid=1; theme=dark" {
		t.Errorf("cookie = %q", v)
	}
}

func TestParseHostHeaderBecomesExplicitHost(t *testing.T) {
	req := mustParse(t, `curl http://127.0.0.1:8080/ -H 'Host: virtual.example.test'`)
	if req.Host != "127.0.0.1:8080" {
		t.Errorf("connect host = %q", req.Host)
	}
	if req.ExplicitHost != "virtual.example.test" {
		t.Errorf("explicit host = %q", req.ExplicitHost)
	}
}

func TestParseJSONFlagSetsHeadersAndBody(t *testing.T) {
	req := mustParse(t, `curl --json '{"a":1}' http://example.test/`)
	if string(req.Body) != `{"a":1}` || req.Method != "POST" {
		t.Fatalf("body/method = %q/%s", req.Body, req.Method)
	}
	ct, _ := req.HeaderGet("Content-Type")
	ac, _ := req.HeaderGet("Accept")
	if ct != "application/json" || ac != "application/json" {
		t.Errorf("headers = %q / %q, want json defaults", ct, ac)
	}
}

func TestParseIgnorableTransferFlagsWarnButSucceed(t *testing.T) {
	req, warns, err := Parse(`curl -sS --compressed -o /dev/null --max-time 5 http://example.test/`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(warns) < 2 {
		t.Errorf("want warnings for ignored flags, got %v", warns)
	}
	// --compressed changes the wire request, so it must materialize.
	if v, _ := req.HeaderGet("Accept-Encoding"); !strings.Contains(v, "gzip") {
		t.Errorf("accept-encoding = %q, want gzip from --compressed", v)
	}
}

func TestParseErrorCases(t *testing.T) {
	cases := map[string]string{
		"unknown flag":     `curl --frobnicate url`,
		"missing value":    `curl url -H`,
		"no URL":           `curl -s`,
		"two URLs":         `curl http://a.example.test http://b.example.test`,
		"multipart":        `curl -F 'f=@x' url`,
		"data from file":   `curl -d @payload.txt url`,
		"cookie jar file":  `curl -b cookies.txt url`,
		"malformed header": `curl -H 'NoColonHere' url`,
		"bad scheme":       `curl ftp://example.test/`,
		"not curl at all":  `wget http://example.test/`,
	}
	for name, cmd := range cases {
		if _, _, err := Parse(cmd); err == nil {
			t.Errorf("%s: Parse(%q) succeeded, want error", name, cmd)
		}
	}
}

func TestRenderRoundTripsThroughParse(t *testing.T) {
	orig := mustParse(t, `curl 'http://example.test/p?a=1&b=2' -X PATCH -H 'X-Token: v w' -H 'Cookie: s=1' --data-raw '{"k":"it'\''s"}'`)
	rendered := Render(orig)
	back := mustParse(t, rendered)
	if !reflect.DeepEqual(orig, back) {
		t.Fatalf("round trip mismatch:\n orig: %+v\n back: %+v\n cmd:  %s", orig, back, rendered)
	}
}

func TestRenderOmitsRedundantMethod(t *testing.T) {
	get := Render(mustParse(t, `curl http://example.test/`))
	if strings.Contains(get, "-X") {
		t.Errorf("GET should not render -X: %s", get)
	}
	post := Render(mustParse(t, `curl http://example.test/ -d a=1`))
	if strings.Contains(post, "-X") {
		t.Errorf("POST with body should not render -X: %s", post)
	}
	del := Render(mustParse(t, `curl -X DELETE http://example.test/x`))
	if !strings.Contains(del, "-X DELETE") {
		t.Errorf("DELETE must render -X: %s", del)
	}
}

func TestQuoteIsShellSafe(t *testing.T) {
	if q := Quote("it's"); q != `'it'\''s'` {
		t.Errorf("Quote = %s", q)
	}
	if Quote("plainword") != "plainword" {
		t.Errorf("plain words should not be quoted")
	}
	if Quote("") != "''" {
		t.Errorf("empty string must render as ''")
	}
}
