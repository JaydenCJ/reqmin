// Tokenizer tests: pasted "Copy as cURL" text arrives with every quoting
// style browsers emit, so getting argv splitting right is the difference
// between minimizing the user's request and minimizing a mangled one.
package curl

import (
	"reflect"
	"testing"
)

func mustSplit(t *testing.T, in string) []string {
	t.Helper()
	got, err := SplitCommand(in)
	if err != nil {
		t.Fatalf("SplitCommand(%q): %v", in, err)
	}
	return got
}

func TestSplitPlainWordsAndWhitespaceRuns(t *testing.T) {
	got := mustSplit(t, "curl   -X\t\tPOST  http://example.test/a")
	want := []string{"curl", "-X", "POST", "http://example.test/a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitSingleQuotesPreserveEverything(t *testing.T) {
	// Single quotes are the default of browser "Copy as cURL" on
	// macOS/Linux; nothing inside them may be interpreted.
	got := mustSplit(t, `curl 'a b' '$HOME' '\n'`)
	want := []string{"curl", "a b", "$HOME", `\n`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitDoubleQuotesWithEscapes(t *testing.T) {
	// In POSIX double quotes, backslash escapes only $ ` " \ and newline;
	// before anything else (like \s) it stays literal.
	got := mustSplit(t, `curl "a \"quoted\" value" "back\\slash" "rm\s+-rf"`)
	want := []string{"curl", `a "quoted" value`, `back\slash`, `rm\s+-rf`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitANSICQuoting(t *testing.T) {
	// Chrome switches to $'…' when a header value contains an apostrophe.
	got := mustSplit(t, `curl $'it\'s\na test\x21'`)
	want := []string{"curl", "it's\na test!"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitLineContinuations(t *testing.T) {
	// Both Unix (\ + LF) and Windows-flavored (\ + CRLF) continuations.
	got := mustSplit(t, "curl \\\n  -H 'A: b' \\\r\n  url")
	want := []string{"curl", "-H", "A: b", "url"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitJoinsSegmentsAndKeepsEmptyArgs(t *testing.T) {
	// Adjacent quoted segments concatenate, '' survives as an empty arg,
	// and a bare backslash escapes the next character.
	got := mustSplit(t, `curl 'a'"b"c '' x\ y`)
	want := []string{"curl", "abc", "", "x y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitMalformedInputIsAnError(t *testing.T) {
	for _, in := range []string{"curl 'oops", `curl "oops`, `curl $'oops`, `curl trailing\`, `curl $'\q'`} {
		if _, err := SplitCommand(in); err == nil {
			t.Errorf("SplitCommand(%q): want error, got none", in)
		}
	}
}
