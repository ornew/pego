package grammar

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// TypeRefs returns the type references in the tree rooted at node, such as a *Grammar, in the
// order of the tree: in type definitions, rule types, casts, list constructors and every other
// place where a type is written. A reference shared by several places is returned once.
func TypeRefs(node any) []*TypeRef {
	var out []*TypeRef
	seen := map[uintptr]bool{}
	var visit func(v reflect.Value)
	visit = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				visit(v.Elem())
			}
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			if r, ok := v.Interface().(*TypeRef); ok {
				out = append(out, r)
				return
			}
			visit(v.Elem())
		case reflect.Struct:
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					visit(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				visit(v.Index(i))
			}
		case reflect.Map:
			keys := v.MapKeys()
			slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
			for _, k := range keys {
				visit(v.MapIndex(k))
			}
		}
	}
	visit(reflect.ValueOf(node))
	return out
}
