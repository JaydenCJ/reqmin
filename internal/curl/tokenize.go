// Shell-style tokenizer for pasted curl command lines.
//
// "Copy as cURL" output differs by browser and OS: single quotes on
// macOS/Linux, $'…' ANSI-C quoting when a header contains an apostrophe,
// double quotes from some Windows tools, and backslash-newline continuations
// everywhere. This tokenizer accepts all of those without shelling out.
package curl

import (
	"fmt"
	"strings"
)

// SplitCommand splits a command line into argv the way a POSIX shell would,
// supporting '…', "…" (with \" \\ \$ \` escapes), $'…' ANSI-C quoting, bare
// backslash escapes, and backslash-newline line continuations.
func SplitCommand(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		started bool // distinguishes "" (an empty quoted arg) from no arg
	)
	flush := func() {
		if started {
			args = append(args, cur.String())
			cur.Reset()
			started = false
		}
	}
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
			i++
		case c == '\\':
			if i+1 >= len(s) {
				return nil, fmt.Errorf("trailing backslash")
			}
			next := s[i+1]
			if next == '\n' { // line continuation: swallow both
				i += 2
				continue
			}
			if next == '\r' && i+2 < len(s) && s[i+2] == '\n' {
				i += 3
				continue
			}
			started = true
			cur.WriteByte(next)
			i += 2
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated single quote")
			}
			started = true
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 2
		case c == '"':
			consumed, text, err := scanDoubleQuoted(s[i:])
			if err != nil {
				return nil, err
			}
			started = true
			cur.WriteString(text)
			i += consumed
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			consumed, text, err := scanANSIC(s[i:])
			if err != nil {
				return nil, err
			}
			started = true
			cur.WriteString(text)
			i += consumed
		default:
			started = true
			cur.WriteByte(c)
			i++
		}
	}
	flush()
	return args, nil
}

// scanDoubleQuoted consumes a leading "…" segment; returns bytes consumed
// and the decoded text.
func scanDoubleQuoted(s string) (int, string, error) {
	var b strings.Builder
	i := 1 // skip opening quote
	for i < len(s) {
		c := s[i]
		switch c {
		case '"':
			return i + 1, b.String(), nil
		case '\\':
			if i+1 >= len(s) {
				return 0, "", fmt.Errorf("trailing backslash in double quotes")
			}
			next := s[i+1]
			switch next {
			case '"', '\\', '$', '`':
				b.WriteByte(next)
				i += 2
			case '\n': // continuation inside double quotes
				i += 2
			default: // backslash is literal before anything else
				b.WriteByte('\\')
				b.WriteByte(next)
				i += 2
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return 0, "", fmt.Errorf("unterminated double quote")
}

// scanANSIC consumes a leading $'…' segment (bash ANSI-C quoting) and
// decodes the escapes browsers actually emit.
func scanANSIC(s string) (int, string, error) {
	var b strings.Builder
	i := 2 // skip $'
	for i < len(s) {
		c := s[i]
		switch c {
		case '\'':
			return i + 1, b.String(), nil
		case '\\':
			if i+1 >= len(s) {
				return 0, "", fmt.Errorf("trailing backslash in $'…'")
			}
			next := s[i+1]
			switch next {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			case '\'':
				b.WriteByte('\'')
			case '"':
				b.WriteByte('"')
			case '0':
				b.WriteByte(0)
			case 'x':
				if i+3 >= len(s) {
					return 0, "", fmt.Errorf("truncated \\x escape in $'…'")
				}
				hi, ok1 := hexVal(s[i+2])
				lo, ok2 := hexVal(s[i+3])
				if !ok1 || !ok2 {
					return 0, "", fmt.Errorf("invalid \\x escape in $'…'")
				}
				b.WriteByte(hi<<4 | lo)
				i += 4
				continue
			default:
				return 0, "", fmt.Errorf("unsupported escape \\%c in $'…'", next)
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return 0, "", fmt.Errorf("unterminated $'…' quote")
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
