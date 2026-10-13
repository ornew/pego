package grammar

import "reflect"

// Clone returns an independent copy of g, including source metadata and raw
// formatter trivia. Shared AST children stay shared within the copy. It does
// not validate the grammar; pointer cycles are preserved for Validate to report.
// Values must use the supported AST types and attribute argument types. Arbitrary
// application objects and cyclic attribute maps or slices are not supported.
func (g *Grammar) Clone() *Grammar {
	if g == nil {
		return nil
	}
	return cloneValue(reflect.ValueOf(g), make(map[any]reflect.Value)).Interface().(*Grammar)
}

func cloneValue(v reflect.Value, seen map[any]reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		if copy, ok := seen[v.Interface()]; ok {
			return copy
		}
		copy := reflect.New(v.Type().Elem())
		seen[v.Interface()] = copy
		copy.Elem().Set(cloneValue(v.Elem(), seen))
		return copy
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		copy := reflect.New(v.Type()).Elem()
		copy.Set(cloneValue(v.Elem(), seen))
		return copy
	case reflect.Struct:
		copy := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			copy.Field(i).Set(cloneValue(v.Field(i), seen))
		}
		return copy
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		copy := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			copy.Index(i).Set(cloneValue(v.Index(i), seen))
		}
		return copy
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		copy := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			copy.SetMapIndex(iter.Key(), cloneValue(iter.Value(), seen))
		}
		return copy
	default:
		return v
	}
}
