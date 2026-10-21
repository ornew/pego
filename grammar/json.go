package grammar

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// In the JSON representation, a value of an interface type (Decl,
// TypeExpr, Expr, or Term) is an object whose "@type" member
// holds the Go type name. Field names follow the json tags.

var registry = map[string]reflect.Type{}

func register(vs ...any) {
	for _, v := range vs {
		t := reflect.TypeOf(v).Elem()
		registry[t.Name()] = t
	}
}

func init() {
	register(
		(*TypeDef)(nil), (*TypeAlias)(nil), (*VarDef)(nil), (*RuleDef)(nil),
		(*TypeRef)(nil), (*ListType)(nil), (*OptionalType)(nil), (*JoinType)(nil), (*MeetType)(nil), (*TupleType)(nil), (*TypeApp)(nil), (*StructType)(nil), (*TypeGroup)(nil),
		(*Ref)(nil), (*Literal)(nil), (*CharClass)(nil), (*Any)(nil), (*Seq)(nil),
		(*Choice)(nil), (*Group)(nil), (*InlineAction)(nil), (*Repeat)(nil), (*Optional)(nil), (*And)(nil), (*Not)(nil),
		(*Atomic)(nil), (*Recovery)(nil), (*Stream)(nil), (*Discard)(nil), (*Capture)(nil), (*Cut)(nil), (*Top)(nil),
		(*Bottom)(nil), (*BeginInput)(nil), (*EndInput)(nil), (*BeginLine)(nil),
		(*EndLine)(nil), (*Predicate)(nil), (*Attributed)(nil), (*OperatorsRule)(nil),
		(*IntLit)(nil), (*StringLit)(nil), (*BoolLit)(nil), (*NilLit)(nil),
		(*CaptureRef)(nil), (*IndexRef)(nil), (*VarRef)(nil), (*Member)(nil),
		(*New)(nil), (*Call)(nil), (*Lambda)(nil), (*Binary)(nil), (*Unary)(nil), (*Cast)(nil),
		(*Bind)(nil), (*Block)(nil), (*TermGroup)(nil), (*ListConstructor)(nil), (*TupleLit)(nil),
	)
}

// MarshalJSON converts g to JSON.
func MarshalJSON(g *Grammar) ([]byte, error) {
	v, err := encode(reflect.ValueOf(g))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes a grammar from JSON.
// It requires one complete JSON value and checks structural validity with
// Validate. Unknown fields and duplicate object keys retain encoding/json's
// permissive behavior; compilation still performs semantic checks.
func UnmarshalJSON(data []byte) (*Grammar, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("$: trailing JSON value")
		}
		return nil, fmt.Errorf("$: trailing JSON data: %w", err)
	}
	v, err := decode(raw, reflect.TypeOf((*Grammar)(nil)), "$")
	if err != nil {
		return nil, err
	}
	g := v.Interface().(*Grammar)
	if err := Validate(g); err != nil {
		return nil, err
	}
	return g, nil
}

type fieldTag struct {
	name      string
	omitEmpty bool
	skip      bool
}

func parseTag(f reflect.StructField) fieldTag {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return fieldTag{skip: true}
	}
	name, opts, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	return fieldTag{name: name, omitEmpty: opts == "omitempty"}
}

func encode(v reflect.Value) (any, error) {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		e := v.Elem()
		if v.Type().NumMethod() == 0 && (e.Kind() == reflect.String || e.Kind() == reflect.Bool || e.Kind() == reflect.Int) {
			return e.Interface(), nil
		}
		m, err := encode(e)
		if err != nil {
			return nil, err
		}
		obj, ok := m.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("grammar: %s is not an object", e.Type())
		}
		obj["@type"] = e.Elem().Type().Name()
		return obj, nil
	case reflect.Pointer:
		if v.IsNil() {
			return nil, nil
		}
		return encode(v.Elem())
	case reflect.Struct:
		obj := map[string]any{}
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			tag := parseTag(t.Field(i))
			if tag.skip {
				continue
			}
			fv := v.Field(i)
			if tag.omitEmpty && fv.IsZero() {
				continue
			}
			x, err := encode(fv)
			if err != nil {
				return nil, err
			}
			// Raw json:"-" fields do not make an optional metadata object
			// present when all of its serialized fields are empty.
			if tag.omitEmpty && fv.Kind() == reflect.Struct {
				if fields, ok := x.(map[string]any); ok && len(fields) == 0 {
					continue
				}
			}
			obj[tag.name] = x
		}
		return obj, nil
	case reflect.Slice:
		arr := make([]any, v.Len())
		for i := range arr {
			x, err := encode(v.Index(i))
			if err != nil {
				return nil, err
			}
			arr[i] = x
		}
		return arr, nil
	case reflect.Map:
		obj := map[string]any{}
		for _, key := range v.MapKeys() {
			x, err := encode(v.MapIndex(key))
			if err != nil {
				return nil, err
			}
			obj[key.String()] = x
		}
		return obj, nil
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int32:
		return v.Interface(), nil
	}
	return nil, fmt.Errorf("grammar: cannot encode %s", v.Type())
}

func decode(raw any, t reflect.Type, path string) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.Interface:
		if raw == nil {
			return reflect.Zero(t), nil
		}
		if t.NumMethod() == 0 {
			switch value := raw.(type) {
			case string, bool:
				return reflect.ValueOf(value), nil
			case json.Number:
				return decode(value, reflect.TypeFor[int](), path)
			}
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected an object", path)
		}
		name, _ := obj["@type"].(string)
		st, ok := registry[name]
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: unknown @type %q", path, name)
		}
		p := reflect.PointerTo(st)
		if !p.Implements(t) {
			return reflect.Value{}, fmt.Errorf("%s: %s is not a %s", path, name, t.Name())
		}
		v, err := decode(raw, p, path)
		if err != nil {
			return reflect.Value{}, err
		}
		iv := reflect.New(t).Elem()
		iv.Set(v)
		return iv, nil
	case reflect.Pointer:
		if raw == nil {
			return reflect.Zero(t), nil
		}
		v := reflect.New(t.Elem())
		e, err := decode(raw, t.Elem(), path)
		if err != nil {
			return reflect.Value{}, err
		}
		v.Elem().Set(e)
		return v, nil
	case reflect.Struct:
		obj, ok := raw.(map[string]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected an object", path)
		}
		// A concrete node type takes no discriminator, but a present one must name it.
		if name, ok := obj["@type"]; ok && name != t.Name() {
			return reflect.Value{}, fmt.Errorf("%s: @type %v is not a %s", path, name, t.Name())
		}
		v := reflect.New(t).Elem()
		for i := 0; i < t.NumField(); i++ {
			tag := parseTag(t.Field(i))
			if tag.skip {
				continue
			}
			x, ok := obj[tag.name]
			if !ok {
				continue
			}
			fv, err := decode(x, t.Field(i).Type, path+"."+tag.name)
			if err != nil {
				return reflect.Value{}, err
			}
			v.Field(i).Set(fv)
		}
		return v, nil
	case reflect.Slice:
		if raw == nil {
			return reflect.Zero(t), nil
		}
		arr, ok := raw.([]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected an array", path)
		}
		v := reflect.MakeSlice(t, len(arr), len(arr))
		for i, x := range arr {
			e, err := decode(x, t.Elem(), fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return reflect.Value{}, err
			}
			v.Index(i).Set(e)
		}
		return v, nil
	case reflect.Map:
		obj, ok := raw.(map[string]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected an object", path)
		}
		result := reflect.MakeMapWithSize(t, len(obj))
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, err := decode(obj[key], t.Elem(), path+"["+strconv.Quote(key)+"]")
			if err != nil {
				return reflect.Value{}, err
			}
			result.SetMapIndex(reflect.ValueOf(key), value)
		}
		return result, nil
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected a string", path)
		}
		return reflect.ValueOf(s).Convert(t), nil
	case reflect.Bool:
		b, ok := raw.(bool)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected a boolean", path)
		}
		return reflect.ValueOf(b), nil
	case reflect.Int, reflect.Int32:
		n, ok := raw.(json.Number)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s: expected a number", path)
		}
		i, err := n.Int64()
		if err != nil {
			return reflect.Value{}, fmt.Errorf("%s: %w", path, err)
		}
		if reflect.Zero(t).OverflowInt(i) {
			return reflect.Value{}, fmt.Errorf("%s: integer %s is out of range for %s", path, n, t)
		}
		return reflect.ValueOf(i).Convert(t), nil
	}
	return reflect.Value{}, fmt.Errorf("%s: cannot decode into %s", path, t)
}
