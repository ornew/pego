package engine

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

const topBottomRules = "type Num = { Text string }\nrule n Num = t:([0-9]+) #atomic => Num{Text: text($t)}\nrule w = [a-z]+\n"

func topBottomCases() []genCase {
	return []genCase{
		{"top field", "type T = { A _ }\nrule main = x:(n / w) $$ => T{A: $x}\n" + topBottomRules, []string{"1", "a"}},
		// text applies to nodes of string values, so both alternatives are plain nodes here.
		{"top primitive", "type T = { A _ }\nrule main = x:(n / w) $$ => T{A: len(text($x))}\nrule n = [0-9]+ #atomic\nrule w = [a-z]+\n", []string{"12", "a"}},
		{"top cast", "type T = { A _ }\nrule main = x:(n / w) $$ => T{A: $x}.A.(Num)\n" + topBottomRules, []string{"1", "a"}},
		{"top compare", "type T = { A _ }\nrule main = x:(n / w) {T{A: $x}.A == $x} $$\n" + topBottomRules, []string{"1", "a"}},
		// A struct is not a node, so the meet is node[string].
		{"meet field", "type T = { A (Num | node[string] | int) & node[_] }\nrule main = x:([a-z]+ #atomic) $$ => T{A: $x}\n" + topBottomRules, []string{"a", "1"}},
		{"top list", "type T = { A []_ }\ntype U = { F _ }\nrule main = x:(n / w)+ $$ => T{A: U{F: $x}.F.([]_)}\n" + topBottomRules, []string{"1", "1a"}},
		{"top list mismatch", "type T = { A []_ | int }\ntype U = { F _ }\nrule main = x:(n / w) $$ => T{A: U{F: $x}.F.([]_ | int)}\n" + topBottomRules, []string{"1", "a"}},
		{"meet list", "type T = { A [](Num | node[string]) & []node[_] }\nrule main = xs:[a-z]+ $$ => T{A: $xs.Value}\n" + topBottomRules, []string{"ab", ""}},
	}
}

func TestTopBottomAcrossBackends(t *testing.T) {
	for _, fixture := range topBottomCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := mustCompile(t, fixture.src)
			for _, input := range fixture.inputs {
				baseline := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

func TestTopBottomTypes(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// A _ value is assignable only to _.
		{"type T = { A _ }\ntype U = { B node[string] }\nrule main = x:\"a\" => U{B: T{A: $x}.A}", "_"},
		// No value has type _|_.
		{"type T = { A _|_ }\nrule main = \"a\"", "field A of T has type _|_, which has no values"},
		{"type B ~ []_|_\nrule main B = \"a\"", "rule main has type _|_, which has no values"},
		{"type B ~ *_|_\nrule main = x:\"a\" => $x.(B)", "the cast has type _|_, which has no values"},
		{"rule main = \"a\" => []_|_{}", "the list element has type _|_, which has no values"},
		{"type T = { A int & string }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A node[string] & []node[string] }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A []node[string] & []int }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A *[]node[string] & []int }\nrule main = \"a\"", "field A of T has type _|_"},
		// A meet is the type assignable to both: here node[string], which an int is not.
		{"type T = { A (node[string] | int) & node[_] }\nrule main = \"a\" => T{A: 1}", "cannot use int as node[string]"},
		// Nodes meet in their values, and tuples element by element.
		{"type T = { A node[string | []node[string]] & node[string | int] }\nrule main = \"a\" => T{A: 1}", "cannot use int as node[string]"},
		{"type T = { A (node[string] | int, node[string]) & (node[string] | bool, node[string]) }\nrule main = \"a\" => T{A: 1}", "cannot use int as (node[string], node[string])"},
		{"type T = { A (node[string], int) & (node[string], bool) }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A (node[string],) & (node[string], node[string]) }\nrule main = \"a\"", "field A of T has type _|_"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	for _, src := range []string{
		// A rule may produce a value of type _.
		"type T = { A _ }\nrule main = x:\"a\" => T{A: $x}.A",
		"rule main _ = \"a\"",
		"type T = { A _ | node[string] }\nrule main = \"a\" => T{A: 1}",
		"type T = { A _|_ | node[string] }\nrule main = x:\"a\" => T{A: $x}",
		"type T = { A node[string] & _ }\nrule main = x:\"a\" => T{A: $x}\nrule other = x:\"a\" => T{A: $x}.A",
		"type T = { A _ }\nrule main = x:\"a\" => T{A: $x}.A.(node[string] | node[_])",
		"type B ~ _|_\nrule main = \"a\"", // an alias is not a location
		// Meets look inside lists: []Seq is assignable to both.
		"type T = { A [](node[string] | node[_]) & [](node[_] | node[_]) }\nrule main = x:(\"a\" \"b\")* => T{A: $x.Value}",
		"type T = { A *node[string] & *node[_] }\nrule main = \"a\" => T{A: nil}",
		"type T = { A *[](node[string] | node[_]) & [](node[_] | node[_]) }\nrule main = x:(\"a\" \"b\")* => T{A: $x.Value}",
		"type T = { A ([]node[string] | int) & []node[_] }\nrule main = x:\"a\"* => T{A: $x.Value}",
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if _, err := Compile(g, compile.Options{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

// A list of _ only has to be a list value, whatever its elements.
func TestTopBottomListCheck(t *testing.T) {
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

func TestTopBottomReportedOnce(t *testing.T) {
	g, err := parseTest("type T = { A int & string }\nrule main = x:\"a\" => T{A: $x}")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(g, compile.Options{})
	if err == nil || strings.Count(err.Error(), "_|_") != 1 || strings.Contains(err.Error(), "never") {
		t.Fatalf("got %v, want one _|_ error", err)
	}
	g, err = parseTest("rule main = x:\"a\" => [](int & string){$x}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Compile(g, compile.Options{}); err == nil || strings.Count(err.Error(), "_|_") != 1 {
		t.Fatalf("got %v, want one _|_ error", err)
	}
	// An empty meet (only an AST can hold one) is _.
	g, err = parseTest("type T = { A node[string] }\nrule main = x:\"a\" => T{A: $x}")
	if err != nil {
		t.Fatal(err)
	}
	g.Types()[0].(*grammar.TypeDef).Struct().Fields[0].Type = &grammar.MeetType{}
	if _, err := Compile(g, compile.Options{}); err != nil {
		t.Fatal(err)
	}
}

// A location of type _|_ is an error: no value is assignable to it.
func TestTopBottomLocationsAreRejected(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"rule main = x:\"a\" => $x.(_|_)", "the cast has type _|_"},
		{"rule main = x:\"a\" => $x.(int & string)", "the cast has type _|_"},
		{"type T = { A _|_ }\nrule main = x:\"a\" => T{A: $x}", "field A of T has type _|_"},
		{"type T = { A int & string }\nrule main = x:\"a\" => T{A: $x}", "field A of T has type _|_"},
		{"type B ~ _|_\nrule main B = \"a\"", "rule main has type _|_"},
		{"rule main = x:\"a\" => []_|_{$x}", "the list element has type _|_"},
		{"rule main = x:\"a\" => [](int & string){$x}", "the list element has type _|_"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// Other type errors are reported too.
	g, err := parseTest("type T = { A node[string] }\nrule main = \"a\" => T{A: 1}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), "cannot use int") {
		t.Fatalf("got %v, want a type error", err)
	}
}

func TestTopBottomSyntax(t *testing.T) {
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
	program := mustCompile(t, "type T = { A _; B (node[string] | int) & node[_]; C U }\ntype U ~ _\nrule main = x:\"a\" => T{A: $x, B: $x, C: 1}")
	schema, err := program.OutputSchemaPrototype("main", false)
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
		if _, err := mustCompile(t, src).OutputSchemaPrototype("main", false); err != nil {
			t.Fatalf("%q schema: %v", src, err)
		}
		if message := compileError(t, strings.Replace(src, "rule main", "type T = { F B }\nrule main", 1)); !strings.Contains(message, "no values") {
			t.Fatalf("%q field: %s", src, message)
		}
	}
	// _|_ adds nothing to a union.
	program = mustCompile(t, "type B ~ _|_ | node[string]\ntype T = { A _|_ | node[string]; C B }\nrule main = x:\"a\" => T{A: $x, C: $x}")
	schema, err = program.OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := json.Marshal(schema); !strings.Contains(string(data), `{"name":"A","shape":{"kind":"node","value":{"kind":"scalar","scalar":"string"}}}`) {
		t.Fatalf("bottom union schema:\n%s", data)
	}
}

func TestTopBottomGeneratedGoParity(t *testing.T) { eachMode(t, testTopBottomGeneratedGoParity) }

func testTopBottomGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, topBottomCases(), gen.GenOptions{})
	// A field of type _, like one of type any, keeps the reference runtime.
	testGeneratedTypesCorpus(t, goBin, topBottomCases(), gen.GenOptions{ConvertTypes: true}, 2)
}

func TestTopBottomGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, topBottomCases(), gen.GenOptions{})
}
