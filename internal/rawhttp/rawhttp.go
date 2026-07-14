// Package rawhttp parses and renders requests in plain HTTP/1.1 message
// syntax — the format proxies, .http files, and intercepting tools speak.
//
//	POST /search?q=x HTTP/1.1
//	Host: api.example.test
//	Authorization: Bearer …
//
//	q=x&page=2
//
// Both origin-form ("/path", scheme supplied by the caller) and
// absolute-form ("http://host/path") request targets are accepted.
package rawhttp

import (
	"fmt"
	"strings"

	"github.com/JaydenCJ/reqmin/internal/request"
)

// Parse reads one HTTP request message. scheme ("http" or "https") is used
// when the request target is origin-form; absolute-form targets carry their
// own scheme and win.
func Parse(text string, scheme string) (*request.Request, error) {
	if scheme == "" {
		scheme = "http"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	head, body, hasBody := strings.Cut(text, "\n\n")
	lines := strings.Split(strings.TrimRight(head, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, fmt.Errorf("empty request")
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 2 || len(fields) > 3 {
		return nil, fmt.Errorf("malformed request line %q (want 'METHOD target [HTTP/x.y]')", lines[0])
	}
	req := &request.Request{Method: strings.ToUpper(fields[0]), Proto: "HTTP/1.1"}
	if len(fields) == 3 {
		if !strings.HasPrefix(fields[2], "HTTP/") {
			return nil, fmt.Errorf("malformed protocol %q in request line", fields[2])
		}
		req.Proto = fields[2]
	}
	target := fields[1]

	var host string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return nil, fmt.Errorf("obsolete header line folding is not supported: %q", line)
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("malformed header line %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		switch strings.ToLower(name) {
		case "host":
			host = value
		case "content-length":
			// recomputed at send time
		default:
			req.Headers = append(req.Headers, request.Header{Name: name, Value: value})
		}
	}

	switch {
	case strings.Contains(target, "://"): // absolute-form
		if err := req.SetURL(target); err != nil {
			return nil, err
		}
		if host != "" && !strings.EqualFold(host, req.Host) {
			req.ExplicitHost = host
		}
	case strings.HasPrefix(target, "/"): // origin-form
		if host == "" {
			return nil, fmt.Errorf("origin-form target %q needs a Host header", target)
		}
		if err := req.SetURL(scheme + "://" + host + target); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported request target %q", target)
	}

	if hasBody {
		// A single trailing newline is almost always the file's final
		// newline, not payload; anything beyond that is kept verbatim.
		body = strings.TrimSuffix(body, "\n")
		if body != "" {
			req.Body = []byte(body)
		}
	}
	return req, nil
}

// Render writes the request back in HTTP/1.1 message syntax with a Host
// header first and Content-Length recomputed.
func Render(r *request.Request) string {
	var b strings.Builder
	target := r.Path
	if q := request.EncodePairs(r.Query); q != "" {
		target += "?" + q
	}
	fmt.Fprintf(&b, "%s %s %s\n", r.Method, target, r.Proto)
	host := r.Host
	if r.ExplicitHost != "" {
		host = r.ExplicitHost
	}
	fmt.Fprintf(&b, "Host: %s\n", host)
	for _, h := range r.Headers {
		fmt.Fprintf(&b, "%s: %s\n", h.Name, h.Value)
	}
	if len(r.Body) > 0 {
		fmt.Fprintf(&b, "Content-Length: %d\n", len(r.Body))
	}
	b.WriteString("\n")
	b.Write(r.Body)
	return b.String()
}
