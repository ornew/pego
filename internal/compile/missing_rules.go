package compile

import (
	"fmt"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/value"
)

// markMissingRules records transitive insertion dependencies before matchers or
// value-free twins are compiled. Unrelated rules retain completed memo reuse.
func (prog *Program) markMissingRules(control *Control) {
	var usesMissingType func(grammar.TypeExpr) bool
	usesMissingType = func(t grammar.TypeExpr) bool {
		control.poll()
		switch t := t.(type) {
		case *grammar.TypeRef:
			return t.Name == value.TypeMissing
		case *grammar.ListType:
			return usesMissingType(t.Elem)
		case *grammar.OptionalType:
			return usesMissingType(t.Elem)
		case *grammar.TypeGroup:
			return usesMissingType(t.Type)
		case *grammar.JoinType:
			for _, t := range t.Types {
				if usesMissingType(t) {
					return true
				}
			}
		case *grammar.MeetType:
			for _, t := range t.Types {
				if usesMissingType(t) {
					return true
				}
			}
		case *grammar.TupleType:
			for _, t := range t.Types {
				if usesMissingType(t) {
					return true
				}
			}
		case *grammar.TypeApp:
			for _, t := range t.Args {
				if usesMissingType(t) {
					return true
				}
			}
		case *grammar.StructType:
			for _, f := range t.Fields {
				if usesMissingType(f.Type) {
					return true
				}
			}
		}
		return false
	}
	for _, td := range control.types(prog.Grammar) {
		switch t := td.(type) {
		case *grammar.TypeDef:
			for _, f := range DefFields(t) {
				if usesMissingType(f.Type) {
					prog.HasMissing = true
				}
			}
		case *grammar.TypeAlias:
			if usesMissingType(t.Type) {
				prog.HasMissing = true
			}
		}
	}
	refs := make(map[*Rule][]string)
	for _, r := range prog.RuleList {
		if usesMissingType(r.Def.Type) {
			prog.HasMissing = true
		}
		walkExprControlled(r.Def.Expr, func(e grammar.Expr) {
			control.poll()
			switch e := e.(type) {
			case *grammar.Attributed:
				for _, a := range e.Attrs {
					if a.Name == "missing" {
						r.Missing = true
						prog.HasMissing = true
					}
				}
			case *grammar.Ref:
				refs[r] = append(refs[r], e.Name)
			}
		}, control)
	}
	for changed := true; changed; {
		changed = false
		for _, r := range prog.RuleList {
			control.poll()
			if r.Missing {
				continue
			}
			for _, name := range refs[r] {
				control.poll()
				if callee := prog.ByName[name]; callee != nil && callee.Missing {
					r.Missing = true
					changed = true
					break
				}
			}
		}
	}
}

// MissingUnsupported returns the error of an operation that does not support missing-token
// recovery.
func MissingUnsupported(operation string) error {
	return fmt.Errorf("missing-token recovery is not supported for %s; use Closure whole-input parsing or recognition", operation)
}
