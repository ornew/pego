package language_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

// structSeparatorSources spells one struct type with every accepted separator.
var structSeparatorSources = map[string]string{
	"semicolon":   "package t\ntype T = { A string; B int }\n",
	"trailing":    "package t\ntype T = { A string; B int; }\n",
	"newline":     "package t\ntype T = {\n\tA string\n\tB int\n}\n",
	"mixed":       "package t\ntype T = {\n\tA string; B int\n}\n",
	"blank lines": "package t\ntype T = {\n\n\tA string\n\n\n\tB int\n\n}\n",
	"nested":      "package t\ntype T = { A string; B int }\ntype U = { X { A string; B int } }\n",
}

func countStructFields(t *testing.T, g *grammar.Grammar) int {
	t.Helper()
	for _, d := range g.File.Decls {
		if td, ok := d.(*grammar.TypeDef); ok && td.Name == "T" {
			return len(td.Type.(*grammar.StructType).Fields)
		}
	}
	t.Fatal("type T not found")
	return 0
}

func TestStructFieldSeparators(t *testing.T) {
	p, _ := sourceGrammar(t)
	for name, src := range structSeparatorSources {
		t.Run(name, func(t *testing.T) {
			g, err := pego.ParseGrammar(src)
			if err != nil {
				t.Fatalf("hand-written frontend: %v", err)
			}
			if got := countStructFields(t, g); got != 2 {
				t.Fatalf("hand-written frontend read %d fields, want 2", got)
			}
			formatted := grammar.Format(g)
			again, err := pego.ParseGrammar(formatted)
			if err != nil || grammar.Format(again) != formatted {
				t.Fatalf("formatting is not idempotent (%v):\n%s", err, formatted)
			}
			n, err := p.Parse(src)
			if err != nil {
				t.Fatalf("source grammar: %v", err)
			}
			for _, d := range sourceDecls(t, n) {
				if d.Type() == "TypeDef" && sourceText(t, d, "Name") == "T" {
					if fs := elems(sourceField(t, sourceField(t, d, "Type"), "Fields")); len(fs) != 2 {
						t.Fatalf("source grammar read %d fields, want 2", len(fs))
					}
				}
			}
		})
	}
}

func TestStructFieldSeparatorsFormatCanonically(t *testing.T) {
	var want string
	for _, name := range []string{"newline", "mixed"} {
		g, err := pego.ParseGrammar(structSeparatorSources[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := grammar.Format(g)
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("%s formats differently:\n%s\nwant:\n%s", name, got, want)
		}
	}
	for _, name := range []string{"semicolon", "trailing"} {
		g, err := pego.ParseGrammar(structSeparatorSources[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := grammar.Format(g); !strings.Contains(got, "{ A string; B int }") {
			t.Fatalf("%s: one-line struct is not canonical:\n%s", name, got)
		}
	}
}

func TestStructFieldSeparatorsRejected(t *testing.T) {
	p, _ := sourceGrammar(t)
	for name, src := range map[string]string{
		"comma in struct type":     "package t\ntype T = { A string, B int }\n",
		"adjacent fields":          "package t\ntype T = { A string B int }\n",
		"semicolon in constructor": "package t\ntype T = { A string; B int }\nrule r T = \"x\" => T{A: \"x\"; B: 1}\n",
		"semicolon in call":        "package t\nrule r int = \"x\" => max(1; 2)\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pego.ParseGrammar(src); err == nil {
				t.Fatal("hand-written frontend accepted the source")
			}
			if _, err := p.Parse(src); err == nil {
				t.Fatal("source grammar accepted the source")
			}
		})
	}
	_, err := pego.ParseGrammar("package t\ntype T = { A string, B int }\n")
	if err == nil || !strings.Contains(err.Error(), "between struct fields") {
		t.Fatalf("error should name the struct field separator: %v", err)
	}
}

func TestConstructorAndCallStayCommaSeparated(t *testing.T) {
	src := "package t\ntype T = { A string; B int }\nrule r T = \"x\" => T{A: \"x\", B: 1}\nrule s int = \"y\" => max(1, 2)\n"
	if _, err := pego.ParseGrammar(src); err != nil {
		t.Fatalf("hand-written frontend: %v", err)
	}
	p, _ := sourceGrammar(t)
	if _, err := p.Parse(src); err != nil {
		t.Fatalf("source grammar: %v", err)
	}
}
