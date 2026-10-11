package grammar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

// JSONDecodeError describes a strict grammar JSON intake error. Offset is a
// zero-based byte offset into the input. For duplicate and unknown keys it
// points to the offending key token; syntax errors retain their encoding/json
// cause through Unwrap.
type JSONDecodeError struct {
	Path   string
	Offset int
	Msg    string
	Cause  error
}

func (e *JSONDecodeError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("grammar JSON at byte %d: %s", e.Offset, e.Msg)
	}
	return fmt.Sprintf("%s at byte %d: %s", e.Path, e.Offset, e.Msg)
}

// Unwrap returns the underlying JSON syntax error, if any.
func (e *JSONDecodeError) Unwrap() error { return e.Cause }

type strictJSONMember struct {
	key    string
	keyPos int
	value  *strictJSONValue
}

type strictJSONValue struct {
	value    any
	object   []strictJSONMember
	array    []*strictJSONValue
	keyPos   int
	isObject bool
	isArray  bool
}

// DecodeJSON decodes one complete grammar JSON value, rejects unknown fields
// and duplicate object keys, and validates the resulting AST. UnmarshalJSON
// retains its existing permissive behavior.
func DecodeJSON(data []byte) (*Grammar, error) {
	// First use the whole-value decoder so syntax, trailing-data and nesting
	// behavior match encoding/json. The token pass below only retains key
	// occurrences and offsets that decoding into maps would discard.
	whole := json.NewDecoder(bytes.NewReader(data))
	whole.UseNumber()
	var raw any
	if err := whole.Decode(&raw); err != nil {
		return nil, strictSyntaxError(err, "$", data, int(whole.InputOffset()))
	}
	trailingStart := skipJSONSpace(data, int(whole.InputOffset()))
	var extra any
	if err := whole.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &JSONDecodeError{Path: "$", Offset: trailingStart, Msg: "trailing JSON value"}
		}
		return nil, strictSyntaxError(err, "$", data, trailingStart)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	root, err := parseStrictJSON(dec, data, "$", 0)
	if err != nil {
		return nil, err
	}
	if err := validateStrictJSON(root, reflect.TypeOf((*Grammar)(nil)), "$", data); err != nil {
		return nil, err
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

func parseStrictJSON(dec *json.Decoder, data []byte, path string, valueKeyPos int) (*strictJSONValue, error) {
	start := skipJSONSpace(data, int(dec.InputOffset()))
	tok, err := dec.Token()
	if err != nil {
		return nil, strictSyntaxError(err, path, data, start)
	}
	v := &strictJSONValue{keyPos: valueKeyPos}
	switch tok := tok.(type) {
	case json.Delim:
		switch tok {
		case '{':
			v.isObject = true
			seen := make(map[string]struct{})
			for dec.More() {
				keyPos := skipJSONKeySpace(data, int(dec.InputOffset()))
				kt, err := dec.Token()
				if err != nil {
					return nil, strictSyntaxError(err, path, data, keyPos)
				}
				key, ok := kt.(string)
				if !ok {
					return nil, &JSONDecodeError{Path: path, Offset: keyPos, Msg: "object key is not a string"}
				}
				memberPath := strictPathKey(path, key)
				if _, dup := seen[key]; dup {
					return nil, &JSONDecodeError{Path: memberPath, Offset: keyPos, Msg: "duplicate object key"}
				}
				seen[key] = struct{}{}
				child, err := parseStrictJSON(dec, data, memberPath, keyPos)
				if err != nil {
					return nil, err
				}
				v.object = append(v.object, strictJSONMember{key: key, keyPos: keyPos, value: child})
			}
			if _, err := dec.Token(); err != nil {
				return nil, strictSyntaxError(err, path, data, skipJSONSpace(data, int(dec.InputOffset())))
			}
		case '[':
			v.isArray = true
			for dec.More() {
				indexPath := fmt.Sprintf("%s[%d]", path, len(v.array))
				child, err := parseStrictJSON(dec, data, indexPath, skipJSONSpace(data, int(dec.InputOffset())))
				if err != nil {
					return nil, err
				}
				v.array = append(v.array, child)
			}
			if _, err := dec.Token(); err != nil {
				return nil, strictSyntaxError(err, path, data, skipJSONSpace(data, int(dec.InputOffset())))
			}
		default:
			return nil, &JSONDecodeError{Path: path, Offset: start, Msg: "unexpected closing delimiter"}
		}
	default:
		v.value = tok
	}
	return v, nil
}

func strictSyntaxError(err error, path string, data []byte, fallback int) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return &JSONDecodeError{Path: path, Offset: len(data), Msg: "unexpected end of JSON input", Cause: err}
	}
	offset := fallback
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) && syntax.Offset > 0 {
		offset = int(syntax.Offset - 1)
	}
	return &JSONDecodeError{Path: path, Offset: offset, Msg: err.Error(), Cause: err}
}

func validateStrictJSON(v *strictJSONValue, t reflect.Type, path string, data []byte) error {
	if t.Kind() == reflect.Interface {
		if v == nil || !v.isObject {
			return nil // Existing decode reports the expected-object/type error.
		}
		typeMember := strictMember(v, "@type")
		name, ok := "", false
		if typeMember != nil {
			name, ok = typeMember.value.value.(string)
		}
		st, found := registry[name]
		if !ok || !found {
			pos := v.keyPos
			if typeMember != nil {
				pos = typeMember.keyPos
			}
			return &JSONDecodeError{Path: strictPathKey(path, "@type"), Offset: pos, Msg: fmt.Sprintf("unknown @type %q", name)}
		}
		p := reflect.PointerTo(st)
		if !p.Implements(t) {
			pos := v.keyPos
			if typeMember != nil {
				pos = typeMember.keyPos
			}
			return &JSONDecodeError{Path: strictPathKey(path, "@type"), Offset: pos, Msg: fmt.Sprintf("%s is not a %s", name, t.Name())}
		}
		allowed := map[string]reflect.Type{}
		for i := 0; i < st.NumField(); i++ {
			tag := parseTag(st.Field(i))
			if !tag.skip {
				allowed[tag.name] = st.Field(i).Type
			}
		}
		for _, member := range v.object {
			if member.key == "@type" {
				continue
			}
			ft, exists := allowed[member.key]
			if !exists {
				return unknownStrictKey(member, path, data)
			}
			if err := validateStrictJSON(member.value, ft, strictPathKey(path, member.key), data); err != nil {
				return err
			}
		}
		return nil
	}
	if t.Kind() == reflect.Pointer {
		if v == nil || v.value == nil && !v.isObject && !v.isArray {
			return nil
		}
		return validateStrictJSON(v, t.Elem(), path, data)
	}
	if t.Kind() == reflect.Struct {
		if v == nil || !v.isObject {
			return nil
		}
		allowed := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			tag := parseTag(t.Field(i))
			if !tag.skip {
				allowed[tag.name] = t.Field(i).Type
			}
		}
		for _, member := range v.object {
			ft, exists := allowed[member.key]
			if !exists {
				return unknownStrictKey(member, path, data)
			}
			if err := validateStrictJSON(member.value, ft, strictPathKey(path, member.key), data); err != nil {
				return err
			}
		}
		return nil
	}
	if t.Kind() == reflect.Slice {
		if v == nil || !v.isArray {
			return nil
		}
		for i, child := range v.array {
			if err := validateStrictJSON(child, t.Elem(), fmt.Sprintf("%s[%d]", path, i), data); err != nil {
				return err
			}
		}
	}
	return nil
}

func unknownStrictKey(m strictJSONMember, objectPath string, data []byte) error {
	return &JSONDecodeError{Path: strictPathKey(objectPath, m.key), Offset: m.keyPos, Msg: fmt.Sprintf("unknown field %q", m.key)}
}

func strictMember(v *strictJSONValue, key string) *strictJSONMember {
	for i := range v.object {
		if v.object[i].key == key {
			return &v.object[i]
		}
	}
	return nil
}

func strictPathKey(path, key string) string {
	if key != "" {
		valid := true
		for i, r := range key {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
				valid = false
				break
			}
		}
		if valid {
			return path + "." + key
		}
	}
	return path + "[" + strconv.Quote(key) + "]"
}

func skipJSONSpace(data []byte, offset int) int {
	for offset < len(data) {
		switch data[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		default:
			return offset
		}
	}
	return offset
}

func skipJSONKeySpace(data []byte, offset int) int {
	offset = skipJSONSpace(data, offset)
	if offset < len(data) && data[offset] == ',' {
		offset++
	}
	return skipJSONSpace(data, offset)
}
