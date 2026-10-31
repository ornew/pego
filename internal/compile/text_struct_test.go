package compile

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
)

// A module whose rule converts its value to a type that is not a named node type is rejected.
func TestNodeTypeModuleValidation(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, `type Name = node[string]
rule main = name ("," name)*
rule name Name = ([a-z]+) #atomic`, Options{})
	m := *prog.BytecodeModule()
	if err := ValidateModule(&m); err != nil {
		t.Fatal(err)
	}
	m.Rules = append([]bytecode.RuleInfo(nil), m.Rules...)
	found := false
	for i := range m.Rules {
		if m.Rules[i].NodeType >= 0 {
			m.Rules[i].NodeType = m.Rules[i].Name
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no rule of a named node type")
	}
	if err := ValidateModule(&m); err == nil || !strings.Contains(err.Error(), "is not a named node type") {
		t.Fatalf("got %v", err)
	}
}
