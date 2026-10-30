package compile

import (
	"strings"
	"testing"
)

// TestConcatRejectsTopArgument checks that concat rejects an argument of type _ at compile time,
// even when a later argument would fail when evaluated.
func TestConcatRejectsTopArgument(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`rule main = x:"a" d:dyn => concat($d, list(1 / 0))
rule dyn _ = ds / di
rule ds = "b" => "s"
rule di = _ => 1`, `type A = { N int }
rule main = x:"a" d:dyn => concat($d, list(A{N: 1 / 0}))
rule dyn _ = ds / di
rule ds = "b" => "s"
rule di = _ => 1`} {
		if got := compileMessage(t, src); !strings.Contains(got, "concat: expected a list, got _") {
			t.Errorf("compile error = %q", got)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = x`, `1:13: undefined rule x`},
		{`rule main = "a"
rule main = "b"`, `2:1: rule main is already defined`},
		{`type Match = { Text string }`, `type Match is reserved`},
		{`type foo = { Text string }
rule main = "a"`, `type foo must start with an uppercase letter`},
		{`type Foo = { bar node[string] }
rule main = "a"`, `field bar of Foo must start with an uppercase letter`},
		{`type A = { Text string }
type A = { Text string }`, `type A is already defined`},
		{`rule main = "a" => $x`, `1:20: undefined capture $x`},
		{`rule main = "a" => Nope{}`, `Nope is not a struct type`},
		{`type A = { X node[string] }
rule main = v1:"a" => A{Y: $v1}`, `A has no field Y`},
		{`type A = { X node[string] }
rule main = v1:"a" => A{X: $v1, X: $v1}`, `duplicate field X`},
		{`rule main = "a" => nope(1)`, `unknown function nope`},
		{`rule main = "a" => len(1, 2)`, `len takes 1 arguments, got 2`},
		{`rule main = (x:"a") #atomic`, `capture x inside #atomic, -, or ! has no effect`},
		{`rule main = "a" @nope`, `unknown attribute @nope`},
		// A repetition element sees only its own captures, whether it has any or not.
		{`rule main = n:"a" ({$n != nil} "x")* "y"`, `1:21: capture $n is not visible in a repetition element`},
		{`rule main = n:"a" (m:"x" {$n != nil})* "y"`, `1:27: capture $n is not visible in a repetition element`},
		{`rule main = level(x, lvl)
rule x = "a"`, `rule x has no Operators Rule levels`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}
