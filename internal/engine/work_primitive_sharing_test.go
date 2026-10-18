//go:build !pego_work_controls_reference

package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// A shared AST leaf is first compiled value-free for a terminal rule, then
// value-building for an ordinary rule. It must preserve both modes, rule
// names, captures and active accounting in either dispatch graph.
func TestWorkPrimitiveASTAliases(t *testing.T) {
	for _, tc := range []struct{ name, expr, text string }{
		{"literal", `"é"`, "é"},
		{"class", `[a-zα-ω😀]`, "λ"},
		{"negated", `[^a-z]`, "λ"},
		{"any", `.`, "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("type Token terminal\nrule token Token = %s\nrule value = %s\nrule main = a:value b:token c:value $$", tc.expr, tc.expr)
			reference := compile(t, source)
			g, err := syntax.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			rules := g.Rules()
			rules[1].Expr = rules[0].Expr
			shared, err := Compile(g, Options{SourceInfo: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				input := strings.Repeat(tc.text, 3)
				n, err := shared.ParseWith("main", input, ParseOptions{Unit: unit})
				if err != nil {
					t.Fatal(err)
				}
				width := int32(1)
				if unit == Bytes {
					width = int32(len(tc.text))
				}
				for i, name := range []string{"a", "b", "c"} {
					value, ok := n.Field(name).(*Node)
					if !ok || value == nil || !value.IsTerminal() || value.text != tc.text || value.start != int32(i)*width || value.end != int32(i+1)*width {
						t.Fatalf("unit=%v capture=%s lost value or span: %v", unit, name, value)
					}
					wantType, wantRule := TypeMatch, "value"
					if name == "b" {
						wantType, wantRule = "Token", "token"
					}
					if value.Type() != wantType || value.Rule() != wantRule {
						t.Fatalf("unit=%v capture=%s type/rule=%s/%s", unit, name, value.Type(), value.Rule())
					}
				}
				for _, recognize := range []bool{false, true} {
					for _, text := range []string{input, "", tc.text, "x"} {
						for limit := uint64(0); limit <= 80; limit++ {
							for _, limits := range []WorkLimits{{MaxSteps: limit}, {MaxInputWork: limit}} {
								opts := ParseOptions{Unit: unit, Recognize: recognize, WorkLimits: limits}
								a, ae := shared.ParseWith("main", text, opts)
								b, be := reference.ParseWith("main", text, opts)
								if resultJSON(a, ae) != resultJSON(b, be) {
									t.Fatalf("unit=%v recognize=%v text=%q limits=%+v: shared=%s distinct=%s", unit, recognize, text, limits, resultJSON(a, ae), resultJSON(b, be))
								}
							}
						}
					}
				}
			}
		})
	}
}
