package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

func TestTopBottomAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.TopBottomCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, input := range fixture.Inputs {
				baseline := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

// A list of _ only has to be a list value, whatever its elements.
func TestTopBottomListCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ field, want string }{
		{"[]_", "[]_"},
		{"*[]_", "*[]_"},
		{"*[]_ | int", "*([]_ | int)"},
	} {
		src := "type T = { A " + tc.field + " }\ntype U = { F _ }\nrule main = x:\"a\" => T{A: U{F: \"s\"}.F.(" + tc.field + ")}\nrule ok = x:\"a\"* => T{A: U{F: $x.Value}.F.(" + tc.field + ")}\nrule rule []_ = x:\"a\"* => U{F: $x.Value}.F.([]_)\nrule bad []_ = x:\"a\" => U{F: $x}.F.([]_)"
		for _, opts := range []compile.Options{{}} {
			program := mustCompile(t, src, opts)
			data, err := program.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data)
			if err != nil {
				t.Fatalf("%s: %v", tc.field, err)
			}
			for _, p := range []*Program{program, loaded} {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					if p == loaded && backend == Closure {
						continue // a loaded program runs on the bytecode backends
					}
					if _, err := p.ParseWith("main", "a", ParseOptions{Backend: backend}); err == nil || !strings.Contains(err.Error(), "cannot cast string to "+tc.want) {
						t.Fatalf("%s %s: %v", tc.field, backend, err)
					}
					for _, start := range []string{"ok", "rule"} {
						if _, err := p.ParseWith(start, "aa", ParseOptions{Backend: backend}); err != nil {
							t.Fatalf("%s %s %s: %v", tc.field, start, backend, err)
						}
					}
					if _, err := p.ParseWith("bad", "a", ParseOptions{Backend: backend}); err == nil || !strings.Contains(err.Error(), "cannot cast node[string] to []_") {
						t.Fatalf("%s bad %s: %v", tc.field, backend, err)
					}
				}
			}
		}
	}
}

func TestTopBottomSyntax(t *testing.T) {
	t.Parallel()
	src := "type A ~ []B & *C | _ & (D | B) | _|_\ntype E ~ [](B & C) & *(B | C)\ntype B = { Text string }\ntype C = { Text string }\ntype D = { Text string }\nrule main = x:\"a\" => $x.(_ & node[_])"
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	for _, want := range []string{"type A ~ []B & *C | _ & (D | B) | _|_", "type E ~ [](B & C) & *(B | C)", "$x.(_ & node[_])"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted lacks %q:\n%s", want, formatted)
		}
	}
	alias := g.Types()[0].(*grammar.TypeAlias).Type.(*grammar.JoinType)
	if len(alias.Types) != 3 {
		t.Fatalf("union %#v", alias)
	}
	if m, ok := alias.Types[0].(*grammar.MeetType); !ok || len(m.Types) != 2 {
		t.Fatalf("meet %#v", alias.Types[0])
	}
	if b, ok := alias.Types[2].(*grammar.TypeRef); !ok || b.Name != "_|_" {
		t.Fatalf("bottom %#v", alias.Types[2])
	}
	// The JSON AST keeps meets and the top and bottom types; compiled files keep no types.
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	back, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	// JSON keeps the tree but not the layout, so compare a second encoding.
	again, err := grammar.MarshalJSON(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) || !strings.Contains(string(data), "MeetType") {
		t.Fatalf("JSON round trip:\n%s\nwant\n%s", again, data)
	}
	program := mustCompile(t, src)
	bin, err := program.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(bin)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Grammar != nil || bytes.Contains(bin, []byte("_|_")) {
		t.Fatalf("compiled file keeps the types: %v", loaded.Grammar)
	}
	if again, err := loaded.MarshalBinary("main"); err != nil || !bytes.Equal(again, bin) {
		t.Fatalf("compiled round trip differs: %v", err)
	}
	if got := grammar.Format(program.Grammar); !strings.Contains(got, "type A ~ []B & *C | _ & (D | B) | _|_") || !strings.Contains(got, "$x.(_ & node[_])") {
		t.Fatalf("source grammar:\n%s", got)
	}
}

func TestTopBottomSchema(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, "type T = { A _; B (node[string] | int) & node[_]; C U }\ntype U ~ _\nrule main = x:\"a\" => T{A: $x, B: $x, C: 1}")
	schema, err := outputSchemaPrototype(program, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`{"name":"A","shape":{"kind":"top"}}`, `{"name":"B","shape":{"kind":"node","value":{"kind":"scalar","scalar":"string"}}}`, `{"name":"C","shape":{"kind":"top"}}`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("schema lacks %s:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), `"declared/U"`) {
		t.Fatalf("schema exports the alias U:\n%s", data)
	}
	// Aliases are not exported, and a field cannot have a type without values.
	for _, src := range []string{"type B ~ _|_\nrule main = \"a\"", "type B ~ int & string\nrule main = \"a\"", "type B ~ *(int & string)\nrule main = \"a\""} {
		if _, err := outputSchemaPrototype(mustCompile(t, src), "main", false); err != nil {
			t.Fatalf("%q schema: %v", src, err)
		}
		if message := compileError(t, strings.Replace(src, "rule main", "type T = { F B }\nrule main", 1)); !strings.Contains(message, "no values") {
			t.Fatalf("%q field: %s", src, message)
		}
	}
	// _|_ adds nothing to a union.
	program = mustCompile(t, "type B ~ _|_ | node[string]\ntype T = { A _|_ | node[string]; C B }\nrule main = x:\"a\" => T{A: $x, C: $x}")
	schema, err = outputSchemaPrototype(program, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := json.Marshal(schema); !strings.Contains(string(data), `{"name":"A","shape":{"kind":"node","value":{"kind":"scalar","scalar":"string"}}}`) {
		t.Fatalf("bottom union schema:\n%s", data)
	}
}
