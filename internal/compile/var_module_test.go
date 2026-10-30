package compile

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
)

// TestModuleVariableValidation checks that a module whose code names a variable that its table
// does not declare is rejected, wherever the name appears.
func TestModuleVariableValidation(t *testing.T) {
	t.Parallel()
	fresh := func() *bytecode.Module {
		m := *mustCompile(t, varModuleSource).BytecodeModule()
		m.Strings = append([]string(nil), m.Strings...)
		m.Exprs = append([]bytecode.Instr(nil), m.Exprs...)
		m.Code = append([]bytecode.Instr(nil), m.Code...)
		m.Rules = append([]bytecode.RuleInfo(nil), m.Rules...)
		return &m
	}
	if err := ValidateModule(fresh()); err != nil {
		t.Fatal(err)
	}
	undeclared := func(m *bytecode.Module) int {
		m.Strings = append(m.Strings, "ghost")
		return len(m.Strings) - 1
	}
	for _, tc := range []struct {
		name   string
		mutate func(m *bytecode.Module) bool
	}{
		{"read", func(m *bytecode.Module) bool {
			for i := range m.Exprs {
				if m.Exprs[i].Op == bytecode.EVar {
					m.Exprs[i].A = int32(undeclared(m))
					return true
				}
			}
			return false
		}},
		{"binding", func(m *bytecode.Module) bool {
			for i := range m.Code {
				if m.Code[i].Op == bytecode.OpBind {
					m.Code[i].A = int32(undeclared(m))
					return true
				}
			}
			return false
		}},
		{"rule variables", func(m *bytecode.Module) bool {
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
			if err := ValidateModule(m); err == nil || !strings.Contains(err.Error(), `variable "ghost" is not declared`) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// TestModuleInitialValueMatchesType checks that a module whose variable holds an initial value of
// another type than its declared one is rejected: an int or bool variable with a string, a string
// variable with an int or without a string, a bool that is neither 0 nor 1, and an unknown type.
func TestModuleInitialValueMatchesType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(m *bytecode.Module)
	}{
		{"string for an int", func(m *bytecode.Module) { m.Vars[0].Str = m.Vars[1].Str }},
		{"int for a string", func(m *bytecode.Module) { m.Vars[1].Int = 5 }},
		{"no string for a string", func(m *bytecode.Module) { m.Vars[1].Str = -1 }},
		{"2 for a bool", func(m *bytecode.Module) { m.Vars[2].Int = 2 }},
		{"a string for a bool", func(m *bytecode.Module) { m.Vars[2].Str = m.Vars[1].Str }},
		{"unknown type", func(m *bytecode.Module) { m.Vars[0].Type = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := *mustCompile(t, varModuleSource).BytecodeModule()
			m.Vars = append([]bytecode.VarInfo(nil), m.Vars...)
			tc.mutate(&m)
			if err := ValidateModule(&m); err == nil {
				t.Fatal("a module with a mistyped initial value was accepted")
			}
		})
	}
}
