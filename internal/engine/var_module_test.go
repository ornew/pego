package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
	"github.com/ornew/zeroparse/internal/compile"
)

// varModuleSource declares variables of each type, reads them in predicates and rebinds them.
const varModuleSource = `var n int = 3
var s string = "x"
var b bool
rule main = {n := n + 1} {s := s + "y"} {n > 2 && s != "" && !b} item "a"
rule item = {n > 0} "b"?
`

// TestLoadedVariablesMatch checks that a compiled grammar, which holds one module and no syntax
// tree, loads exactly the variables of its module in each mode: a changed variable table is the
// one the loaded program runs with, or the file is rejected when the module validation rejects it.
func TestLoadedVariablesMatch(t *testing.T) {
	t.Parallel()
	good, err := mustCompile(t, varModuleSource).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(good); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(m *bytecode.Module)
	}{
		{"initial int", func(m *bytecode.Module) { m.Vars[0].Int = 4 }},
		{"initial string", func(m *bytecode.Module) { m.Vars[1].Str = m.Vars[1].Name }},
		{"initial bool", func(m *bytecode.Module) { m.Vars[2].Int = 1 }},
		// The mutations below leave a module that every other check accepts, so only the
		// comparison of the variable tables rejects them.
		{"type", func(m *bytecode.Module) { m.Vars[0].Type, m.Vars[0].Int, m.Vars[0].Str = bytecode.VarBool, 1, -1 }},
		{"name", func(m *bytecode.Module) { m.Vars[0].Name, m.Vars[1].Name = m.Vars[1].Name, m.Vars[0].Name }},
		{"count", func(m *bytecode.Module) {
			m.Strings = append(append([]string(nil), m.Strings...), "extra")
			m.Vars = append(m.Vars, bytecode.VarInfo{Name: len(m.Strings) - 1, Type: bytecode.VarInt, Str: -1})
		}},
	} {
		for _, mode := range allModes {
			t.Run(mode.String()+" "+tc.name, func(t *testing.T) {
				p := mustCompile(t, varModuleSource)
				saved := p
				if mode == CompiledRecognize {
					rec, err := p.Recognizer()
					if err != nil {
						t.Fatal(err)
					}
					saved = rec
				}
				m := saved.Module()
				m.Vars = append([]bytecode.VarInfo(nil), m.Vars...)
				tc.mutate(m)
				data, err := p.Marshal("main", mode)
				if err != nil {
					t.Fatal(err)
				}
				loaded, _, err := LoadProgram(data)
				if verr := compile.ValidateModule(m); verr != nil {
					if err == nil {
						t.Fatalf("a module that fails validation (%v) was loaded", verr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := sameVars(loaded.Vars, compile.ModuleVarTable(m)); err != nil {
					t.Fatalf("the loaded variables differ from the module: %v", err)
				}
			})
		}
	}
}

// sameVars reports the first difference between the variables of two tables: their number, names,
// types and initial values.
func sameVars(a, b *compile.VarTable) error {
	x, y := a.All(), b.All()
	if len(x) != len(y) {
		return fmt.Errorf("%d variables in one table and %d in the other", len(x), len(y))
	}
	for i := range x {
		if x[i].Name != y[i].Name || x[i].Typ != y[i].Typ || x[i].Init != y[i].Init {
			return fmt.Errorf("variable %s (%s = %v) differs from variable %s (%s = %v)",
				x[i].Name, x[i].Typ, x[i].Init, y[i].Name, y[i].Typ, y[i].Init)
		}
	}
	return nil
}
