package engine

import (
	"slices"

	"github.com/ornew/pego/grammar"
)

func frameRefsKnown(t grammar.Term, s *scope, locals ...string) bool {
	ok := true
	capRefs(t, func(name string) {
		if slices.Contains(locals, name) {
			return
		}
		if _, found := s.slots[name]; !found {
			ok = false
		}
	})
	return ok
}

// typedFrameComment reports whether the body of the typed rule r is printed in place under the
// runtime-owned frame, a left-recursion leader or a rule that recovers, and returns the comment of
// its matcher.
func (g *generator) typedFrameComment(r *staticRule) (string, bool) {
	comment := "typed recovery body inlined"
	if r.leader {
		comment = "typed LR body inlined"
	} else if !exprRecovers(r.bodyExpr()) {
		return "", false
	}
	if !flowBodyOK(r.bodyExpr(), true, true) {
		return "", false
	}
	return comment, true
}
