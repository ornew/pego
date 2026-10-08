package duckdb

import "reflect"

// SpanOf returns the range of input that a value of the syntax tree covers. It reports false for a value that is
// not a node of the tree.
func SpanOf(node any) (Span, bool) {
	v := reflect.ValueOf(node)
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return Span{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return Span{}, false
	}
	f := v.FieldByName("Span")
	if !f.IsValid() {
		return Span{}, false
	}
	s, ok := f.Interface().(Span)
	return s, ok
}

var spanType = reflect.TypeOf(Span{})

// Walk calls fn for node and for every node below it, in the order of the input: a node before its children,
// the children in the order of their fields. The nodes are the pointers to the structs and terminals of the
// syntax tree. If fn returns false, Walk does not visit the children of the node.
func Walk(node any, fn func(node any) bool) {
	walk(reflect.ValueOf(node), fn)
}

func walk(v reflect.Value, fn func(node any) bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem(), fn)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		e := v.Elem()
		if e.Kind() != reflect.Struct {
			return
		}
		if !fn(v.Interface()) {
			return
		}
		t := e.Type()
		for i := 0; i < e.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous && f.Type == spanType || !f.IsExported() {
				continue
			}
			walk(e.Field(i), fn)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), fn)
		}
	}
}
