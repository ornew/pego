package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGenerateDocumentCombinations(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateDocumentCombinations)
}

func testGenerateDocumentCombinations(t *testing.T) {
	g, err := parseTest("rule main = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Document: true, Types: true})
	if err != nil || !strings.Contains(string(typed), "func (d *Document) ParseAST(") {
		t.Fatalf("typed Document generation: %v", err)
	}
	if _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Document: true}); err == nil {
		t.Fatal("TypeScript Document generation was accepted")
	}
}
