// End-to-end CLI tests: the full pipeline (parse -> plan -> baseline ->
// ddmin -> report) driven in-process against loopback httptest servers.
// No real network, no sleeps — the servers are deterministic functions of
// the request, so every run probes the same configurations.
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/reqmin/internal/runner"
)

// run invokes the CLI in-process and captures its streams.
func run(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, strings.NewReader(stdin), &out, &errb, runner.DefaultClient(0))
	return code, out.String(), errb.String()
}

// authServer reproduces the canonical pain: of everything "Copy as cURL"
// captured, only the bearer token and one query param actually matter.
func authServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer tok" && r.URL.Query().Get("user") == "42" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fatCurl builds a curl command with plenty of irrelevant baggage.
func fatCurl(url string) string {
	return "curl '" + url + "/api?user=42&session=abc&trace=1' " +
		"-H 'Authorization: Bearer tok' " +
		"-H 'Accept: application/json' " +
		"-H 'Accept-Language: en-US,en;q=0.9' " +
		"-H 'X-Requested-With: XMLHttpRequest' " +
		"-H 'Sec-Fetch-Mode: cors' " +
		"-H 'Referer: http://app.example.test/' " +
		"-H 'Cookie: sid=deadbeef; theme=dark; consent=yes' " +
		"-A 'Mozilla/5.0 (test)'"
}

func TestMinimizeKeepsOnlyWhatTheServerChecks(t *testing.T) {
	srv := authServer(t)
	code, stdout, stderr := run(t, "", fatCurl(srv.URL))
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "Authorization: Bearer tok") {
		t.Errorf("minimized command lost the token:\n%s", stdout)
	}
	if !strings.Contains(stdout, "user=42") {
		t.Errorf("minimized command lost the query param:\n%s", stdout)
	}
	for _, junk := range []string{"Accept-Language", "Sec-Fetch-Mode", "Referer", "Cookie", "Mozilla", "session=abc", "trace=1"} {
		if strings.Contains(stdout, junk) {
			t.Errorf("minimized command still carries %q:\n%s", junk, stdout)
		}
	}
	if !strings.Contains(stderr, "result: kept 2 of") {
		t.Errorf("report should count 2 kept items:\n%s", stderr)
	}
	// The same works with an unquoted, pre-tokenized curl argv.
	code, stdout, _ = run(t, "", "curl", srv.URL+"/api?user=42&junk=1", "-H", "Authorization: Bearer tok", "-H", "X-Noise: n")
	if code != ExitOK {
		t.Fatalf("unquoted argv exit = %d", code)
	}
	if strings.Contains(stdout, "X-Noise") || strings.Contains(stdout, "junk=1") {
		t.Errorf("noise survived unquoted argv:\n%s", stdout)
	}
}

func TestRawHTTPRequestOnStdin(t *testing.T) {
	srv := authServer(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	raw := "GET /api?user=42&junk=9 HTTP/1.1\n" +
		"Host: " + host + "\n" +
		"Authorization: Bearer tok\n" +
		"X-Extra: e\n"
	code, stdout, stderr := run(t, raw, "--format", "raw", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "GET /api?user=42 HTTP/1.1\n") {
		t.Errorf("raw output:\n%s", stdout)
	}
	if strings.Contains(stdout, "X-Extra") {
		t.Errorf("noise header survived:\n%s", stdout)
	}
}

func TestBodyContainsOracleShrinksJSONBody(t *testing.T) {
	// The bug: the server 500s (mentioning "boom") whenever "flag" is
	// true — every other JSON key is irrelevant to the crash.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil && payload["flag"] == true {
			w.WriteHeader(500)
			w.Write([]byte("internal error: boom"))
			return
		}
		w.Write([]byte("fine"))
	}))
	t.Cleanup(srv.Close)

	cmd := `curl ` + srv.URL + `/submit -H 'Content-Type: application/json' ` +
		`--data-raw '{"user":{"id":7,"nick":"a"},"flag":true,"trace":"xyz"}'`
	code, stdout, stderr := run(t, "", "--expect-status", "500", "--expect-body-contains", "boom", cmd)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, `{"flag":true}`) {
		t.Errorf("body not shrunk to the culprit key:\n%s", stdout)
	}
}

func TestBaselineNotReproducingExitsOne(t *testing.T) {
	srv := authServer(t)
	code, _, stderr := run(t, "", "--expect-status", "418", fatCurl(srv.URL))
	if code != ExitNoRepro {
		t.Fatalf("exit = %d, want %d", code, ExitNoRepro)
	}
	if !strings.Contains(stderr, "does not satisfy the oracle") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestKeepPinsItemsThroughMinimization(t *testing.T) {
	srv := authServer(t)
	code, stdout, _ := run(t, "", "--keep", "accept", fatCurl(srv.URL))
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "Accept: application/json") {
		t.Errorf("pinned Accept header was removed:\n%s", stdout)
	}
	if strings.Contains(stdout, "Accept-Language") {
		t.Errorf("--keep accept must not pin Accept-Language (glob is exact):\n%s", stdout)
	}
}

func TestOnlyRestrictsWhatIsMinimized(t *testing.T) {
	srv := authServer(t)
	code, stdout, _ := run(t, "", "--only", "headers", fatCurl(srv.URL))
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	// Query params were out of scope, so even the junk ones survive.
	if !strings.Contains(stdout, "session=abc") {
		t.Errorf("query should be untouched with --only headers:\n%s", stdout)
	}
	if strings.Contains(stdout, "Accept-Language") {
		t.Errorf("headers should still be minimized:\n%s", stdout)
	}
}

func TestJSONFormatReportsKeptAndRemoved(t *testing.T) {
	srv := authServer(t)
	code, stdout, _ := run(t, "", "--format", "json", fatCurl(srv.URL))
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var rep struct {
		Version        string `json:"version"`
		BaselineStatus int    `json:"baseline_status"`
		Kept           []struct{ Kind, Name string }
		Removed        []struct{ Kind, Name string }
		MinimalCurl    string `json:"minimal_curl"`
		RequestsSent   int    `json:"requests_sent"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if rep.BaselineStatus != 200 || len(rep.Kept) != 2 || len(rep.Removed) == 0 {
		t.Errorf("report = %+v", rep)
	}
	if rep.RequestsSent == 0 || !strings.Contains(rep.MinimalCurl, "Authorization") {
		t.Errorf("report = %+v", rep)
	}
}

func TestOutAndQuietRedirectEverything(t *testing.T) {
	srv := authServer(t)
	path := filepath.Join(t.TempDir(), "min.curl")
	code, stdout, stderr := run(t, "", "--out", path, "--quiet", fatCurl(srv.URL))
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty with --out: %q", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr should be empty with --quiet:\n%s", stderr)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(b), "Authorization: Bearer tok") {
		t.Errorf("file content:\n%s", b)
	}
}

func TestDryRunListsItemsWithoutAnyRequest(t *testing.T) {
	// The URL points nowhere routable from a test; dry-run must not care.
	code, stdout, _ := run(t, "", "--dry-run", fatCurl("http://127.0.0.1:1"))
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"Authorization", "sid", "user", "removable"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, stdout)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	code, stdout, _ := run(t, "", "--version")
	if code != ExitOK || stdout != "reqmin 0.1.0\n" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestHelpExitsZeroAndPrintsUsage(t *testing.T) {
	// Asking for help is not a usage error: -h/--help must exit 0.
	for _, flag := range []string{"-h", "--help"} {
		code, _, stderr := run(t, "", flag)
		if code != ExitOK {
			t.Errorf("%s: exit = %d, want %d", flag, code, ExitOK)
		}
		if !strings.Contains(stderr, "Usage:") || !strings.Contains(stderr, "--expect-status") {
			t.Errorf("%s: usage text missing:\n%s", flag, stderr)
		}
	}
}

func TestDryRunSummarySingularizesCounts(t *testing.T) {
	// One header and one query param must not read "1 headers".
	code, stdout, _ := run(t, "", "--dry-run",
		"curl 'http://127.0.0.1:1/?user=42' -H 'X-One: 1'")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "2 removable (1 header, 1 query param)") {
		t.Errorf("summary not singularized:\n%s", stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{}, // no input
		{"--format", "yaml", "curl", "http://example.test/"},      // bad format
		{"--only", "frobs", "curl http://example.test/"},          // bad kind
		{"no-such-file.http"},                                     // unreadable input
		{"--expect-body-regex", "(", "curl http://example.test/"}, // bad regex
		{"a.http", "b.http"},                                      // two inputs
	}
	for _, args := range cases {
		if code, _, _ := run(t, "", args...); code != ExitUsage {
			t.Errorf("args %v: exit = %d, want %d", args, code, ExitUsage)
		}
	}
}

func TestBaselineNetworkFailureExitsRuntime(t *testing.T) {
	// A closed port on loopback fails fast and deterministically.
	code, _, stderr := run(t, "", "curl http://127.0.0.1:1/")
	if code != ExitRuntime {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "baseline request failed") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestBudgetExhaustionStillEmitsAReducedRequest(t *testing.T) {
	srv := authServer(t)
	code, stdout, stderr := run(t, "", "--max-requests", "3", fatCurl(srv.URL))
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "request budget exhausted") {
		t.Errorf("missing budget warning:\n%s", stderr)
	}
	// Whatever came out must still carry the essentials.
	if !strings.Contains(stdout, "Authorization: Bearer tok") || !strings.Contains(stdout, "user=42") {
		t.Errorf("partial result broke the request:\n%s", stdout)
	}
}
