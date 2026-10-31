package compile

import (
	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/bytecode"
)

// A parser variable is declared by a top-level var declaration. Its type is int, string or
// bool, and it starts every parse with its initial value, which is the zero value of the type
// when the declaration gives none. Actions rebind it with name := value.

// VarDecl is a declared variable.
type VarDecl struct {
	Name string
	Typ  BasicTy // tyInt, tyString or tyBool
	Init any     // the initial value: an int, a string or a bool
}

// VarTable holds the variables of a program in declaration order. A table is nil for a grammar
// without variables.
type VarTable struct {
	List   []*VarDecl
	byName map[string]*VarDecl
	// slots maps a name to its position in List once the table is large enough that a scan of
	// List is slower than a map lookup.
	slots map[string]int
}

// slotMapMin is the number of variables from which Index uses a map.
const slotMapMin = 8

// All returns the variables in declaration order.
func (t *VarTable) All() []*VarDecl {
	if t == nil {
		return nil
	}
	return t.List
}

// lookup returns the declaration of the variable name, or nil.
func (t *VarTable) lookup(name string) *VarDecl {
	if t == nil {
		return nil
	}
	return t.byName[name]
}

// Add appends a variable.
func (t *VarTable) Add(d *VarDecl) {
	t.List = append(t.List, d)
	t.byName[d.Name] = d
	if len(t.List) >= slotMapMin {
		if t.slots == nil {
			t.slots = make(map[string]int, len(t.List))
			for i, v := range t.List {
				t.slots[v.Name] = i
			}
		} else {
			t.slots[d.Name] = len(t.List) - 1
		}
	}
}

// Index returns the slot of the variable name, its position in declaration order, or -1.
func (t *VarTable) Index(name string) int {
	if t == nil {
		return -1
	}
	if t.slots != nil {
		if i, ok := t.slots[name]; ok {
			return i
		}
		return -1
	}
	for i, d := range t.List {
		if d.Name == name {
			return i
		}
	}
	return -1
}

// Slots returns the slots of the variables names, in the same order (-1 for an undeclared name).
func (t *VarTable) Slots(names []string) []int {
	if len(names) == 0 {
		return nil
	}
	slots := make([]int, len(names))
	for i, name := range names {
		slots[i] = t.Index(name)
	}
	return slots
}

// ValuesOf returns the initial values of the variables names, or nil for no names.
func (t *VarTable) ValuesOf(names []string) []any {
	if len(names) == 0 {
		return nil
	}
	vals := make([]any, len(names))
	for i, name := range names {
		if d := t.lookup(name); d != nil {
			vals[i] = d.Init
		}
	}
	return vals
}

// newVarTable returns an empty variable table.
func newVarTable() *VarTable {
	return &VarTable{byName: map[string]*VarDecl{}}
}

// zeroValue returns the initial value of a variable of type t that has no initializer.
func zeroValue(t BasicTy) any {
	switch t {
	case TyInt:
		return 0
	case TyString:
		return ""
	}
	return false
}

// valueType returns the variable type of the value v, an int, a string or a bool.
func valueType(v any) BasicTy {
	switch v.(type) {
	case int:
		return TyInt
	case string:
		return TyString
	}
	return TyBool
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
func (c *Compiler) declareVars(g *grammar.Grammar) {
	for _, d := range g.File.Decls {
		c.Control.poll()
		vd, ok := d.(*grammar.VarDef)
		if !ok {
			continue
		}
		if c.Prog.Vars == nil {
			c.Prog.Vars = newVarTable()
		}
		if c.Prog.Vars.lookup(vd.Name) != nil {
			c.errorf(vd.Diagnostic(), "variable %s is already defined", vd.Name)
			continue
		}
		decl := c.varDecl(vd)
		if decl == nil {
			// Keep the name declared, so that its uses do not add errors of their own.
			decl = &VarDecl{Name: vd.Name, Typ: TyInt, Init: 0}
		}
		c.Prog.Vars.Add(decl)
	}
}

// varDecl returns the declaration of vd, or nil after reporting why it is invalid.
func (c *Compiler) varDecl(vd *grammar.VarDef) *VarDecl {
	var typ BasicTy
	if vd.Type != nil {
		ref, ok := vd.Type.(*grammar.TypeRef)
		if ok {
			switch ref.Name {
			case "int":
				typ = TyInt
			case "string":
				typ = TyString
			case "bool":
				typ = TyBool
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
		return &VarDecl{Name: vd.Name, Typ: typ, Init: zeroValue(typ)}
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
	return &VarDecl{Name: vd.Name, Typ: valueType(v), Init: v}
}

// ModuleVarTable returns the variable table that the module m declares, or nil when it declares
// none. It reads back the table that the bytecode compiler writes.
func ModuleVarTable(m *bytecode.Module) *VarTable {
	var t *VarTable
	for _, v := range m.Vars {
		d := &VarDecl{Name: m.Strings[v.Name]}
		switch v.Type {
		case bytecode.VarInt:
			d.Typ, d.Init = TyInt, v.Int
		case bytecode.VarString:
			d.Typ, d.Init = TyString, m.Strings[v.Str]
		default:
			d.Typ, d.Init = TyBool, v.Int == 1
		}
		if t == nil {
			t = newVarTable()
		}
		t.Add(d)
	}
	return t
}
