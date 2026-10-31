package engine

import (
	"testing"

	"github.com/ornew/zeroparse/internal/gentest"
)

// The locals of an Operators Rule's actions read a scalar as its value, as an ordinary capture
// does: the closure, bytecode and iterative backends agree on every input of the corpus.
func TestOperatorsRuleLocalsBackends(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.OperatorsRuleLocalCases {
		prog := mustCompile(t, c.Src)
		for _, input := range c.Inputs {
			checkBackends(t, prog, "main", input)
		}
	}
}

// A scalar operand read by $lhs or $value is the scalar, not the range of the node that carries it.
func TestOperatorsRuleScalarLocals(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = e $$
rule e = #operators {
    operand "(" n:[0-9] ")" => text($n)
    operand [0-9]
    level { infix left "+" => text($lhs) }
}`)
	for _, tc := range []struct{ input, want string }{
		{"(1)+2", `("1")@main`},
		{"1+2", `("1")@main`},
	} {
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := resultWith(prog, tc.input, ParseOptions{Backend: b}); got != tc.want {
				t.Errorf("%v: %q\n got  %s\n want %s", b, tc.input, got, tc.want)
			}
		}
	}
	whole := mustCompile(t, `rule main = v:e $$ => $v
rule e = #operators {
    operand "(" n:[0-9] ")" => text($n)
    operand [0-9]
    level { infix none "+" }
} => text($value)`)
	for _, tc := range []struct{ input, want string }{
		{"(1)", `"1"`},
		{"(1)+2", `"(1)+2"`},
	} {
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := resultWith(whole, tc.input, ParseOptions{Backend: b}); got != tc.want {
				t.Errorf("%v: %q\n got  %s\n want %s", b, tc.input, got, tc.want)
			}
		}
	}
}
