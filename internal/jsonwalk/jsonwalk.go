// Package jsonwalk is a small order-preserving JSON document model.
//
// encoding/json unmarshals objects into maps, which destroys member order —
// unacceptable for a tool whose whole promise is "the minimized request is
// your request minus the parts that don't matter". jsonwalk parses into an
// ordered tree, enumerates removable object-key paths, removes keys by path,
// and re-encodes compactly with the original member order and the original
// number literals intact.
package jsonwalk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Kind classifies a Value.
type Kind int

// The JSON value kinds.
const (
	Invalid Kind = iota
	Object
	Array
	String
	Number
	Bool
	Null
)

// Member is one key/value member of an object, order-preserving.
type Member struct {
	Key   string
	Value *Value
}

// Value is one node of the document tree. Scalars keep their literal text in
// Lit (numbers verbatim, strings/bools/null re-marshaled canonically).
type Value struct {
	Kind    Kind
	Members []Member // Kind == Object
	Elems   []*Value // Kind == Array
	Lit     string   // scalar literal, already valid JSON
}

// Parse decodes data into an ordered tree. Exactly one top-level value is
// allowed; trailing non-whitespace is an error.
func Parse(data []byte) (*Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (*Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := &Value{Kind: Object}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Members = append(obj.Members, Member{Key: key, Value: val})
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := &Value{Kind: Array}
			for dec.More() {
				el, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr.Elems = append(arr.Elems, el)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		lit, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		return &Value{Kind: String, Lit: string(lit)}, nil
	case json.Number:
		return &Value{Kind: Number, Lit: t.String()}, nil
	case bool:
		if t {
			return &Value{Kind: Bool, Lit: "true"}, nil
		}
		return &Value{Kind: Bool, Lit: "false"}, nil
	case nil:
		return &Value{Kind: Null, Lit: "null"}, nil
	default:
		return nil, fmt.Errorf("unexpected token %v", tok)
	}
}

// Encode renders the tree compactly, preserving member order.
func (v *Value) Encode() []byte {
	var b strings.Builder
	v.encode(&b)
	return []byte(b.String())
}

func (v *Value) encode(b *strings.Builder) {
	switch v.Kind {
	case Object:
		b.WriteByte('{')
		for i, m := range v.Members {
			if i > 0 {
				b.WriteByte(',')
			}
			key, _ := json.Marshal(m.Key)
			b.Write(key)
			b.WriteByte(':')
			m.Value.encode(b)
		}
		b.WriteByte('}')
	case Array:
		b.WriteByte('[')
		for i, el := range v.Elems {
			if i > 0 {
				b.WriteByte(',')
			}
			el.encode(b)
		}
		b.WriteByte(']')
	default:
		b.WriteString(v.Lit)
	}
}

// Clone deep-copies the tree so a candidate can be mutated safely.
func (v *Value) Clone() *Value {
	c := &Value{Kind: v.Kind, Lit: v.Lit}
	if v.Members != nil {
		c.Members = make([]Member, len(v.Members))
		for i, m := range v.Members {
			c.Members[i] = Member{Key: m.Key, Value: m.Value.Clone()}
		}
	}
	if v.Elems != nil {
		c.Elems = make([]*Value, len(v.Elems))
		for i, el := range v.Elems {
			c.Elems[i] = el.Clone()
		}
	}
	return c
}

// ObjectPaths enumerates every object-key path reachable from the root
// through objects only (arrays are treated as opaque in v0.1.0, since
// removing elements would shift sibling indices). Parents are listed before
// their children, so removing a parent subsumes its descendants.
func (v *Value) ObjectPaths() [][]string {
	var out [][]string
	var walk func(node *Value, prefix []string)
	walk = func(node *Value, prefix []string) {
		if node.Kind != Object {
			return
		}
		for _, m := range node.Members {
			path := append(append([]string(nil), prefix...), m.Key)
			out = append(out, path)
			walk(m.Value, path)
		}
	}
	walk(v, nil)
	return out
}

// Remove deletes the object member at path, mutating the tree. It reports
// whether anything was removed; removing an already-removed path (e.g. a
// child whose parent was dropped first) is a harmless no-op.
func (v *Value) Remove(path []string) bool {
	if len(path) == 0 {
		return false
	}
	node := v
	for _, key := range path[:len(path)-1] {
		next := node.member(key)
		if next == nil {
			return false
		}
		node = next
	}
	if node.Kind != Object {
		return false
	}
	last := path[len(path)-1]
	for i, m := range node.Members {
		if m.Key == last {
			node.Members = append(node.Members[:i], node.Members[i+1:]...)
			return true
		}
	}
	return false
}

func (v *Value) member(key string) *Value {
	if v.Kind != Object {
		return nil
	}
	for _, m := range v.Members {
		if m.Key == key {
			return m.Value
		}
	}
	return nil
}

// PathString renders a path for display, joining keys with dots.
func PathString(path []string) string {
	return strings.Join(path, ".")
}
