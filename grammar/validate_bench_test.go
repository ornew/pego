package grammar

import (
	"fmt"
	"testing"
)

// BenchmarkValidate isolates ordinary structural validation from compilation.
// Fixtures are built outside timing and cover polling sites in different AST
// shapes; this does not measure cancellation latency or source parsing.
func BenchmarkValidate(b *testing.B) {
	wide := &Grammar{Statements: make([]Statement, 4096)}
	for i := range wide.Statements {
		wide.Statements[i] = &RuleDef{Name: fmt.Sprintf("item%d", i), Expr: &Literal{Value: "a"}}
	}
	var deep Expr = &Top{}
	for range 4096 {
		deep = &Optional{Expr: deep}
	}
	kwargs := make(map[string]any, 4096)
	for i := range 4096 {
		kwargs[fmt.Sprintf("key%d", i)] = i
	}
	ranges := make([]CharRange, 4096)
	for i := range ranges {
		ranges[i] = CharRange{Lo: 'a', Hi: 'z'}
	}
	for _, fixture := range []struct {
		name string
		g    *Grammar
	}{
		{"Small", expressionGrammar(&Seq{Items: []Expr{&Literal{Value: "a"}, &Optional{Expr: &Top{}}}})},
		{"WideRules", wide},
		{"DeepOptional", expressionGrammar(deep)},
		{"AttributeKeys", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: kwargs}}})},
		{"CharacterRanges", expressionGrammar(&CharClass{Ranges: ranges})},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			if err := Validate(fixture.g); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := Validate(fixture.g); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
