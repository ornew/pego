package engine

import (
	"fmt"
	"strings"
	"testing"
)

// varModuleSource declares variables of each type, reads them in predicates and rebinds them.
const varModuleSource = `var n int = 3
var s string = "x"
var b bool
rule main = {n := n + 1} {s := s + "y"} {n > 2 && s != "" && !b} item "a"
rule item = {n > 0} "b"?
`

// TestModuleVariableValidation checks that a module whose code names a variable that its table
// does not declare is rejected, wherever the name appears.
func TestModuleVariableValidation(t *testing.T) {
	fresh := func() *Module {
		m := *compile(t, varModuleSource).Module()
		m.Strings = append([]string(nil), m.Strings...)
		m.Exprs = append([]Instr(nil), m.Exprs...)
		m.Code = append([]Instr(nil), m.Code...)
		m.Rules = append([]RuleInfo(nil), m.Rules...)
		return &m
	}
	if err := validateModule(fresh()); err != nil {
		t.Fatal(err)
	}
	undeclared := func(m *Module) int {
		m.Strings = append(m.Strings, "ghost")
		return len(m.Strings) - 1
	}
	for _, tc := range []struct {
		name   string
		mutate func(m *Module) bool
	}{
		{"read", func(m *Module) bool {
			for i := range m.Exprs {
				if m.Exprs[i].Op == EVar {
					m.Exprs[i].A = int32(undeclared(m))
					return true
				}
			}
			return false
		}},
		{"binding", func(m *Module) bool {
			for i := range m.Code {
				if m.Code[i].Op == OpBind {
					m.Code[i].A = int32(undeclared(m))
					return true
				}
			}
			return false
		}},
		{"rule variables", func(m *Module) bool {
			for i := range m.Rules {
				if len(m.Rules[i].Vars) != 0 {
					m.Rules[i].Vars = []int{undeclared(m)}
					return true
				}
			}
			return false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fresh()
			if !tc.mutate(m) {
				t.Fatal("nothing to mutate")
			}
			if err := validateModule(m); err == nil || !strings.Contains(err.Error(), `variable "ghost" is not declared`) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// TestLoadedVariablesMatch checks that a compiled grammar, which holds one module and no syntax
// tree, loads exactly the variables of its module in each mode: a changed variable table is the
// one the loaded program runs with, or the file is rejected when the module validation rejects it.
func TestLoadedVariablesMatch(t *testing.T) {
	good, err := compile(t, varModuleSource).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(good); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(m *Module)
	}{
		{"initial int", func(m *Module) { m.Vars[0].Int = 4 }},
		{"initial string", func(m *Module) { m.Vars[1].Str = m.Vars[1].Name }},
		{"initial bool", func(m *Module) { m.Vars[2].Int = 1 }},
		// The mutations below leave a module that every other check accepts, so only the
		// comparison of the variable tables rejects them.
		{"type", func(m *Module) { m.Vars[0].Type, m.Vars[0].Int, m.Vars[0].Str = varBool, 1, -1 }},
		{"name", func(m *Module) { m.Vars[0].Name, m.Vars[1].Name = m.Vars[1].Name, m.Vars[0].Name }},
		{"count", func(m *Module) {
			m.Strings = append(append([]string(nil), m.Strings...), "extra")
			m.Vars = append(m.Vars, VarInfo{Name: len(m.Strings) - 1, Type: varInt, Str: -1})
		}},
	} {
		for _, mode := range allModes {
			t.Run(mode.String()+" "+tc.name, func(t *testing.T) {
				p := compile(t, varModuleSource)
				saved := p
				if mode == CompiledRecognize {
					rec, err := p.recognizer()
					if err != nil {
						t.Fatal(err)
					}
					saved = rec
				}
				m := saved.Module()
				m.Vars = append([]VarInfo(nil), m.Vars...)
				tc.mutate(m)
				data, err := p.Marshal("main", mode)
				if err != nil {
					t.Fatal(err)
				}
				loaded, _, err := LoadProgram(data)
				if verr := validateModule(m); verr != nil {
					if err == nil {
						t.Fatalf("a module that fails validation (%v) was loaded", verr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := sameVars(loaded.vars, moduleVarTable(m)); err != nil {
					t.Fatalf("the loaded variables differ from the module: %v", err)
				}
			})
		}
	}
}

// TestModuleInitialValueMatchesType checks that a module whose variable holds an initial value of
// another type than its declared one is rejected: an int or bool variable with a string, a string
// variable with an int or without a string, a bool that is neither 0 nor 1, and an unknown type.
func TestModuleInitialValueMatchesType(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(m *Module)
	}{
		{"string for an int", func(m *Module) { m.Vars[0].Str = m.Vars[1].Str }},
		{"int for a string", func(m *Module) { m.Vars[1].Int = 5 }},
		{"no string for a string", func(m *Module) { m.Vars[1].Str = -1 }},
		{"2 for a bool", func(m *Module) { m.Vars[2].Int = 2 }},
		{"a string for a bool", func(m *Module) { m.Vars[2].Str = m.Vars[1].Str }},
		{"unknown type", func(m *Module) { m.Vars[0].Type = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := *compile(t, varModuleSource).Module()
			m.Vars = append([]VarInfo(nil), m.Vars...)
			tc.mutate(&m)
			if err := validateModule(&m); err == nil {
				t.Fatal("a module with a mistyped initial value was accepted")
			}
		})
	}
}

// sameVars reports the first difference between the variables of two tables: their number, names,
// types and initial values.
func sameVars(a, b *varTable) error {
	x, y := a.all(), b.all()
	if len(x) != len(y) {
		return fmt.Errorf("%d variables in one table and %d in the other", len(x), len(y))
	}
	for i := range x {
		if x[i].name != y[i].name || x[i].typ != y[i].typ || x[i].init != y[i].init {
			return fmt.Errorf("variable %s (%s = %v) differs from variable %s (%s = %v)",
				x[i].name, x[i].typ, x[i].init, y[i].name, y[i].typ, y[i].init)
		}
	}
	return nil
}
