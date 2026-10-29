package compile

import "github.com/ornew/pego/grammar"

// nodeMembers are the members of a node, and the removed ones that the checker reports: they are
// not captures, which are read with x.$name.
var nodeMembers = map[string]bool{"Start": true, "End": true, "Value": true, "startPos": true, "endPos": true, "children": true}

// builtins maps built-in functions to their number of arguments (-1 for variadic).
var builtins = map[string]int{
	"len": 1, "text": 1, "foldl": 3, "foldr": 3, "map": 2, "list": -1, "concat": -1, "filter": 2,
}

// CheckTerm resolves the names in a value expression and reports statically detectable errors.
func (c *Compiler) CheckTerm(t grammar.Term, s *Scope, locals []string) {
	c.Control.poll()
	switch t := t.(type) {
	case *grammar.CaptureRef:
		if contains(locals, t.Name) {
			return
		}
		if _, ok := s.Slots[t.Name]; !ok {
			for o := s.outer; o != nil; o = o.outer {
				if _, ok := o.Slots[t.Name]; ok {
					c.errorf(t.Diagnostic(), "capture $%s is not visible in a repetition element, which sees only its own captures; define a variable from it before the repetition", t.Name)
					return
				}
			}
			c.errorf(t.Diagnostic(), "undefined capture $%s", t.Name)
		}
	case *grammar.IndexRef:
		c.errorf(t.Diagnostic(), "positional capture $%d is removed; name the element with a capture", t.Index)
	case *grammar.VarRef:
		if c.Prog.Vars.lookup(t.Name) == nil {
			c.errorf(t.Diagnostic(), "undefined variable %s; declare it with var %s at the top level", t.Name, t.Name)
		}
	case *grammar.Member:
		c.CheckTerm(t.X, s, locals)
	case *grammar.New:
		for _, x := range t.Args {
			c.CheckTerm(x, s, locals)
		}
		if _, ok := c.Prog.NodeTypeOf(t.Type); ok {
			if len(t.Fields) != 0 || len(t.Args) != 1 {
				c.errorf(t.Diagnostic(), "a node type constructor takes one value: write %s{x}", t.Type)
			}
			for _, fi := range t.Fields {
				c.CheckTerm(fi.Value, s, locals)
			}
			return
		}
		_, st := c.Prog.StructType(t.Type)
		if st == nil {
			c.errorf(t.Diagnostic(), "%s is not a struct type or a node type", t.Type)
		} else if len(t.Args) != 0 {
			if f := FieldOf(st, "Text"); f != nil && len(t.Args) == 1 {
				c.errorf(t.Diagnostic(), "a struct constructor names its fields: write %s{Text: ...} (pego fix rewrites %s{...})", t.Type, t.Type)
			} else {
				c.errorf(t.Diagnostic(), "a struct constructor names its fields: write %s{Field: value}", t.Type)
			}
		}
		seen := map[string]bool{}
		for _, fi := range t.Fields {
			if seen[fi.Name] {
				c.errorf(fi.Diagnostic(), "duplicate field %s", fi.Name)
			}
			seen[fi.Name] = true
			if st != nil && FieldOf(st, fi.Name) == nil {
				c.errorf(fi.Diagnostic(), "%s has no field %s", t.Type, fi.Name)
			}
			c.CheckTerm(fi.Value, s, locals)
		}
	case *grammar.ListConstructor:
		k := &TypeResolver{control: c.Control, Prog: c.Prog, errs: &c.errs, report: true}
		reported := len(c.errs)
		elem := k.Resolve(t.Elem)
		if len(c.errs) > reported || k.sawUnsupported {
			// The element type is in error already.
		} else if elem == tyNever {
			c.errorf(t.Diagnostic(), "the list element has type _|_, which has no values")
		} else if !concreteListElement(elem) {
			c.errorf(t.Diagnostic(), "list constructors require a concrete element type, got %s", elem)
		}
		for _, item := range t.Items {
			c.CheckTerm(item, s, locals)
		}
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			c.CheckTerm(x, s, locals)
		}
	case *grammar.Call:
		n, ok := builtins[t.Func]
		if !ok {
			c.errorf(t.Diagnostic(), "unknown function %s", t.Func)
		} else if n >= 0 && n != len(t.Args) {
			c.errorf(t.Diagnostic(), "%s takes %d arguments, got %d", t.Func, n, len(t.Args))
		}
		for _, a := range t.Args {
			c.CheckTerm(a, s, locals)
		}
	case *grammar.Lambda:
		c.CheckTerm(t.Body, s, append(append([]string(nil), locals...), t.Params...))
	case *grammar.Binary:
		c.CheckTerm(t.L, s, locals)
		c.CheckTerm(t.R, s, locals)
	case *grammar.Unary:
		c.CheckTerm(t.X, s, locals)
	case *grammar.Cast:
		k := &TypeResolver{control: c.Control, Prog: c.Prog, errs: &c.errs, report: true}
		k.resolveLocation(t.Type, t.Diagnostic(), "the cast")
		c.CheckTerm(t.X, s, locals)
	case *grammar.Bind:
		c.errorf(t.Diagnostic(), "binding %s := ... is only allowed as a whole predicate", t.Name)
	}
}

// FieldOf returns the field name of the struct type st, or nil if st has no such field.
func FieldOf(st *grammar.TypeDef, name string) *grammar.Field {
	for _, f := range DefFields(st) {
		if f.Name == name {
			return f
		}
	}
	return nil
}
