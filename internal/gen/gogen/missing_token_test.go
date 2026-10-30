package gogen_test

import (
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestMissingTokensTypeOnlyGrammarCannotBeLowered(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testMissingTokensTypeOnlyGrammarCannotBeLowered)
}

func testMissingTokensTypeOnlyGrammarCannotBeLowered(t *testing.T) {
	prog := mustCompile(t, `type Item ~ missing
rule main = "a"`)
	if n, err := prog.Parse("main", "a"); n == nil || err != nil {
		t.Fatal(n, err)
	}
	if !prog.HasMissing {
		t.Fatal("reserved Missing reference was not recorded")
	}
	if _, err := prog.MarshalBinary("main"); err == nil {
		t.Fatal("saved a reserved type without a supported binary representation")
	}
	if _, err := gentest.GenerateCode(t, prog.Grammar, gen.GenOptions{Package: "example", Start: "main"}); err == nil {
		t.Fatal("generated unsupported Missing type")
	}
}
