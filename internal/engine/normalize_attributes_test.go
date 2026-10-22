package engine

import (
	"os"
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestNormalizationCopyOnChangeParity(t *testing.T) {
	for _, source := range []string{
		`rule main = "a"? (&"b" / !"x") "b"+ $$`,
		`rule main = ("a" #repeat(1, 3) #atomic @error("as")) "b" $$`,
		`rule main = ("a" #atomic ";") #recover(skip=([^;]+ ";")) $$`,
		`rule main = "a"* #stream $$`,
		`type Number = { Text (string) }
type Alias ~ (Number)
var count int
rule main (Alias) = {count := (1)} {(count == 1)} n:([0-9]+) => (Number{Text: (text($n))})`,
		`rule main = operators {
    skip " "*
    operand [0-9]+
    operand "(" main ")"
    level { infix left "+" / "-" }
    level product { infix left "*" / "/" }
}`,
		`rule main = "a" @error(nil, "bad")`,
		`rule main = "a" @error("a", message="b")`,
		`rule main = "a" @atomic`,
		`rule main = "a" #repeat(3, 1)`,
	} {
		t.Run(source, func(t *testing.T) {
			g, err := parseTest(source)
			if err != nil {
				t.Fatal(err)
			}
			before, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			got, gotErr := normalizeAttributesWithCopies(g, false)
			want, wantErr := normalizeAttributesWithCopies(g, true)
			if !reflect.DeepEqual(gotErr, wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("normalization differs: got %#v / %v; want %#v / %v", got, gotErr, want, wantErr)
			}
			after, err := grammar.MarshalJSON(g)
			if err != nil || string(after) != string(before) {
				t.Fatalf("source changed: %v", err)
			}
			if gotErr != nil {
				return
			}
			optimized, optimizedErr := Compile(g, Options{})
			eager, eagerErr := Compile(g, Options{eagerAttributeCopies: true})
			if !reflect.DeepEqual(optimizedErr, eagerErr) {
				t.Fatalf("compile errors differ: %v / %v", optimizedErr, eagerErr)
			}
			if optimizedErr != nil {
				return
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, input := range []string{"", "b", "ab", "aaab", "a;", "x;", "123", "1 + 2 * 3", "é"} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						if actual, expected := resultJSON(optimized.ParseWith("main", input, opts)), resultJSON(eager.ParseWith("main", input, opts)); actual != expected {
							t.Fatalf("%s/%s %q: %s / %s", backend, unit, input, actual, expected)
						}
					}
				}
			}
			for _, p := range []*Program{optimized, eager} {
				_, _ = p.ParseWith("main", "ab", ParseOptions{Recognize: true})
			}
			after, err = grammar.MarshalJSON(g)
			if err != nil || string(after) != string(before) {
				t.Fatalf("compilation/backend initialization changed source: %v", err)
			}
		})
	}
}

func TestNormalizationContainerShapes(t *testing.T) {
	literal := &grammar.Literal{Value: "a"}
	for _, expr := range []grammar.Expr{
		&grammar.Seq{}, &grammar.Seq{Items: []grammar.Expr{}},
		&grammar.Choice{}, &grammar.Choice{Alts: []grammar.Expr{}},
		&grammar.OperatorsRule{},
		&grammar.OperatorsRule{Operands: []*grammar.OperatorsRuleOperand{}, Levels: []*grammar.OperatorsRuleLevel{{}}},
		&grammar.Attributed{Expr: literal},
		&grammar.Attributed{Expr: literal, Attrs: []*grammar.Attribute{{Name: "custom"}}},
		&grammar.Attributed{Expr: literal, Attrs: []*grammar.Attribute{{Name: "custom", Args: []any{}, KwArgs: map[string]any{}, KeywordOrder: []string{}}}},
		&grammar.Attributed{Expr: literal, Attrs: []*grammar.Attribute{{Name: "custom", Args: []any{nil, map[string]any{"x": []int{1, 2}}}, KwArgs: map[string]any{"x": []int{3}}}}},
	} {
		g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: expr}}}}
		got, gotErr := normalizeAttributesWithCopies(g, false)
		want, wantErr := normalizeAttributesWithCopies(g, true)
		if !reflect.DeepEqual(gotErr, wantErr) || !reflect.DeepEqual(got, want) {
			t.Fatalf("%T: normalized shape differs", expr)
		}
	}
	for _, statements := range [][]grammar.Decl{nil, {}} {
		g := &grammar.Grammar{File: grammar.File{Decls: statements}}
		got, _ := normalizeAttributesWithCopies(g, false)
		want, _ := normalizeAttributesWithCopies(g, true)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("empty grammar shape differs")
		}
	}
}

func TestNormalizationAttributeBindingDoesNotMutate(t *testing.T) {
	for _, name := range []string{"error", "recover"} {
		key := "message"
		if name == "recover" {
			key = "skip"
		}
		a := &grammar.Attribute{Name: name, Pragma: name == "recover", Args: []any{&grammar.Literal{Value: "x"}}, KwArgs: map[string]any{"custom": []int{1, 2}}}
		n := &attributeNormalizer{}
		bound := n.bind(n.attribute(a), key)
		if len(a.Args) != 1 || len(a.KwArgs) != 1 || len(bound.Args) != 0 || len(bound.KwArgs) != 2 {
			t.Fatal("binding mutated source")
		}
		if _, found := a.KwArgs[key]; found {
			t.Fatal("keyword inserted in source")
		}
	}
}

func TestNormalizationSharesUnchangedNodes(t *testing.T) {
	g, err := parseTest(`rule main = "a" "b"*`)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAttributes(g)
	if err != nil {
		t.Fatal(err)
	}
	if normalized != g {
		t.Fatal("unchanged grammar was copied")
	}
	g, err = parseTest("rule main = (\"a\") \"b\"*\nrule other = \"c\"")
	if err != nil {
		t.Fatal(err)
	}
	normalized, err = normalizeAttributes(g)
	if err != nil {
		t.Fatal(err)
	}
	if normalized == g || normalized.Rules()[0] == g.Rules()[0] || normalized.Rules()[1] != g.Rules()[1] {
		t.Fatal("changed path sharing differs")
	}
	if normalized.Rules()[0].Expr.(*grammar.Seq).Items[1] != g.Rules()[0].Expr.(*grammar.Seq).Items[1] {
		t.Fatal("unchanged sibling copied")
	}
}

var normalizationBenchmarkGrammar *grammar.Grammar
var normalizationBenchmarkProgram *Program

func BenchmarkAttributeNormalization(b *testing.B) {
	sources := map[string]string{
		"plain":      `rule main = "a"? (&"b" / !"x") "b"+ $$`,
		"attributes": `rule main = ("a" @error("as")) "b" @error(message="bs") $$`,
		"pragmas":    `rule main = ("a" #repeat(1, 3) #atomic ";") #recover(skip=[^;]+ ";") $$`,
		"operators":  `rule main = operators { skip " "*; operand [0-9]+; operand "(" main ")"; level { infix left "+" / "-" }; level product { infix left "*" / "/" } }`,
	}
	source, err := os.ReadFile("../../pego.pego")
	if err != nil {
		b.Fatal(err)
	}
	sources["source-grammar"] = string(source)
	for name, path := range map[string]string{"json": "../../parsers/json/json.pego", "minilang": "../../examples/minilang/minilang.pego"} {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		sources[name] = string(data)
	}
	for name, source := range sources {
		g, err := parseTest(source)
		if err != nil {
			b.Fatal(err)
		}
		for _, eager := range []bool{true, false} {
			mode := "copy-on-change"
			if eager {
				mode = "eager"
			}
			b.Run(name+"/"+mode+"/normalize", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					var err error
					normalizationBenchmarkGrammar, err = normalizeAttributesWithCopies(g, eager)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(name+"/"+mode+"/compile", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					var err error
					normalizationBenchmarkProgram, err = Compile(g, Options{eagerAttributeCopies: eager})
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(name+"/"+mode+"/source-prepare", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					ast, err := parseTest(source)
					if err != nil {
						b.Fatal(err)
					}
					normalizationBenchmarkProgram, err = Compile(ast, Options{eagerAttributeCopies: eager})
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
