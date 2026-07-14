// Package curl converts between pasted "Copy as cURL" command lines and the
// reqmin request model. Parse understands the flag subset that browsers and
// API clients actually emit; Render turns a minimized request back into a
// copy-pasteable one-liner.
package curl

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/JaydenCJ/reqmin/internal/request"
)

// valueFlags consume the next argument. Value true means the flag
// contributes to the request; false means it is safely ignored with a
// warning (transfer options that cannot change what the server sees).
var valueFlags = map[string]bool{
	"-H": true, "--header": true,
	"-X": true, "--request": true,
	"-d": true, "--data": true, "--data-raw": true,
	"--data-binary": true, "--data-ascii": true,
	"--data-urlencode": true,
	"-b":               true, "--cookie": true,
	"-u": true, "--user": true,
	"-A": true, "--user-agent": true,
	"-e": true, "--referer": true,
	"--url":  true,
	"--json": true,
	"-r":     true, "--range": true,
	// ignored transfer options (still consume their value)
	"-o": false, "--output": false,
	"-m": false, "--max-time": false,
	"--connect-timeout": false,
	"--retry":           false,
	"-w":                false, "--write-out": false,
}

// boolFlags take no value. All current entries are transfer options that do
// not change the request on the wire (or that reqmin replaces), so they are
// ignored with a warning — except -G/--get, -I/--head and --compressed,
// which are handled explicitly.
var boolFlags = map[string]bool{
	"-s": true, "--silent": true,
	"-S": true, "--show-error": true,
	"-k": true, "--insecure": true,
	"-L": true, "--location": true,
	"-v": true, "--verbose": true,
	"-i": true, "--include": true,
	"-g": true, "--globoff": true,
	"-f": true, "--fail": true,
	"--no-progress-meter": true,
	"--http1.1":           true, "--http2": true,
}

// shortIgnorable lists single-letter flags that may appear combined
// (e.g. -sS) and are safe to drop.
const shortIgnorable = "sSkLvigf"

// Parse tokenizes a full command line ("curl https://… -H …") and builds a
// request. It returns the request, human-readable warnings for flags that
// were ignored, and an error for anything it cannot faithfully represent.
func Parse(command string) (*request.Request, []string, error) {
	tokens, err := SplitCommand(command)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot tokenize command: %w", err)
	}
	return ParseTokens(tokens)
}

// ParseTokens builds a request from an already-split argv whose first
// element must be "curl".
func ParseTokens(tokens []string) (*request.Request, []string, error) {
	if len(tokens) == 0 || tokens[0] != "curl" {
		return nil, nil, fmt.Errorf("command does not start with curl")
	}
	var (
		warns      []string
		rawURL     string
		method     string
		dataParts  []string
		headers    []request.Header
		hostHdr    string
		getMode    bool
		headMode   bool
		jsonMode   bool
		compressed bool
	)
	args := tokens[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		takeValue := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s is missing its value", arg)
			}
			i++
			return args[i], nil
		}
		switch {
		case arg == "-G" || arg == "--get":
			getMode = true
		case arg == "-I" || arg == "--head":
			headMode = true
		case arg == "--compressed":
			compressed = true
		case arg == "-H" || arg == "--header":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			name, value, ok := splitHeader(v)
			if !ok {
				return nil, nil, fmt.Errorf("malformed header %q (want 'Name: value')", v)
			}
			switch strings.ToLower(name) {
			case "host":
				hostHdr = value
			case "content-length":
				warns = append(warns, "ignoring Content-Length header (recomputed at send time)")
			default:
				headers = append(headers, request.Header{Name: name, Value: value})
			}
		case arg == "-X" || arg == "--request":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			method = strings.ToUpper(v)
		case arg == "-d" || arg == "--data" || arg == "--data-raw" ||
			arg == "--data-binary" || arg == "--data-ascii":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			if strings.HasPrefix(v, "@") && arg != "--data-raw" {
				return nil, nil, fmt.Errorf("reading request data from a file (%s %s) is not supported; inline the payload", arg, v)
			}
			dataParts = append(dataParts, v)
		case arg == "--data-urlencode":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			enc, err := dataURLEncode(v)
			if err != nil {
				return nil, nil, err
			}
			dataParts = append(dataParts, enc)
		case arg == "--json":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			dataParts = append(dataParts, v)
			jsonMode = true
		case arg == "-b" || arg == "--cookie":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			if !strings.Contains(v, "=") {
				return nil, nil, fmt.Errorf("cookie files (%s %s) are not supported; pass 'name=value' pairs", arg, v)
			}
			headers = append(headers, request.Header{Name: "Cookie", Value: v})
		case arg == "-u" || arg == "--user":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			cred := base64.StdEncoding.EncodeToString([]byte(v))
			headers = append(headers, request.Header{Name: "Authorization", Value: "Basic " + cred})
		case arg == "-A" || arg == "--user-agent":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			headers = append(headers, request.Header{Name: "User-Agent", Value: v})
		case arg == "-e" || arg == "--referer":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			headers = append(headers, request.Header{Name: "Referer", Value: v})
		case arg == "--url":
			v, err := takeValue()
			if err != nil {
				return nil, nil, err
			}
			rawURL = v
		case arg == "-F" || arg == "--form":
			return nil, nil, fmt.Errorf("multipart form data (%s) is not supported in v0.1.0", arg)
		case arg == "-T" || arg == "--upload-file":
			return nil, nil, fmt.Errorf("file uploads (%s) are not supported in v0.1.0", arg)
		default:
			if want, known := valueFlags[arg]; known {
				v, err := takeValue()
				if err != nil {
					return nil, nil, err
				}
				if !want {
					warns = append(warns, fmt.Sprintf("ignoring transfer option %s %s", arg, v))
				} else if arg == "-r" || arg == "--range" {
					headers = append(headers, request.Header{Name: "Range", Value: "bytes=" + v})
				}
				continue
			}
			if boolFlags[arg] {
				warns = append(warns, "ignoring transfer option "+arg)
				continue
			}
			if combinedIgnorable(arg) {
				warns = append(warns, "ignoring transfer options "+arg)
				continue
			}
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return nil, nil, fmt.Errorf("unsupported curl flag %s", arg)
			}
			if rawURL != "" {
				return nil, nil, fmt.Errorf("multiple URLs given (%q and %q); reqmin minimizes one request at a time", rawURL, arg)
			}
			rawURL = arg
		}
	}
	if rawURL == "" {
		return nil, nil, fmt.Errorf("no URL found in the curl command")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL // curl's scheme-less default
	}

	req := &request.Request{Proto: "HTTP/1.1"}
	if err := req.SetURL(rawURL); err != nil {
		return nil, nil, err
	}
	req.Headers = headers
	req.ExplicitHost = hostHdr

	data := strings.Join(dataParts, "&")
	switch {
	case getMode && data != "":
		req.Query = append(req.Query, request.ParsePairs(data)...)
	case data != "":
		req.Body = []byte(data)
	}

	if headMode {
		req.Method = "HEAD"
	} else if len(req.Body) > 0 {
		req.Method = "POST"
	} else {
		req.Method = "GET"
	}
	if method != "" {
		req.Method = method
	}

	if jsonMode {
		if _, ok := req.HeaderGet("Content-Type"); !ok {
			req.Headers = append(req.Headers, request.Header{Name: "Content-Type", Value: "application/json"})
		}
		if _, ok := req.HeaderGet("Accept"); !ok {
			req.Headers = append(req.Headers, request.Header{Name: "Accept", Value: "application/json"})
		}
	} else if len(req.Body) > 0 {
		if _, ok := req.HeaderGet("Content-Type"); !ok {
			// curl's implicit default for -d; the server's behavior may
			// depend on it, so it must be part of the search space.
			req.Headers = append(req.Headers, request.Header{Name: "Content-Type", Value: "application/x-www-form-urlencoded"})
		}
	}
	if compressed {
		if _, ok := req.HeaderGet("Accept-Encoding"); !ok {
			req.Headers = append(req.Headers, request.Header{Name: "Accept-Encoding", Value: "gzip, deflate"})
		}
	}
	return req, warns, nil
}

// splitHeader splits "Name: value" (value may be empty). curl's "Name;"
// form for forcing an empty header is also accepted.
func splitHeader(s string) (string, string, bool) {
	if i := strings.Index(s, ":"); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
	}
	if strings.HasSuffix(s, ";") && len(s) > 1 && !strings.Contains(s, " ") {
		return s[:len(s)-1], "", true
	}
	return "", "", false
}

// dataURLEncode implements curl's --data-urlencode forms:
// "content", "=content", and "name=content".
func dataURLEncode(v string) (string, error) {
	if strings.HasPrefix(v, "@") || strings.Contains(v, "=@") {
		return "", fmt.Errorf("reading --data-urlencode from a file (%s) is not supported", v)
	}
	if strings.HasPrefix(v, "=") {
		return url.QueryEscape(v[1:]), nil
	}
	if i := strings.Index(v, "="); i >= 0 {
		return v[:i] + "=" + url.QueryEscape(v[i+1:]), nil
	}
	return url.QueryEscape(v), nil
}

// combinedIgnorable reports whether arg is a bundle of ignorable short
// flags, e.g. "-sS" or "-sSL".
func combinedIgnorable(arg string) bool {
	if len(arg) < 3 || arg[0] != '-' || arg[1] == '-' {
		return false
	}
	for _, c := range arg[1:] {
		if !strings.ContainsRune(shortIgnorable, c) {
			return false
		}
	}
	return true
}
