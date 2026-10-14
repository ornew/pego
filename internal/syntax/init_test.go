package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestInitSourceAndJSONRoundTrip(t *testing.T) {
	src := "init {\n  n := 1\n  next := n + 1\n}\nrule first = \"a\""
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Statements) != 2 {
		t.Fatalf("statements = %d", len(g.Statements))
	}
	init, ok := g.Statements[0].(*grammar.InitDef)
	if !ok || len(init.Block.Statements) != 2 {
		t.Fatalf("init declaration = %#v", g.Statements[0])
	}
	formatted := grammar.Format(g)
	if !strings.Contains(formatted, "next := n + 1") {
		t.Fatalf("formatted initializer missing assignment: %s", formatted)
	}
	if _, err := Parse(formatted); err != nil {
		t.Fatalf("formatted source: %v\n%s", err, formatted)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grammar.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
}
