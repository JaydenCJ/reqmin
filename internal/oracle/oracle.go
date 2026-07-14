// Package oracle decides whether a response "still reproduces". Predicates
// come from CLI flags and are ANDed together; when the user gives none, the
// oracle binds itself to the baseline response's status code, so the default
// question is "does the server still answer the same way?".
package oracle

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Config carries the raw predicate flags before compilation.
type Config struct {
	Status         int      // expected status; 0 = unset
	BodyContains   []string // every string must appear in the body
	BodyRegex      string   // RE2 pattern the body must match
	HeaderContains []string // "Name: substring" or bare "Name" (presence)
}

type headerPred struct {
	name string
	sub  string
}

// Oracle is a compiled, immutable-after-bind predicate set.
type Oracle struct {
	status   int
	contains []string
	re       *regexp.Regexp
	reSrc    string
	headers  []headerPred
	explicit bool // user supplied at least one predicate
}

// New compiles a config, validating the regex and header specs up front.
func New(c Config) (*Oracle, error) {
	o := &Oracle{
		status:   c.Status,
		contains: c.BodyContains,
		reSrc:    c.BodyRegex,
	}
	if c.BodyRegex != "" {
		re, err := regexp.Compile(c.BodyRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid --expect-body-regex: %w", err)
		}
		o.re = re
	}
	for _, spec := range c.HeaderContains {
		name, sub, _ := strings.Cut(spec, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("invalid --expect-header %q (want 'Name: substring' or 'Name')", spec)
		}
		o.headers = append(o.headers, headerPred{name: name, sub: strings.TrimSpace(sub)})
	}
	o.explicit = c.Status != 0 || len(c.BodyContains) > 0 || c.BodyRegex != "" || len(c.HeaderContains) > 0
	return o, nil
}

// BindBaseline anchors an empty oracle to the baseline status code. It is a
// no-op when the user supplied explicit predicates.
func (o *Oracle) BindBaseline(status int) {
	if !o.explicit {
		o.status = status
	}
}

// Check evaluates every predicate against a response. On failure it names
// the first predicate that did not hold, for diagnostics.
func (o *Oracle) Check(status int, header http.Header, body []byte) (bool, string) {
	if o.status != 0 && status != o.status {
		return false, fmt.Sprintf("status is %d, want %d", status, o.status)
	}
	for _, s := range o.contains {
		if !strings.Contains(string(body), s) {
			return false, fmt.Sprintf("body does not contain %q", s)
		}
	}
	if o.re != nil && !o.re.Match(body) {
		return false, fmt.Sprintf("body does not match /%s/", o.reSrc)
	}
	for _, hp := range o.headers {
		got := header.Values(hp.name)
		if len(got) == 0 {
			return false, fmt.Sprintf("response header %s is absent", hp.name)
		}
		if hp.sub != "" && !anyContains(got, hp.sub) {
			return false, fmt.Sprintf("response header %s does not contain %q", hp.name, hp.sub)
		}
	}
	return true, ""
}

// Describe renders the predicate set for reports.
func (o *Oracle) Describe() string {
	var parts []string
	if o.status != 0 {
		parts = append(parts, fmt.Sprintf("status == %d", o.status))
	}
	for _, s := range o.contains {
		parts = append(parts, fmt.Sprintf("body contains %q", s))
	}
	if o.re != nil {
		parts = append(parts, fmt.Sprintf("body matches /%s/", o.reSrc))
	}
	for _, hp := range o.headers {
		if hp.sub == "" {
			parts = append(parts, fmt.Sprintf("header %s present", hp.name))
		} else {
			parts = append(parts, fmt.Sprintf("header %s contains %q", hp.name, hp.sub))
		}
	}
	if len(parts) == 0 {
		return "(unbound)"
	}
	return strings.Join(parts, " && ")
}

func anyContains(values []string, sub string) bool {
	for _, v := range values {
		if strings.Contains(v, sub) {
			return true
		}
	}
	return false
}
