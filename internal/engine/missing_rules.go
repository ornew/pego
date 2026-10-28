package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/value"
)

// markMissingRules records transitive insertion dependencies before matchers or
// value-free twins are compiled. Unrelated rules retain completed memo reuse.
func (prog *staticProgram) markMissingRules(control *compileControl) {
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
			for _, f := range defFields(t) {
				if usesMissingType(f.Type) {
					prog.hasMissing = true
				}
			}
		case *grammar.TypeAlias:
			if usesMissingType(t.Type) {
				prog.hasMissing = true
			}
		}
	}
	refs := make(map[*staticRule][]string)
	for _, r := range prog.rules {
		if usesMissingType(r.def.Type) {
			prog.hasMissing = true
		}
		walkExprControlled(r.def.Expr, func(e grammar.Expr) {
			control.poll()
			switch e := e.(type) {
			case *grammar.Attributed:
				for _, a := range e.Attrs {
					if a.Name == "missing" {
						r.missing = true
						prog.hasMissing = true
					}
				}
			case *grammar.Ref:
				refs[r] = append(refs[r], e.Name)
			}
		}, control)
	}
	for changed := true; changed; {
		changed = false
		for _, r := range prog.rules {
			control.poll()
			if r.missing {
				continue
			}
			for _, name := range refs[r] {
				control.poll()
				if callee := prog.byName[name]; callee != nil && callee.missing {
					r.missing = true
					changed = true
					break
				}
			}
		}
	}
}

func missingUnsupported(operation string) error {
	return fmt.Errorf("missing-token recovery is not supported for %s; use Closure whole-input parsing or recognition", operation)
}
