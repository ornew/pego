package cel

import "github.com/ornew/zeroparse/parsers/cel/types"

// Constant returns the Go value of an expression that is a literal, or a list or a map of literals, in parentheses
// or not: an int64, uint64, float64, string, []byte, bool or nil (null), a []any for a list, and a map[any]any for a
// map. The second result is false for anything else: an operator, a name, a call, a message, an optional element or
// entry, and a literal out of range.
//
// A key of a map is the value of its key expression, so keys of different types do not collide (1 and 1u are different
// keys); of two equal keys the last wins.
func Constant(e types.Expr) (any, bool) {
	switch e := e.(type) {
	case *types.Paren:
		return Constant(e.X)
	case *types.IntLit:
		v, err := e.Value()
		return v, err == nil
	case *types.UintLit:
		v, err := e.Value()
		return v, err == nil
	case *types.DoubleLit:
		v, err := e.Value()
		return v, err == nil
	case *types.StringLit:
		return e.Value(), true
	case *types.BytesLit:
		return e.Value(), true
	case *types.BoolLit:
		return e.Value(), true
	case *types.NullLit:
		return nil, true
	case *types.ListLit:
		l := make([]any, len(e.Elems))
		for i, x := range e.Elems {
			v, ok := Constant(x)
			if !ok {
				return nil, false
			}
			l[i] = v
		}
		return l, true
	case *types.MapLit:
		m := make(map[any]any, len(e.Entries))
		for _, en := range e.Entries {
			if en.Optional {
				return nil, false
			}
			k, ok := Constant(en.Key)
			if !ok || !hashable(k) {
				return nil, false
			}
			v, ok := Constant(en.Value)
			if !ok {
				return nil, false
			}
			m[k] = v
		}
		return m, true
	}
	return nil, false
}

// hashable reports whether v can be the key of a map: the keys of a CEL map are bools, ints, uints and strings.
func hashable(v any) bool {
	switch v.(type) {
	case int64, uint64, bool, string:
		return true
	}
	return false
}
