// Package items turns a request into the flat list of independently
// removable atoms that delta debugging searches over: individual headers,
// query parameters, cookies, form fields, JSON body keys, or the whole raw
// body. A Plan knows how to materialize any keep/drop subset back into a
// well-formed request without re-encoding the surviving parts.
package items

import (
	"fmt"
	"path"
	"strings"

	"github.com/JaydenCJ/reqmin/internal/jsonwalk"
	"github.com/JaydenCJ/reqmin/internal/request"
)

// Kind classifies a removable atom.
type Kind string

// The item kinds, in report order.
const (
	KindHeader Kind = "header"
	KindQuery  Kind = "query"
	KindCookie Kind = "cookie"
	KindForm   Kind = "form"
	KindJSON   Kind = "json"
	KindBody   Kind = "body"
)

// AllKinds lists every kind in canonical order.
var AllKinds = []Kind{KindHeader, KindQuery, KindCookie, KindForm, KindJSON, KindBody}

// Item is one removable atom of the request.
type Item struct {
	Kind   Kind
	Name   string // display name: header name, decoded param key, dotted JSON path
	Forced bool   // matched a --keep pattern; never offered for removal

	idx  int      // index into the kind's backing slice
	path []string // KindJSON only
}

type bodyKind int

const (
	bodyNone bodyKind = iota
	bodyForm
	bodyJSON
	bodyRaw
)

// Options controls which atoms become items.
type Options struct {
	// Only, when non-nil, restricts enumeration to these kinds; everything
	// else is left untouched in every candidate.
	Only map[Kind]bool
	// Keep holds case-insensitive glob patterns; matching items are pinned.
	// A pattern matches the item name ("Authorization", "user.id") or the
	// kind-qualified form ("header:X-*").
	Keep []string
}

// Plan binds a base request to its enumerated items.
type Plan struct {
	base  *request.Request
	Items []Item

	headerItem map[int]int // base.Headers index -> Items index

	cookieParts []string // raw name=value pieces from all Cookie headers
	cookiePos   int      // index of the first Cookie header, -1 if none

	queryItem map[int]int // base.Query index -> Items index

	formPairs []request.Pair
	jsonRoot  *jsonwalk.Value
	body      bodyKind
}

// New enumerates the removable items of req.
func New(req *request.Request, opts Options) (*Plan, error) {
	for _, pat := range opts.Keep {
		if _, err := path.Match(pat, "probe"); err != nil {
			return nil, fmt.Errorf("invalid --keep pattern %q: %w", pat, err)
		}
	}
	p := &Plan{
		base:       req,
		headerItem: map[int]int{},
		queryItem:  map[int]int{},
		cookiePos:  -1,
	}
	want := func(k Kind) bool { return opts.Only == nil || opts.Only[k] }

	// Headers (Cookie headers become cookie items instead).
	for i, h := range req.Headers {
		if strings.EqualFold(h.Name, "Cookie") {
			if p.cookiePos < 0 {
				p.cookiePos = i
			}
			if want(KindCookie) {
				for _, part := range request.ParseCookies(h.Value) {
					p.cookieParts = append(p.cookieParts, part)
					p.add(Item{Kind: KindCookie, Name: request.CookieName(part), idx: len(p.cookieParts) - 1}, opts)
				}
			}
			continue
		}
		if want(KindHeader) {
			p.headerItem[i] = len(p.Items)
			p.add(Item{Kind: KindHeader, Name: h.Name, idx: i}, opts)
		}
	}

	// Query parameters.
	if want(KindQuery) {
		for i, pr := range req.Query {
			p.queryItem[i] = len(p.Items)
			p.add(Item{Kind: KindQuery, Name: pr.Key(), idx: i}, opts)
		}
	}

	// Body.
	p.classifyBody()
	switch p.body {
	case bodyForm:
		if want(KindForm) {
			p.formPairs = request.ParsePairs(string(req.Body))
			for i, pr := range p.formPairs {
				p.add(Item{Kind: KindForm, Name: pr.Key(), idx: i}, opts)
			}
		} else {
			p.body = bodyNone
		}
	case bodyJSON:
		if want(KindJSON) {
			for _, jp := range p.jsonRoot.ObjectPaths() {
				p.add(Item{Kind: KindJSON, Name: jsonwalk.PathString(jp), path: jp}, opts)
			}
		} else {
			p.body = bodyNone
		}
	case bodyRaw:
		if want(KindBody) {
			p.add(Item{Kind: KindBody, Name: "entire body"}, opts)
		} else {
			p.body = bodyNone
		}
	}
	return p, nil
}

func (p *Plan) add(it Item, opts Options) {
	it.Forced = matchesAny(opts.Keep, it)
	p.Items = append(p.Items, it)
}

func matchesAny(patterns []string, it Item) bool {
	name := strings.ToLower(it.Name)
	qualified := string(it.Kind) + ":" + name
	for _, pat := range patterns {
		pat = strings.ToLower(pat)
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
		if ok, _ := path.Match(pat, qualified); ok {
			return true
		}
	}
	return false
}

// classifyBody decides how the body decomposes into items. Form bodies need
// the explicit content type; JSON is accepted by content type or by a
// successful parse of a leading '{' — but only objects decompose.
func (p *Plan) classifyBody() {
	if len(p.base.Body) == 0 {
		p.body = bodyNone
		return
	}
	ct, _ := p.base.HeaderGet("Content-Type")
	ct = strings.ToLower(ct)
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		p.body = bodyForm
		return
	}
	looksJSON := strings.Contains(ct, "json") ||
		(ct == "" && strings.HasPrefix(strings.TrimSpace(string(p.base.Body)), "{"))
	if looksJSON {
		if root, err := jsonwalk.Parse(p.base.Body); err == nil && root.Kind == jsonwalk.Object {
			p.jsonRoot = root
			p.body = bodyJSON
			return
		}
	}
	p.body = bodyRaw
}

// Minimizable returns the indices of items the search may remove
// (everything not pinned by --keep).
func (p *Plan) Minimizable() []int {
	var out []int
	for i, it := range p.Items {
		if !it.Forced {
			out = append(out, i)
		}
	}
	return out
}

// Counts tallies items per kind, for reports.
func (p *Plan) Counts() map[Kind]int {
	out := map[Kind]int{}
	for _, it := range p.Items {
		out[it.Kind]++
	}
	return out
}

// Materialize builds the candidate request that keeps exactly the items
// where keep[i] (or Forced) is true. The base request is never mutated.
func (p *Plan) Materialize(keep []bool) *request.Request {
	kept := func(i int) bool { return p.Items[i].Forced || keep[i] }

	out := p.base.Clone()
	out.Headers = nil
	out.Query = nil
	out.Body = nil

	// Headers, with the rebuilt Cookie header at its original position.
	for i, h := range p.base.Headers {
		if strings.EqualFold(h.Name, "Cookie") {
			if i == p.cookiePos {
				if v := p.cookieHeader(kept); v != "" {
					out.Headers = append(out.Headers, request.Header{Name: "Cookie", Value: v})
				}
			}
			continue
		}
		if itemIdx, ok := p.headerItem[i]; ok && !kept(itemIdx) {
			continue
		}
		out.Headers = append(out.Headers, h)
	}

	// Query.
	for i, pr := range p.base.Query {
		if itemIdx, ok := p.queryItem[i]; ok && !kept(itemIdx) {
			continue
		}
		out.Query = append(out.Query, pr)
	}

	// Body.
	switch p.body {
	case bodyNone:
		out.Body = append([]byte(nil), p.base.Body...)
	case bodyForm:
		var pairs []request.Pair
		for i := range p.Items {
			if p.Items[i].Kind == KindForm && kept(i) {
				pairs = append(pairs, p.formPairs[p.Items[i].idx])
			}
		}
		if enc := request.EncodePairs(pairs); enc != "" {
			out.Body = []byte(enc)
		}
	case bodyJSON:
		root := p.jsonRoot.Clone()
		for i := range p.Items {
			if p.Items[i].Kind == KindJSON && !kept(i) {
				root.Remove(p.Items[i].path)
			}
		}
		out.Body = root.Encode()
	case bodyRaw:
		for i := range p.Items {
			if p.Items[i].Kind == KindBody && kept(i) {
				out.Body = append([]byte(nil), p.base.Body...)
			}
		}
	}
	return out
}

func (p *Plan) cookieHeader(kept func(int) bool) string {
	var parts []string
	for i := range p.Items {
		if p.Items[i].Kind == KindCookie && kept(i) {
			parts = append(parts, p.cookieParts[p.Items[i].idx])
		}
	}
	return strings.Join(parts, "; ")
}

// ParseOnly parses a comma-separated kind list ("headers,query") into an
// Only set. Both singular and plural spellings are accepted.
func ParseOnly(spec string) (map[Kind]bool, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	aliases := map[string]Kind{
		"header": KindHeader, "headers": KindHeader,
		"query": KindQuery, "params": KindQuery,
		"cookie": KindCookie, "cookies": KindCookie,
		"form": KindForm,
		"json": KindJSON,
		"body": KindBody,
	}
	out := map[Kind]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		k, ok := aliases[part]
		if !ok {
			return nil, fmt.Errorf("unknown item kind %q (want headers, query, cookies, form, json, or body)", part)
		}
		out[k] = true
	}
	return out, nil
}
