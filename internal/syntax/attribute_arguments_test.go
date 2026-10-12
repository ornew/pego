package syntax_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

func TestRepetitionPragmaBounds(t *testing.T) {
	for _, tc := range []struct {
		source   string
		args     []any
		min, max int
		input    string
	}{
		{`"a" #repeat(2)`, []any{2}, 2, 2, "aa"},
		{`"a" #repeat(2, 3)`, []any{2, 3}, 2, 3, "aaa"},
		{`"a" #repeat(, 3)`, []any{nil, 3}, 0, 3, "aa"},
		{`"a" #repeat(2, )`, []any{2, nil}, 2, -1, "aaaa"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			g, err := syntax.Parse("rule main = " + tc.source)
			if err != nil {
				t.Fatal(err)
			}
			assertBounds := func(g *grammar.Grammar) {
				t.Helper()
				rep := g.Rules()[0].Expr.(*grammar.Repeat)
				if rep.Min != tc.min || rep.Max != tc.max {
					t.Fatalf("bounds %d,%d", rep.Min, rep.Max)
				}
			}
			assertBounds(g)
			before, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(before)
			if err != nil {
				t.Fatal(err)
			}
			assertBounds(decoded)
			formatted := grammar.Format(decoded)
			reparsed, err := syntax.Parse(formatted)
			if err != nil {
				t.Fatal(err)
			}
			assertBounds(reparsed)
			p, err := engine.Compile(g, engine.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rep := p.Grammar.Rules()[0].Expr.(*grammar.Repeat)
			if rep.Min != tc.min || rep.Max != tc.max {
				t.Fatalf("normalized bounds %d,%d", rep.Min, rep.Max)
			}
			after, _ := grammar.MarshalJSON(g)
			if !bytes.Equal(before, after) {
				t.Fatal("compilation mutated source AST")
			}
			data, err := p.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			loaded, start, err := engine.LoadProgram(data, engine.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var expected string
			for _, program := range []*engine.Program{p, loaded} {
				for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
					n, err := program.ParseWith(start, tc.input, engine.ParseOptions{Backend: backend})
					if err != nil {
						t.Fatal(err)
					}
					if expected == "" {
						expected = n.String()
					} else if n.String() != expected {
						t.Fatalf("CST differs: %s != %s", n.String(), expected)
					}
				}
			}
		})
	}
}

func TestAttributeArgumentBinding(t *testing.T) {
	for _, src := range []string{
		`rule main = "a" @error("expected a")`,
		`rule main = "a" @error(message="expected a")`,
		`rule main = "a" #recover(skip="b")`,
		`rule main = "a" #recover(skip="b")`,
		`rule main = [a-z] #repeat(2) #atomic`,
		`rule main = ("a" #atomic) #repeat(2)`,
		`rule main = "a" #repeat(2) #repeat(1)`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Compile(g, engine.Options{}); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
	for _, src := range []string{
		`rule main = "a" #repeat(min=2)`,
		`rule main = "a" #repeat(,)`,
		`rule main = "a" #repeat(-1)`,
		`rule main = "a" #repeat(2,-1)`,
		`rule main = "a" #repeat(,-2)`,
		`rule main = "a" #repeat(2,1)`,
		`rule main = "a" #repeat("two")`,
		`rule main = "a" @error("x",message="y")`,
		`rule main = "a" #atomic(1)`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			continue
		}
		if _, err := engine.Compile(g, engine.Options{}); err == nil {
			t.Fatalf("accepted %s", src)
		}
	}
	for _, src := range []string{
		`rule main = "a" @error(message="x", "y")`,
		`rule main = "a" @error(message="x",message="y")`,
	} {
		if _, err := syntax.Parse(src); err == nil {
			t.Fatalf("accepted %s", src)
		}
	}
}

func TestAttributeScalarJSON(t *testing.T) {
	g, err := syntax.Parse(`rule main = "a" @custom(1, "text", , flag=true, missing=nil, expr=(a / b))`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"kwArgs"`) {
		t.Fatal("keyword arguments absent")
	}
	decoded, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(decoded)
	g2, err := syntax.Parse(formatted)
	if err != nil {
		t.Fatal(err)
	}
	data2, _ := grammar.MarshalJSON(g2)
	if !bytes.Equal(data, data2) {
		t.Fatalf("argument JSON changed\n%s\n%s\n%s", data, data2, formatted)
	}
}

func TestAttributeOmittedSlots(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []any
	}{{"@custom(2)", []any{2}}, {"@custom(2, )", []any{2, nil}}, {"@custom(, 2)", []any{nil, 2}}} {
		g, err := syntax.Parse("rule main = \"a\" " + tc.source)
		if err != nil {
			t.Fatal(err)
		}
		a := g.Rules()[0].Expr.(*grammar.Attributed).Attrs[0]
		if !reflect.DeepEqual(a.Args, tc.want) {
			t.Fatalf("%#v", a.Args)
		}
		data, err := grammar.MarshalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := grammar.DecodeJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		again, err := syntax.Parse(grammar.Format(decoded))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.Rules()[0].Expr.(*grammar.Attributed).Attrs[0].Args, tc.want) {
			t.Fatal("lost slots")
		}
	}
}

func TestAttributeNativeIntegerRoundTrip(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	minimum := -maximum - 1
	g := &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{
		Name: "main", Expr: &grammar.Attributed{Expr: &grammar.Literal{Value: "a"},
			Attrs: []*grammar.Attribute{{Name: "custom", Args: []any{minimum, maximum, -1}, KwArgs: map[string]any{"minimum": minimum}}}},
	}}}
	source := grammar.Format(g)
	parsed, err := syntax.Parse(source)
	if err != nil {
		t.Fatalf("formatted native integers: %v\n%s", err, source)
	}
	got := parsed.Rules()[0].Expr.(*grammar.Attributed).Attrs[0]
	if !reflect.DeepEqual(got.Args, []any{minimum, maximum, -1}) || got.KwArgs["minimum"] != minimum {
		t.Fatalf("integer arguments changed: %+v", got)
	}
}

func TestAttributeEqualsLiteralIsAnExpressionArgument(t *testing.T) {
	g, err := syntax.Parse(`rule main = "a" @custom(x "=")`)
	if err != nil {
		t.Fatal(err)
	}
	a := g.Rules()[0].Expr.(*grammar.Attributed).Attrs[0]
	if len(a.Args) != 1 || len(a.KwArgs) != 0 {
		t.Fatalf("arguments: %+v", a)
	}
	seq, ok := a.Args[0].(*grammar.Seq)
	if !ok || len(seq.Items) != 2 {
		t.Fatalf("argument: %#v", a.Args[0])
	}
	if ref, ok := seq.Items[0].(*grammar.Ref); !ok || ref.Name != "x" {
		t.Fatalf("reference: %#v", seq.Items[0])
	}
	if lit, ok := seq.Items[1].(*grammar.Literal); !ok || lit.Value != "=" {
		t.Fatalf("literal: %#v", seq.Items[1])
	}
}
