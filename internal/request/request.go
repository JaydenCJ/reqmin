// Package request holds the canonical HTTP request model shared by every
// reqmin component. It is deliberately dumber than net/http.Request: headers
// keep their original order and duplicates, query and form pairs keep their
// exact original encoding (never re-encoded), and nothing here performs I/O.
// That losslessness is what lets a minimized request stay byte-comparable to
// the request the user actually captured.
package request

import (
	"fmt"
	"net/url"
	"strings"
)

// Header is one header line, order- and duplicate-preserving.
type Header struct {
	Name  string
	Value string
}

// Pair is one raw key[=value] segment of a query string, form body, or
// Cookie header. Raw is kept verbatim (still percent-encoded) so that
// dropping neighbors never re-encodes the survivors.
type Pair struct {
	Raw string
}

// Key returns the decoded key portion of the pair, for display. If the key
// is not valid percent-encoding it is returned as-is.
func (p Pair) Key() string {
	k := p.Raw
	if i := strings.Index(k, "="); i >= 0 {
		k = k[:i]
	}
	if d, err := url.QueryUnescape(k); err == nil {
		return d
	}
	return k
}

// Request is a mutable, order-preserving HTTP request.
type Request struct {
	Method string
	Scheme string // "http" or "https"
	Host   string // authority actually connected to (may include :port)
	Path   string // escaped path, always starts with "/"
	Query  []Pair // raw query segments in original order
	Proto  string // e.g. "HTTP/1.1"; informational only

	// Headers excludes Host and Content-Length: the authority lives in
	// Host/ExplicitHost and Content-Length is recomputed at send time.
	Headers []Header

	// ExplicitHost, when non-empty, is sent as the Host header instead of
	// the authority (the `curl -H 'Host: …'` override).
	ExplicitHost string

	Body []byte
}

// SetURL parses an absolute http(s) URL into Scheme/Host/Path/Query.
func (r *Request) SetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q (want http or https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL %q has no host", raw)
	}
	r.Scheme = u.Scheme
	r.Host = u.Host
	r.Path = u.EscapedPath()
	if r.Path == "" {
		r.Path = "/"
	}
	r.Query = ParsePairs(u.RawQuery)
	return nil
}

// URL rebuilds the absolute URL from its parts.
func (r *Request) URL() string {
	var b strings.Builder
	b.WriteString(r.Scheme)
	b.WriteString("://")
	b.WriteString(r.Host)
	b.WriteString(r.Path)
	if q := EncodePairs(r.Query); q != "" {
		b.WriteString("?")
		b.WriteString(q)
	}
	return b.String()
}

// HeaderGet returns the first header value matching name (case-insensitive).
func (r *Request) HeaderGet(name string) (string, bool) {
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

// Clone returns a deep copy of the request.
func (r *Request) Clone() *Request {
	c := *r
	c.Query = append([]Pair(nil), r.Query...)
	c.Headers = append([]Header(nil), r.Headers...)
	c.Body = append([]byte(nil), r.Body...)
	return &c
}

// ParsePairs splits a raw query or form string on "&" into pairs, skipping
// empty segments (a lone "&&" contributes nothing to a request).
func ParsePairs(raw string) []Pair {
	if raw == "" {
		return nil
	}
	var out []Pair
	for _, seg := range strings.Split(raw, "&") {
		if seg == "" {
			continue
		}
		out = append(out, Pair{Raw: seg})
	}
	return out
}

// EncodePairs joins pairs back with "&", byte-identical to the surviving
// original segments.
func EncodePairs(pairs []Pair) string {
	segs := make([]string, len(pairs))
	for i, p := range pairs {
		segs[i] = p.Raw
	}
	return strings.Join(segs, "&")
}

// ParseCookies splits a Cookie header value on ";" into raw name=value
// pieces, trimming the conventional space after each semicolon.
func ParseCookies(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// CookieName returns the name portion of a raw "name=value" cookie piece.
func CookieName(raw string) string {
	if i := strings.Index(raw, "="); i >= 0 {
		return raw[:i]
	}
	return raw
}
