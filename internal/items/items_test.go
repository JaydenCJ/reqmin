// Item enumeration and materialization tests: the Plan is the bridge
// between "a request" and "a set ddmin can search", so it must decompose
// every dimension correctly and rebuild any subset without re-encoding
// the surviving parts.
package items

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/reqmin/internal/curl"
	"github.com/JaydenCJ/reqmin/internal/request"
)

func planFor(t *testing.T, cmd string, opts Options) *Plan {
	t.Helper()
	req, _, err := curl.Parse(cmd)
	if err != nil {
		t.Fatalf("curl.Parse: %v", err)
	}
	p, err := New(req, opts)
	if err != nil {
		t.Fatalf("items.New: %v", err)
	}
	return p
}

func names(p *Plan, kind Kind) []string {
	var out []string
	for _, it := range p.Items {
		if it.Kind == kind {
			out = append(out, it.Name)
		}
	}
	return out
}

// keepAllBut returns a keep mask dropping the items whose kind+name match.
func keepAllBut(p *Plan, kind Kind, dropNames ...string) []bool {
	mask := make([]bool, len(p.Items))
	for i, it := range p.Items {
		mask[i] = true
		for _, dn := range dropNames {
			if it.Kind == kind && it.Name == dn {
				mask[i] = false
			}
		}
	}
	return mask
}

func TestEnumerateHeadersQueryAndCookies(t *testing.T) {
	p := planFor(t, `curl 'http://example.test/p?a=1&b=2' -H 'X-One: 1' -H 'Cookie: sid=s; theme=dark' -H 'X-Two: 2'`, Options{})
	if got := names(p, KindHeader); strings.Join(got, ",") != "X-One,X-Two" {
		t.Errorf("headers = %v", got)
	}
	if got := names(p, KindQuery); strings.Join(got, ",") != "a,b" {
		t.Errorf("query = %v", got)
	}
	if got := names(p, KindCookie); strings.Join(got, ",") != "sid,theme" {
		t.Errorf("cookies = %v", got)
	}
}

func TestFormBodyDecomposesIntoFields(t *testing.T) {
	p := planFor(t, `curl http://example.test/ -d 'user=a&pass=b&csrf=t'`, Options{})
	if got := names(p, KindForm); strings.Join(got, ",") != "user,pass,csrf" {
		t.Errorf("form fields = %v", got)
	}
}

func TestJSONBodyDecomposesIntoNestedKeys(t *testing.T) {
	p := planFor(t, `curl http://example.test/ -H 'Content-Type: application/json' --data-raw '{"user":{"id":1,"nick":"a"},"debug":true}'`, Options{})
	got := strings.Join(names(p, KindJSON), ",")
	if got != "user,user.id,user.nick,debug" {
		t.Errorf("json keys = %q", got)
	}
}

func TestOpaqueBodyIsOneItem(t *testing.T) {
	p := planFor(t, `curl http://example.test/ -H 'Content-Type: text/plain' --data-raw 'just some text'`, Options{})
	if got := names(p, KindBody); len(got) != 1 {
		t.Errorf("body items = %v, want exactly one", got)
	}
	// Invalid JSON behind a JSON content type falls back to the same
	// all-or-nothing treatment instead of erroring out.
	p = planFor(t, `curl http://example.test/ -H 'Content-Type: application/json' --data-raw '{broken'`, Options{})
	if len(names(p, KindJSON)) != 0 || len(names(p, KindBody)) != 1 {
		t.Errorf("want raw-body fallback, got %+v", p.Items)
	}
}

func TestMaterializeDropHeaderKeepsOrderOfRest(t *testing.T) {
	p := planFor(t, `curl http://example.test/ -H 'A: 1' -H 'B: 2' -H 'C: 3'`, Options{})
	out := p.Materialize(keepAllBut(p, KindHeader, "B"))
	if len(out.Headers) != 2 || out.Headers[0].Name != "A" || out.Headers[1].Name != "C" {
		t.Fatalf("headers = %+v", out.Headers)
	}
}

func TestMaterializeDropQueryDoesNotReencodeSurvivors(t *testing.T) {
	// %2Fpath must stay %2Fpath — re-encoding could change server behavior.
	p := planFor(t, `curl 'http://example.test/?redirect=%2Fadmin%2F&junk=1&flag'`, Options{})
	out := p.Materialize(keepAllBut(p, KindQuery, "junk"))
	if got := out.URL(); got != "http://example.test/?redirect=%2Fadmin%2F&flag" {
		t.Fatalf("URL = %q", got)
	}
}

func TestMaterializeCookieSubsetRebuildsSingleHeader(t *testing.T) {
	p := planFor(t, `curl http://example.test/ -H 'A: 1' -H 'Cookie: sid=s; junk=j; theme=dark' -H 'B: 2'`, Options{})
	out := p.Materialize(keepAllBut(p, KindCookie, "junk"))
	if v, _ := out.HeaderGet("Cookie"); v != "sid=s; theme=dark" {
		t.Fatalf("cookie = %q", v)
	}
	// The rebuilt Cookie header must stay at its original position.
	if out.Headers[1].Name != "Cookie" {
		t.Fatalf("cookie header moved: %+v", out.Headers)
	}
	// Dropping every cookie removes the header entirely.
	out = p.Materialize(keepAllBut(p, KindCookie, "sid", "junk", "theme"))
	if _, ok := out.HeaderGet("Cookie"); ok {
		t.Fatal("empty Cookie header must disappear")
	}
}

func TestMaterializeJSONSubset(t *testing.T) {
	p := planFor(t, `curl http://example.test/ -H 'Content-Type: application/json' --data-raw '{"keep":1,"user":{"id":7,"junk":true},"drop":0}'`, Options{})
	out := p.Materialize(keepAllBut(p, KindJSON, "drop", "user.junk"))
	if got := string(out.Body); got != `{"keep":1,"user":{"id":7}}` {
		t.Fatalf("body = %s", got)
	}
}

func TestMaterializeNeverMutatesBase(t *testing.T) {
	req, _, err := curl.Parse(`curl 'http://example.test/?a=1' -H 'X: 1' -H 'Cookie: s=1' -d 'f=v'`)
	if err != nil {
		t.Fatalf("curl.Parse: %v", err)
	}
	before := curl.Render(req)
	p, err := New(req, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.Materialize(make([]bool, len(p.Items))) // drop everything
	if after := curl.Render(req); after != before {
		t.Fatalf("base request mutated:\n before: %s\n after:  %s", before, after)
	}
}

func TestKeepPatternsPinItems(t *testing.T) {
	p := planFor(t, `curl 'http://example.test/?token=1' -H 'Authorization: b' -H 'X-Trace: t'`,
		Options{Keep: []string{"authorization", "query:tok*"}})
	pinned := map[string]bool{}
	for _, it := range p.Items {
		if it.Forced {
			pinned[string(it.Kind)+":"+it.Name] = true
		}
	}
	if !pinned["header:Authorization"] || !pinned["query:token"] {
		t.Fatalf("pinned = %v", pinned)
	}
	// Pinned items are excluded from the search space...
	if len(p.Minimizable()) != 1 {
		t.Fatalf("minimizable = %v", p.Minimizable())
	}
	// ...and survive materialization even when the mask says drop.
	out := p.Materialize(make([]bool, len(p.Items)))
	if _, ok := out.HeaderGet("Authorization"); !ok {
		t.Fatal("pinned header was dropped")
	}
}

func TestOnlyRestrictsEnumeration(t *testing.T) {
	only, err := ParseOnly("headers,cookies")
	if err != nil {
		t.Fatalf("ParseOnly: %v", err)
	}
	p := planFor(t, `curl 'http://example.test/?a=1' -H 'X: 1' -H 'Cookie: s=1' -d 'f=v'`, Options{Only: only})
	counts := p.Counts()
	if counts[KindHeader] == 0 || counts[KindCookie] == 0 {
		t.Errorf("counts = %v, want headers and cookies present", counts)
	}
	if counts[KindQuery] != 0 || counts[KindForm] != 0 {
		t.Errorf("counts = %v, want query/form excluded", counts)
	}
	// Excluded dimensions must pass through every candidate untouched.
	out := p.Materialize(make([]bool, len(p.Items)))
	if got := out.URL(); !strings.Contains(got, "a=1") {
		t.Errorf("query dropped despite --only: %q", got)
	}
	if string(out.Body) != "f=v" {
		t.Errorf("body dropped despite --only: %q", out.Body)
	}
}

func TestBadOptionsAreErrors(t *testing.T) {
	if _, err := ParseOnly("headers,frobs"); err == nil {
		t.Fatal("want error for unknown --only kind")
	}
	req := &request.Request{Method: "GET", Scheme: "http", Host: "example.test", Path: "/"}
	if _, err := New(req, Options{Keep: []string{"[unclosed"}}); err == nil {
		t.Fatal("want error for malformed --keep glob")
	}
}
