// Rendering a request back into a copy-pasteable curl one-liner.
package curl

import (
	"strings"

	"github.com/JaydenCJ/reqmin/internal/request"
)

// Render turns a request into a single-line curl command that reproduces it
// exactly: explicit method only when it differs from what curl would infer,
// headers in original order, the body via --data-raw.
func Render(r *request.Request) string {
	parts := []string{"curl", Quote(r.URL())}
	implied := "GET"
	if len(r.Body) > 0 {
		implied = "POST"
	}
	if r.Method != implied {
		parts = append(parts, "-X", r.Method)
	}
	if r.ExplicitHost != "" {
		parts = append(parts, "-H", Quote("Host: "+r.ExplicitHost))
	}
	for _, h := range r.Headers {
		parts = append(parts, "-H", Quote(h.Name+": "+h.Value))
	}
	if len(r.Body) > 0 {
		parts = append(parts, "--data-raw", Quote(string(r.Body)))
	}
	return strings.Join(parts, " ")
}

// Quote wraps s in single quotes, escaping embedded single quotes the
// POSIX way, so the output survives a paste into any Bourne-family shell.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`&|;<>()*?[]#~=%!{}") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
