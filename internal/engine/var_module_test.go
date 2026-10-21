package engine

import (
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
				if m.Code[i].Op == OpAssign {
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

// TestLoadedVariablesMatch checks that a saved grammar whose module disagrees with its syntax
// tree about a variable is rejected on load, and so is a recognition bundle that disagrees with
// the main module.
func TestLoadedVariablesMatch(t *testing.T) {
	good, err := compile(t, varModuleSource).MarshalBinaryWith("main", MarshalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(good, Options{}); err != nil {
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
		t.Run("syntax tree "+tc.name, func(t *testing.T) {
			p := compile(t, varModuleSource)
			m := p.Module()
			m.Vars = append([]VarInfo(nil), m.Vars...)
			tc.mutate(m)
			data, err := p.MarshalBinaryWith("main", MarshalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadProgram(data, Options{}); err == nil {
				t.Fatal("a module that differs from its syntax tree was loaded")
			}
		})
		t.Run("recognition "+tc.name, func(t *testing.T) {
			p := compile(t, varModuleSource)
			rec, err := p.recognizer()
			if err != nil {
				t.Fatal(err)
			}
			m := rec.Module()
			m.Vars = append([]VarInfo(nil), m.Vars...)
			tc.mutate(m)
			bundle := encodeRecognition(rec)
			if _, _, err := decodeRecognition(bundle, compile(t, varModuleSource).Module()); err == nil {
				t.Fatal("a bundle that differs from the main module was accepted")
			}
		})
	}
}
