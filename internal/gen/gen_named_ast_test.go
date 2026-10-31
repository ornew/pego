package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedNamedASTUntypedAndTS(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedNamedASTUntypedAndTS)
}

func testGeneratedNamedASTUntypedAndTS(t *testing.T) {
	g, err := parseTest(gentest.NamedASTGrammar)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "plain", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "func ParseRuleAST(") {
		t.Fatal("typed entry point in untyped parser")
	}
	ts, err := gentest.GenerateTSBundle(g, gen.GenOptions{Package: "plain", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ts), "function ParseRuleAST(") {
		t.Fatal("Go API leaked into TypeScript")
	}
}
