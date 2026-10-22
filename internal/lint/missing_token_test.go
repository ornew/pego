package lint

import (
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/syntax"
)

func TestMissingTokenDoesNotProveAChoiceShadowed(t *testing.T) {
	// Insertion permits the first alternative to match a different source
	// prefix. A strict literal-prefix proof must not discard that alternative.
	g, err := syntax.Parse(`rule main = "a" / "ab" #missing`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Compile(starttest.MarkAll(g), engine.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, finding := range Run(g, "main", Options{}) {
		if finding.Check == "shadowed-alternative" {
			t.Fatal(finding)
		}
	}
}
