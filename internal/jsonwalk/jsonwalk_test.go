// Ordered-JSON tests: minimizing a JSON body must never reorder the
// surviving members or rewrite untouched number literals — the shrunken
// payload has to look like the user's payload, minus the removed keys.
package jsonwalk

import (
	"reflect"
	"testing"
)

func mustParse(t *testing.T, src string) *Value {
	t.Helper()
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return v
}

func TestParseEncodePreservesMemberOrder(t *testing.T) {
	src := `{"zeta":1,"alpha":2,"mid":{"y":true,"a":null}}`
	if got := string(mustParse(t, src).Encode()); got != src {
		t.Fatalf("encode = %s, want original order %s", got, src)
	}
}

func TestParsePreservesNumberLiterals(t *testing.T) {
	// float64 round-tripping would turn 1e2 into 100 and mangle big ints.
	src := `{"a":1e2,"b":0.30,"c":9007199254740993}`
	if got := string(mustParse(t, src).Encode()); got != src {
		t.Fatalf("encode = %s, want literals preserved from %s", got, src)
	}
}

func TestParseScalarKindsAndArrays(t *testing.T) {
	v := mustParse(t, `{"s":"x","n":1,"b":false,"z":null,"arr":[1,"two",{"k":3}]}`)
	kinds := map[string]Kind{}
	for _, m := range v.Members {
		kinds[m.Key] = m.Value.Kind
	}
	want := map[string]Kind{"s": String, "n": Number, "b": Bool, "z": Null, "arr": Array}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}

func TestParseRejectsMalformedAndTrailingData(t *testing.T) {
	for _, src := range []string{`{"a":}`, `{"a":1}}`, `{"a":1} extra`, `{`, ``} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q): want error, got none", src)
		}
	}
}

func TestObjectPathsListsParentsBeforeChildren(t *testing.T) {
	v := mustParse(t, `{"user":{"id":1,"address":{"zip":"9"}},"flag":true}`)
	got := v.ObjectPaths()
	want := [][]string{
		{"user"},
		{"user", "id"},
		{"user", "address"},
		{"user", "address", "zip"},
		{"flag"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if s := PathString(got[3]); s != "user.address.zip" {
		t.Fatalf("PathString = %q", s)
	}
	// Removing array elements would shift sibling indices; v0.1.0 treats
	// arrays as opaque values and never descends into them.
	arr := mustParse(t, `{"items":[{"deep":1}]}`)
	if got := arr.ObjectPaths(); !reflect.DeepEqual(got, [][]string{{"items"}}) {
		t.Fatalf("array paths = %v, want just [items]", got)
	}
}

func TestRemoveNestedKey(t *testing.T) {
	v := mustParse(t, `{"user":{"id":1,"name":"a"},"keep":2}`)
	if !v.Remove([]string{"user", "name"}) {
		t.Fatal("Remove reported nothing removed")
	}
	if got := string(v.Encode()); got != `{"user":{"id":1},"keep":2}` {
		t.Fatalf("encode = %s", got)
	}
}

func TestRemoveMissingPathsAreNoOps(t *testing.T) {
	// ddmin drops parents and children independently, so removing a child
	// whose parent is already gone must neither error nor mutate.
	v := mustParse(t, `{"user":{"id":1},"z":0}`)
	if v.Remove([]string{"ghost", "child"}) {
		t.Fatal("Remove of a missing path reported success")
	}
	v.Remove([]string{"user"})
	if v.Remove([]string{"user", "id"}) {
		t.Fatal("removing a child of a removed parent should be a no-op")
	}
	if got := string(v.Encode()); got != `{"z":0}` {
		t.Fatalf("encode = %s", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	orig := mustParse(t, `{"a":{"b":1},"arr":[1,2]}`)
	clone := orig.Clone()
	clone.Remove([]string{"a", "b"})
	clone.Elems = nil
	if got := string(orig.Encode()); got != `{"a":{"b":1},"arr":[1,2]}` {
		t.Fatalf("mutating the clone changed the original: %s", got)
	}
}

func TestEncodeUnicodeStringStaysValid(t *testing.T) {
	src := `{"greeting":"héllo \"world\""}`
	v := mustParse(t, src)
	back, err := Parse(v.Encode())
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if string(back.Encode()) != string(v.Encode()) {
		t.Fatalf("encoding is not stable: %s vs %s", back.Encode(), v.Encode())
	}
}
