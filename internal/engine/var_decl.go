package engine

import (
	"github.com/ornew/pego/grammar"
)

// A parser variable is declared by a top-level var declaration. Its type is int, string or
// bool, and it starts every parse with its initial value, which is the zero value of the type
// when the declaration gives none. Actions rebind it with name := value.

// varDecl is a declared variable.
type varDecl struct {
	name string
	typ  basicTy // tyInt, tyString or tyBool
	init any     // the initial value: an int, a string or a bool
}

// varTable holds the variables of a program in declaration order. A table is nil for a grammar
// without variables.
type varTable struct {
	list   []*varDecl
	byName map[string]*varDecl
	// bound is the initial set of names that have been bound (parser.bound).
	bound map[string]bool
}

// all returns the variables in declaration order.
func (t *varTable) all() []*varDecl {
	if t == nil {
		return nil
	}
	return t.list
}

// lookup returns the declaration of the variable name, or nil.
func (t *varTable) lookup(name string) *varDecl {
	if t == nil {
		return nil
	}
	return t.byName[name]
}

// add appends a variable.
func (t *varTable) add(d *varDecl) {
	t.list = append(t.list, d)
	t.byName[d.name] = d
	t.bound[d.name] = true
}

// valuesOf returns the initial values of the variables names, or nil for no names.
func (t *varTable) valuesOf(names []string) []any {
	if len(names) == 0 {
		return nil
	}
	vals := make([]any, len(names))
	for i, name := range names {
		if d := t.lookup(name); d != nil {
			vals[i] = d.init
		}
	}
	return vals
}

func newVarTable() *varTable {
	return &varTable{byName: map[string]*varDecl{}, bound: map[string]bool{}}
}

// zeroValue returns the initial value of a variable of type t that has no initializer.
func zeroValue(t basicTy) any {
	switch t {
	case tyInt:
		return 0
	case tyString:
		return ""
	}
	return false
}

// valueType returns the variable type of the value v, an int, a string or a bool.
func valueType(v any) basicTy {
	switch v.(type) {
	case int:
		return tyInt
	case string:
		return tyString
	}
	return tyBool
}

// constantValue returns the value of an initializer: an int, a string or a bool literal, or an
// int literal with a minus sign. ok is false for any other expression.
func constantValue(t grammar.Term) (v any, ok bool) {
	switch t := t.(type) {
	case *grammar.TermGroup:
		return constantValue(t.Term)
	case *grammar.IntLit:
		return t.Value, true
	case *grammar.StringLit:
		return t.Value, true
	case *grammar.BoolLit:
		return t.Value, true
	case *grammar.Unary:
		if lit, isInt := t.X.(*grammar.IntLit); isInt && t.Op == "-" {
			return -lit.Value, true
		}
	}
	return nil, false
}

// declareVars records the variables of the grammar and reports the invalid declarations.
func (c *compiler) declareVars(g *grammar.Grammar) {
	for _, d := range g.File.Decls {
		c.control.poll()
		vd, ok := d.(*grammar.VarDef)
		if !ok {
			continue
		}
		if c.prog.vars == nil {
			c.prog.vars = newVarTable()
		}
		if c.prog.vars.lookup(vd.Name) != nil {
			c.errorf(vd.Diagnostic(), "variable %s is already defined", vd.Name)
			continue
		}
		decl := c.varDecl(vd)
		if decl == nil {
			// Keep the name declared, so that its uses do not add errors of their own.
			decl = &varDecl{name: vd.Name, typ: tyInt, init: 0}
		}
		c.prog.vars.add(decl)
	}
}

// varDecl returns the declaration of vd, or nil after reporting why it is invalid.
func (c *compiler) varDecl(vd *grammar.VarDef) *varDecl {
	var typ basicTy
	if vd.Type != nil {
		ref, ok := vd.Type.(*grammar.TypeRef)
		if ok {
			switch ref.Name {
			case "int":
				typ = tyInt
			case "string":
				typ = tyString
			case "bool":
				typ = tyBool
			default:
				ok = false
			}
		}
		if !ok {
			if ref, isRef := vd.Type.(*grammar.TypeRef); isRef {
				if to, retired := retiredTypeNames[ref.Name]; retired {
					c.errorf(vd.Diagnostic(), "%s is not a type; write %s", ref.Name, to)
					return nil
				}
			}
			c.errorf(vd.Diagnostic(), "variable %s must have type int, string, or bool, got %s", vd.Name, grammar.FormatType(vd.Type))
			return nil
		}
	}
	if vd.Value == nil {
		if vd.Type == nil {
			c.errorf(vd.Diagnostic(), "variable %s needs a type or an initial value", vd.Name)
			return nil
		}
		return &varDecl{name: vd.Name, typ: typ, init: zeroValue(typ)}
	}
	if _, isNil := vd.Value.(*grammar.NilLit); isNil {
		c.errorf(termPos(vd.Value, vd.Diagnostic()), "variable %s cannot be nil; its type is int, string, or bool", vd.Name)
		return nil
	}
	v, ok := constantValue(vd.Value)
	if !ok {
		c.errorf(termPos(vd.Value, vd.Diagnostic()), "the initial value of variable %s must be an int, string, or bool literal", vd.Name)
		return nil
	}
	if vd.Type != nil && valueType(v) != typ {
		c.errorf(termPos(vd.Value, vd.Diagnostic()), "cannot use %s as %s in the initial value of variable %s", valueType(v), typ, vd.Name)
		return nil
	}
	return &varDecl{name: vd.Name, typ: valueType(v), init: v}
}
