package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// A hidden rule is not an entry point: not the start rule, not a rule parsed by name.
func TestHiddenRulesAreNotEntryPoints(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = _a "c"
rule _a = "a" "b"`)
	for _, name := range p.Rules() {
		if strings.HasPrefix(name, "_") {
			t.Errorf("Rules lists the hidden rule %s", name)
		}
	}
	if _, err := p.Parse("_a", "ab"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("parse by a hidden name: %v", err)
	}
	if _, err := p.MarshalBinary("_a"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("marshal with a hidden start: %v", err)
	}
	if _, err := outputSchemaPrototype(p, "_a", false); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("schema of a hidden rule: %v", err)
	}
	data, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.LoadProgram(data); err != nil {
		t.Fatal(err)
	}
	// The same holds for the rule of an inline action, whose name nobody declared.
	q := mustCompile(t, `rule main = (x:"a" => $x) "b"`)
	if _, err := q.Parse("_main_1", "a"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("parse by a generated name: %v", err)
	}
	for _, g := range []struct{ src, start string }{
		{`rule main = _a
rule _a = "a"`, "_a"},
		{`rule main = (x:"a" => $x)`, "_main_1"},
	} {
		parsed, err := parseTest(g.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gentest.Generate(t, parsed, gen.GenOptions{Module: "m", Start: g.start}); err == nil || !strings.Contains(err.Error(), "hidden") {
			t.Errorf("Go generation with the start rule %s: %v", g.start, err)
		}
		if _, err := gentest.GenerateTS(parsed, gen.GenOptions{Start: g.start}); err == nil || !strings.Contains(err.Error(), "hidden") {
			t.Errorf("TypeScript generation with the start rule %s: %v", g.start, err)
		}
	}
}
