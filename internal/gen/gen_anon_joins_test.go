package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// One declared node type and error, the value of a rule that recovers, is a join with an interface
// named after the type and Error, implemented by *Block and *Error, and a union in TypeScript.
func TestGeneratedTypedAnonymousJoinOneNodeAndError(t *testing.T) {
	t.Parallel()
	types, err := gentest.GoTypesOf(t, gentest.AnonErrorJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type BlockOrError interface{ isBlockOrError() }",
		"Else BlockOrError",
		"Last BlockOrError",
		"Rest []*Block",
		"func (*Block) isBlockOrError()",
		"func (*Error) isBlockOrError()",
		"is the join type Block | error.",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("types.go lacks %q:\n%s", want, types)
		}
	}
	if n := strings.Count(types, "interface{ is"); n != 1 {
		t.Errorf("%d interfaces, want 1", n)
	}
	// An alias of the same members is the type of the field.
	types, err = gentest.GoTypesOf(t, `type Block = { Text string }
type S ~ Block | error
type R = { Else *(Block | error) }
rule main R = e:alt $$ => R{Else: $e}
rule alt Block = t:[a-z]+ ";" => Block{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(types, "Else S") || strings.Contains(types, "BlockOrError") {
		t.Errorf("the field is not the alias S:\n%s", types)
	}
	g, err := parseTest(gentest.AnonErrorJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gentest.GenerateTS(g, gen.GenOptions{Types: true, Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "types.ts" && !strings.Contains(string(f.Content), `readonly "Else": RecoveredError | null | Block;`) {
			t.Errorf("types.ts lacks the union for Else:\n%s", f.Content)
		}
	}
}
