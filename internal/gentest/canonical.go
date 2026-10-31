package gentest

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ornew/zeroparse/internal/gen/gogen"
)

// CanonicalValueSource is the Go code of the generated program that turns a typed value into the
// tree canonicalTyped reads. A struct is an object with its Go type name ("struct") and its
// exported fields ("fields") under their Go names. A struct that embeds a span has "span" and its
// positions; a struct that embeds a node has "node", its positions and its "value". A slice is an
// array, and a nil slice is null.
const CanonicalValueSource = `
// canonicalValue returns v as the tree of JSON values that the test canonicalizes.
func canonicalValue(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return canonicalValue(v.Elem())
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = canonicalValue(v.Index(i))
		}
		return out
	case reflect.Struct:
		out := map[string]any{"struct": typeName(v.Interface())}
		fields := map[string]any{}
		out["fields"] = fields
		if strings.HasPrefix(v.Type().Name(), "TNode") {
			// The node itself: its span and its value are its own fields.
			out["node"] = true
			out["start"], out["end"] = v.FieldByName("Start").Int(), v.FieldByName("End").Int()
			out["value"] = canonicalValue(v.FieldByName("Value"))
			return out
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if f.Anonymous {
				switch {
				case f.Type.Name() == "Span":
					out["span"] = true
					out["start"], out["end"] = v.Field(i).Field(0).Int(), v.Field(i).Field(1).Int()
				case strings.HasPrefix(f.Type.Name(), "TNode"):
					node := v.Field(i)
					out["node"] = true
					out["start"], out["end"] = node.FieldByName("Start").Int(), node.FieldByName("End").Int()
					out["value"] = canonicalValue(node.FieldByName("Value"))
				default:
					panic("canonicalValue: unexpected embedded field " + f.Name)
				}
				continue
			}
			fields[f.Name] = canonicalValue(v.Field(i))
		}
		return out
	case reflect.Map, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		panic("canonicalValue: unsupported kind " + v.Kind().String())
	}
	return v.Interface()
}
`

// engineDroppedKeys are the keys of an engine node that the canonical form drops: the rule that
// produced a node without an action, and the locations of its captures. A typed value has no
// counterpart for them.
var engineDroppedKeys = []string{"rule", "captures"}

// CanonicalEngine returns the canonical form of an engine value, decoded from its JSON. The
// engine's shapes are:
//   - a node (string, tuple, list or named node) is {"start", "end", "value"}, with "type" for a
//     named node type; the value of a tuple or list is an array;
//   - a struct is {"type", "start", "end", "fields"}: its range is that of the rule that built it,
//     and its fields are keyed by their grammar names.
//
// Keys other than the dropped ones are errors, so the canonical form never hides a difference.
func CanonicalEngine(v any) (any, error) {
	switch v := v.(type) {
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			c, err := CanonicalEngine(x)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		if _, ok := v["fields"]; ok {
			if err := onlyKeys(v, "type", "start", "end", "fields"); err != nil {
				return nil, err
			}
			fields, ok := v["fields"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("engine fields are %T", v["fields"])
			}
			out := map[string]any{"type": v["type"], "start": v["start"], "end": v["end"], "fields": map[string]any{}}
			for name, x := range fields {
				c, err := CanonicalEngine(x)
				if err != nil {
					return nil, err
				}
				out["fields"].(map[string]any)[name] = c
			}
			return out, nil
		}
		if _, ok := v["start"]; ok {
			if err := onlyKeys(v, append([]string{"type", "start", "end", "value"}, engineDroppedKeys...)...); err != nil {
				return nil, err
			}
			value, err := CanonicalEngine(v["value"])
			if err != nil {
				return nil, err
			}
			out := map[string]any{"start": v["start"], "end": v["end"], "value": value}
			if t, ok := v["type"]; ok {
				out["type"] = t
			}
			return out, nil
		}
		return nil, fmt.Errorf("engine object with keys %v has no known shape", keysOf(v))
	}
	return v, nil
}

// TypedGenericNode is the "type" of a canonical typed value that is a generic TNode[...]. A field
// declared as an anonymous node type, such as node[string], holds a generic node, which has no Go
// counterpart for the named node type the engine's value may keep; CanonicalDifferences takes it
// to match any engine "type" or none.
const TypedGenericNode = "\x00generic node"

// canonicalTyped returns the canonical form of a typed value, from the tree CanonicalValueSource
// builds. It maps the typed shapes to the engine's:
//   - a node (a struct with "node") is {"start", "end", "value", "type"}, where "type" is the named
//     node type, or TypedGenericNode for a generic TNode[...];
//   - a struct with a span is {"type", "start", "end", "fields"}, the engine's struct shape; the
//     "span" marker, which the Go representation adds, is dropped;
//   - a struct without a span whose fields are E0, E1, ... is a tuple, which is an array;
//   - the fields are keyed by their grammar names (goFieldName).
//
// Keys other than the listed ones are errors.
func canonicalTyped(v any) (any, error) {
	switch v := v.(type) {
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			c, err := canonicalTyped(x)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		name, ok := v["struct"].(string)
		if !ok {
			return nil, fmt.Errorf("typed object with keys %v has no struct name", keysOf(v))
		}
		fields, ok := v["fields"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("typed struct %s has no fields object", name)
		}
		if node, _ := v["node"].(bool); node {
			if err := onlyKeys(v, "struct", "node", "start", "end", "value", "fields"); err != nil {
				return nil, err
			}
			if len(fields) != 0 {
				return nil, fmt.Errorf("typed node %s has fields %v", name, keysOf(fields))
			}
			value, err := canonicalTyped(v["value"])
			if err != nil {
				return nil, err
			}
			out := map[string]any{"start": v["start"], "end": v["end"], "value": value}
			if strings.HasPrefix(name, "TNode[") {
				out["type"] = TypedGenericNode
			} else {
				out["type"] = name
			}
			return out, nil
		}
		if err := onlyKeys(v, "struct", "span", "start", "end", "fields"); err != nil {
			return nil, err
		}
		if span, _ := v["span"].(bool); span {
			canon, err := canonicalTypedFields(fields)
			if err != nil {
				return nil, err
			}
			return map[string]any{"type": name, "start": v["start"], "end": v["end"], "fields": canon}, nil
		}
		if span, _ := v["span"].(bool); !span && name == "Unit" && len(fields) == 0 {
			// The generated Unit is the empty tuple, which the engine encodes as an empty array.
			return []any{}, nil
		}
		if span, _ := v["span"].(bool); !span {
			if elems, ok, err := tupleElements(fields); err != nil {
				return nil, err
			} else if ok {
				out := make([]any, len(elems))
				for i, x := range elems {
					c, err := canonicalTyped(x)
					if err != nil {
						return nil, err
					}
					out[i] = c
				}
				return out, nil
			}
		}
		canon, err := canonicalTypedFields(fields)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": name, "fields": canon}, nil
	}
	return v, nil
}

// goFieldName returns the grammar name of the field goName of a generated struct. The generator adds
// a "_" to a field whose name is a runtime method name or the name of the embedded span, so a single
// trailing "_" over such a name is removed. Other names are returned as they are.
func goFieldName(goName string) string {
	if base, ok := strings.CutSuffix(goName, "_"); ok && gogen.RuntimeMethodNames[base] {
		return base
	}
	return goName
}

func canonicalTypedFields(fields map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(fields))
	for goName, x := range fields {
		name := goFieldName(goName)
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("typed fields map to the grammar field %s twice", name)
		}
		c, err := canonicalTyped(x)
		if err != nil {
			return nil, err
		}
		out[name] = c
	}
	return out, nil
}

// tupleElements returns the elements of a typed tuple, whose fields are E0, E1, ... in order, and
// whether fields are those of a tuple.
func tupleElements(fields map[string]any) ([]any, bool, error) {
	if len(fields) == 0 {
		return nil, false, nil
	}
	elems := make([]any, len(fields))
	for k, x := range fields {
		i, err := strconv.Atoi(strings.TrimPrefix(k, "E"))
		if !strings.HasPrefix(k, "E") || err != nil || i < 0 || i >= len(fields) || "E"+strconv.Itoa(i) != k {
			return nil, false, nil
		}
		elems[i] = x
	}
	return elems, true, nil
}

// CanonicalTypedMatches reports whether the typed value encoded in raw, as the generated program
// prints it, has the canonical form want of the engine's value.
func CanonicalTypedMatches(raw json.RawMessage, want any) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	got, err := canonicalTyped(v)
	if err != nil {
		return err
	}
	var diffs []string
	CanonicalDifferences(got, want, "$", false, &diffs)
	if len(diffs) > 0 {
		return fmt.Errorf("canonical values differ: %s", strings.Join(diffs, "; "))
	}
	return nil
}

// CanonicalDifferences appends to out a description of every place where the canonical values a
// and b differ, by path. fieldsObject tells whether a is the "fields" object of a struct, whose keys
// are the struct's declared fields. A key of such an object that only the typed side has is not a
// difference when the typed value is nil or a zero scalar: the spec says an unset field reads as
// nil, the engine writes no key for it, and the typed runtime writes the zero value of the field's
// Go type (see canonicalUnsetZero). The engine writes the key of every field set to a zero value,
// which TestEngineStructKeepsExplicitZeroFields pins, so a missing key there is an unset field. A
// key outside a fields object is a difference whatever its value.
func CanonicalDifferences(a, b any, path string, fieldsObject bool, out *[]string) {
	switch a := a.(type) {
	case map[string]any:
		if b, ok := b.(map[string]any); ok {
			_, isStruct := a["fields"]
			keys := slices.Sorted(maps.Keys(a))
			for k := range b {
				if _, ok := a[k]; !ok {
					keys = append(keys, k)
				}
			}
			for _, k := range keys {
				x, inA := a[k]
				y, inB := b[k]
				switch {
				case k == "type" && x == TypedGenericNode:
					// A generic typed node matches the engine's node whatever its named type.
				case inA && inB:
					CanonicalDifferences(x, y, path+"."+k, isStruct && k == "fields", out)
				case inA && fieldsObject && canonicalUnsetZero(x):
					// The engine omits an unset field of a struct; the typed value holds its zero value.
				default:
					*out = append(*out, fmt.Sprintf("%s.%s present only in the %s value", path, k, map[bool]string{true: "typed", false: "engine"}[inA]))
				}
			}
			return
		}
	case []any:
		if b, ok := b.([]any); ok && len(a) == len(b) {
			for i := range a {
				CanonicalDifferences(a[i], b[i], fmt.Sprintf("%s[%d]", path, i), false, out)
			}
			return
		}
	}
	if reflect.DeepEqual(a, b) {
		return
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	*out = append(*out, fmt.Sprintf("%s: typed %.200s, engine %.200s", path, ab, bb))
}

// canonicalUnsetZero reports whether a typed value is the zero value of its Go type that an unset
// engine field reads as: null (a nil pointer, slice or interface), "", 0 or false. An empty list is
// not, so an unset engine field never equals [].
func canonicalUnsetZero(x any) bool {
	switch x := x.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}

// onlyKeys reports an error if m has a key outside allowed.
func onlyKeys(m map[string]any, allowed ...string) error {
	for k := range m {
		if !slices.Contains(allowed, k) {
			return fmt.Errorf("unexpected key %q in %v", k, keysOf(m))
		}
	}
	return nil
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
