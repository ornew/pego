package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
	"testing"
)

func classMembershipProgram(t *testing.T, expr grammar.Expr) *Program {
	t.Helper()
	g, err := syntax.Parse(`rule main = [a]`)
	if err != nil {
		t.Fatal(err)
	}
	g.Rules()[0].Expr = expr
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatalf("compile class grammar: %v", err)
	}
	return p
}

// Keep ordinary membership semantics covered in the instrumentation-free build.
func TestClassMembershipBoundaries(t *testing.T) {
	cases := []struct {
		name      string
		ranges    []grammar.CharRange
		hit, miss []rune
	}{
		{"one", []grammar.CharRange{{Lo: 'a', Hi: 'z'}}, []rune{'a', 'm', 'z'}, []rune{'`', '{', 128, 0x10000}},
		{"two", []grammar.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10002}}, []rune{'a', 'z', 0x10000, 0x10001, 0x10002}, []rune{'`', '{', 128, 0xffff, 0x10003}},
		{"bitmap", []grammar.CharRange{{Lo: 63, Hi: 65}, {Lo: 127, Hi: 128}, {Lo: 0x10000, Hi: 0x10002}}, []rune{63, 64, 65, 127, 128, 0x10000, 0x10002}, []rune{62, 66, 126, 129, 0xffff, 0x10003}},
	}
	for _, tc := range cases {
		for _, negated := range []bool{false, true} {
			p := classMembershipProgram(t, &grammar.CharClass{Ranges: tc.ranges, Negated: negated})
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, c := range []struct {
						runes []rune
						want  bool
					}{{tc.hit, !negated}, {tc.miss, negated}} {
						for _, r := range c.runes {
							n, err := p.ParseWith("main", string(r), ParseOptions{Backend: backend, Unit: unit})
							if (err == nil) != c.want || (n != nil) != c.want || n != nil && n.Text != string(r) {
								t.Fatalf("%s negated=%v %v/%v U+%04X: node=%v error type=%T, want match=%v", tc.name, negated, backend, unit, r, n, err, c.want)
							}
						}
					}
				}
			}
		}
	}
}
