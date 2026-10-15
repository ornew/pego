package engine

import "github.com/ornew/pego/grammar"

// Copies use private destination storage, so cancellation never changes the
// source AST. Allocations themselves remain nonpreemptible.
func normalizationCopy[T any](dst, src []T, control *compileControl) {
	if control == nil {
		copy(dst, src)
		return
	}
	for off, end := 0, min(len(dst), len(src)); off < end; {
		control.check()
		next := min(off+256, end)
		copy(dst[off:next], src[off:next])
		off = next
	}
}

func normalizationClone[T any](src []T, control *compileControl) []T {
	if control == nil {
		return append([]T(nil), src...)
	}
	control.check()
	if len(src) == 0 {
		return nil
	}
	out := make([]T, len(src))
	normalizationCopy(out, src, control)
	return out
}

func appendNormalizedAttribute(src []*grammar.Attribute, a *grammar.Attribute, control *compileControl) []*grammar.Attribute {
	if control == nil {
		return append(append([]*grammar.Attribute(nil), src...), a)
	}
	control.check()
	out := make([]*grammar.Attribute, len(src)+1)
	normalizationCopy(out, src, control)
	out[len(src)] = a
	return out
}

func appendNormalizedPredicates(dst, src []grammar.Expr, control *compileControl) []grammar.Expr {
	if control == nil {
		return append(dst, src...)
	}
	for off := 0; off < len(src); {
		control.check()
		next := min(off+256, len(src))
		dst = append(dst, src[off:next]...)
		off = next
	}
	return dst
}
