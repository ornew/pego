//go:build !pego_work_controls_reference

package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// A shared AST leaf is first compiled value-free for a text struct rule, then
// value-building for an ordinary rule. It must preserve both modes, rule
// names, captures and active accounting in either dispatch graph.
func TestWorkPrimitiveASTAliases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, expr, text string }{
		{"literal", `"é"`, "é"},
		{"class", `[a-zα-ω😀]`, "λ"},
		{"negated", `[^a-z]`, "λ"},
		{"any", `.`, "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("type Token = { Text string }\nrule token Token = t:(%s) #atomic => Token{Text: text($t)}\nrule value = %s\nrule main = a:value b:token c:value $$", tc.expr, tc.expr)
			reference := mustCompile(t, source)
			g, err := parseTest(source)
			if err != nil {
				t.Fatal(err)
			}
			rules := g.Rules()
			rules[1].Expr = primitiveOf(rules[0].Expr)
			shared, err := Compile(g, compile.Options{SourceInfo: true})
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
					val, ok := n.Field(name).(*value.Node)
					text := ""
					if val != nil && name == "b" {
						text, _ = val.Field("Text").(string)
					} else if val != nil && val.IsTerminal() {
						text = value.D(val).Text
					}
					if !ok || val == nil || text != tc.text || value.D(val).Start != int32(i)*width || value.D(val).End != int32(i+1)*width {
						t.Fatalf("unit=%v capture=%s lost value or span: %v", unit, name, val)
					}
					// A text struct value carries no rule name.
					wantType, wantRule := value.TypeString, "value"
					if name == "b" {
						wantType, wantRule = "Token", ""
					}
					if val.Type() != wantType || val.Rule() != wantRule {
						t.Fatalf("unit=%v capture=%s type/rule=%s/%s", unit, name, val.Type(), val.Rule())
					}
				}
				for _, recognize := range []bool{false, true} {
					for _, text := range []string{input, "", tc.text, "x"} {
						for limit := uint64(0); limit <= 80; limit++ {
							for _, limits := range []limits.Limits{{MaxWork: limit}, {MaxWork: limit}} {
								opts := ParseOptions{Unit: unit, Recognize: recognize, Limits: limits}
								a, ae := shared.ParseWith("main", text, opts)
								b, be := reference.ParseWith("main", text, opts)
								if gentest.ResultJSON(a, ae) != gentest.ResultJSON(b, be) {
									t.Fatalf("unit=%v recognize=%v text=%q limits=%+v: shared=%s distinct=%s", unit, recognize, text, limits, gentest.ResultJSON(a, ae), gentest.ResultJSON(b, be))
								}
							}
						}
					}
				}
			}
		})
	}
}

// primitiveOf returns the leaf expression under the capture, group and #atomic of a text struct
// rule body.
func primitiveOf(e grammar.Expr) grammar.Expr {
	for {
		switch x := e.(type) {
		case *grammar.Capture:
			e = x.Expr
		case *grammar.Group:
			e = x.Expr
		case *grammar.Atomic:
			e = x.Expr
		case *grammar.Attributed:
			e = x.Expr
		default:
			return e
		}
	}
}
