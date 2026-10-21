package cel_test

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// render writes a typed tree in the notation of the golden files (testdata/*.golden), which the engine
// tests print from the nodes of the same grammar: a struct is Name{Field: value, ...} with its fields in
// alphabetical order and without positions, a named node type is Name("text"), a nil is nil.
func render(v any) string {
	var b strings.Builder
	renderValue(&b, reflect.ValueOf(v))
	return b.String()
}

func renderValue(b *strings.Builder, v reflect.Value) {
	switch v.Kind() {
	case reflect.Invalid:
		b.WriteString("nil")
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		renderValue(b, v.Elem())
	case reflect.String:
		b.WriteString(strconv.Quote(v.String()))
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Slice:
		b.WriteString("[")
		for i := range v.Len() {
			if i > 0 {
				b.WriteString(" ")
			}
			renderValue(b, v.Index(i))
		}
		b.WriteString("]")
	case reflect.Struct:
		t := v.Type()
		if strings.HasPrefix(t.Name(), "TNode[") {
			renderValue(b, v.FieldByName("Value"))
			return
		}
		if t.NumField() == 1 && t.Field(0).Anonymous && strings.HasPrefix(t.Field(0).Type.Name(), "TNode[") {
			b.WriteString(t.Name() + "(")
			renderValue(b, v.Field(0).FieldByName("Value"))
			b.WriteString(")")
			return
		}
		var names []string
		for i := range t.NumField() {
			if f := t.Field(i); f.Name != "Span" && f.IsExported() {
				names = append(names, f.Name)
			}
		}
		sort.Strings(names)
		b.WriteString(t.Name() + "{")
		for i, n := range names {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(n + ": ")
			renderValue(b, v.FieldByName(n))
		}
		b.WriteString("}")
	default:
		b.WriteString("?" + v.Kind().String())
	}
}
