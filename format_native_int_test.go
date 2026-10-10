package pego_test

import (
	"fmt"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

func TestFormatNativeMinimumRoundTrip(t *testing.T) {
	max := int(^uint(0) >> 1)
	minimum := &grammar.IntLit{Value: -max - 1}
	for _, tc := range []struct {
		name string
		term grammar.Term
		want int
	}{
		{"literal", minimum, minimum.Value},
		{"negated", &grammar.Unary{Op: "-", X: minimum}, minimum.Value},
		{"left addition", &grammar.Binary{Op: "+", L: minimum, R: &grammar.IntLit{Value: 1}}, minimum.Value + 1},
		{"right subtraction", &grammar.Binary{Op: "-", L: &grammar.IntLit{Value: 1}, R: minimum}, minimum.Value + 1},
		{"left division", &grammar.Binary{Op: "/", L: minimum, R: &grammar.IntLit{Value: 2}}, minimum.Value / 2},
		{"right multiplication", &grammar.Binary{Op: "*", L: &grammar.IntLit{Value: 2}, R: minimum}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := pego.ParseGrammar("type N struct { V int }\ndef main = \"é\" $$ -> new N{V: 0}")
			if err != nil {
				t.Fatal(err)
			}
			g.Rules()[0].Action.(*grammar.New).Fields[0].Value = tc.term
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			jsonAST, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			source := grammar.Format(jsonAST)
			sourceAST, err := pego.ParseGrammar(source)
			if err != nil {
				t.Fatalf("formatted source cannot be parsed: %v\n%s", err, source)
			}
			if again := grammar.Format(sourceAST); again != source {
				t.Fatalf("format is not idempotent:\n%s\n%s", source, again)
			}
			for _, ast := range []*grammar.Grammar{g, jsonAST, sourceAST} {
				p, err := pego.Compile(ast, "main")
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := p.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := pego.LoadParser(encoded)
				if err != nil {
					t.Fatal(err)
				}
				for _, parser := range []*pego.Parser{p, loaded} {
					for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
						for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
							n, err := parser.Parse("é", pego.WithBackend(backend), pego.WithUnit(unit))
							want := fmt.Sprintf("(N V=%d)", tc.want)
							if err != nil || n.String() != want {
								t.Fatalf("backend %v, unit %v: %v, %v; want %s", backend, unit, n, err, want)
							}
						}
					}
				}
			}
		})
	}
}
