// Oracle tests: predicate compilation, AND semantics, baseline binding,
// and the diagnostics users see when a candidate stops reproducing.
package oracle

import (
	"net/http"
	"strings"
	"testing"
)

func TestDefaultOracleBindsToBaselineStatus(t *testing.T) {
	o, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	o.BindBaseline(503)
	if ok, _ := o.Check(503, nil, nil); !ok {
		t.Error("bound status 503 must pass")
	}
	if ok, why := o.Check(200, nil, nil); ok || !strings.Contains(why, "503") {
		t.Errorf("ok=%v why=%q, want status mismatch naming 503", ok, why)
	}
}

func TestBindBaselineDoesNotOverrideExplicitPredicates(t *testing.T) {
	o, err := New(Config{Status: 401})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	o.BindBaseline(200) // must be a no-op
	if ok, _ := o.Check(401, nil, nil); !ok {
		t.Error("explicit --expect-status 401 was clobbered by the baseline")
	}
}

func TestBodyPredicates(t *testing.T) {
	// Every --expect-body-contains must hold, not just one.
	o, _ := New(Config{BodyContains: []string{"alpha", "beta"}})
	if ok, _ := o.Check(200, nil, []byte("alpha and beta")); !ok {
		t.Error("both substrings present, want pass")
	}
	if ok, why := o.Check(200, nil, []byte("alpha only")); ok || !strings.Contains(why, "beta") {
		t.Errorf("ok=%v why=%q, want failure naming the missing substring", ok, why)
	}
	// --expect-body-regex is an unanchored RE2 search.
	o, err := New(Config{BodyRegex: `"error":\s*"rate.?limited"`})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ok, _ := o.Check(200, nil, []byte(`{"error": "rate-limited"}`)); !ok {
		t.Error("matching body must pass")
	}
	if ok, _ := o.Check(200, nil, []byte(`{"error": "other"}`)); ok {
		t.Error("non-matching body must fail")
	}
}

func TestPredicateCompileErrors(t *testing.T) {
	if _, err := New(Config{BodyRegex: "("}); err == nil {
		t.Error("want error for invalid regex")
	}
	if _, err := New(Config{HeaderContains: []string{": no name"}}); err == nil {
		t.Error("want error for header spec without a name")
	}
}

func TestHeaderPredicatesPresenceAndSubstring(t *testing.T) {
	o, err := New(Config{HeaderContains: []string{"X-Cache", "Content-Type: json"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := http.Header{}
	h.Set("X-Cache", "HIT")
	h.Set("Content-Type", "application/json; charset=utf-8")
	if ok, why := o.Check(200, h, nil); !ok {
		t.Errorf("want pass, got %q", why)
	}
	h.Del("X-Cache")
	if ok, why := o.Check(200, h, nil); ok || !strings.Contains(why, "absent") {
		t.Errorf("ok=%v why=%q, want absence failure", ok, why)
	}
}

func TestPredicatesAreANDed(t *testing.T) {
	o, _ := New(Config{Status: 200, BodyContains: []string{"ok"}})
	if ok, _ := o.Check(200, nil, []byte("ok")); !ok {
		t.Error("all predicates hold, want pass")
	}
	if ok, _ := o.Check(500, nil, []byte("ok")); ok {
		t.Error("status predicate failed, want overall failure")
	}
	if ok, _ := o.Check(200, nil, []byte("nope")); ok {
		t.Error("body predicate failed, want overall failure")
	}
}

func TestDescribeNamesEveryPredicate(t *testing.T) {
	o, _ := New(Config{Status: 200, BodyContains: []string{"x"}, BodyRegex: "y+", HeaderContains: []string{"K: v"}})
	d := o.Describe()
	for _, want := range []string{"status == 200", `body contains "x"`, "body matches /y+/", `header K contains "v"`} {
		if !strings.Contains(d, want) {
			t.Errorf("Describe() = %q, missing %q", d, want)
		}
	}
}
