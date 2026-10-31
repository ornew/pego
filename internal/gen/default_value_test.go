package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestDefaultValueGeneratedRejectsTypes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testDefaultValueGeneratedRejectsTypes)
}

func testDefaultValueGeneratedRejectsTypes(t *testing.T) {
	g := mustParse("rule main = \"a\" => 1")
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true, DefaultValue: true}); err == nil || !strings.Contains(err.Error(), "default values") {
		t.Errorf("typed output with default values: %v", err)
	}
	if _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Types: true, DefaultValue: true}); err == nil || !strings.Contains(err.Error(), "default values") {
		t.Errorf("typed TypeScript output with default values: %v", err)
	}
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", DefaultValue: true}); err != nil {
		t.Errorf("generation with default values: %v", err)
	}
}
