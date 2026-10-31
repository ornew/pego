package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

func TestOperatorsRuleCST(t *testing.T) {
	t.Parallel()
	check(t, gentest.Arith,
		ok("1", `(("1")@e)@main`),
		ok("1+2*3", `((lhs:("1")@e op:"+" rhs:(lhs:("2")@e op:"*" rhs:("3")@e)@e)@e)@main`),
		ok("1 - 2 - 3", `((lhs:(lhs:("1")@e op:"-" rhs:("2")@e)@e op:"-" rhs:("3")@e)@e)@main`),
		ok("2^3^4", `((lhs:("2")@e op:"^" rhs:(lhs:("3")@e op:"^" rhs:("4")@e)@e)@e)@main`),
		ok("-2^2", `((op:"-" rhs:(lhs:("2")@e op:"^" rhs:("2")@e)@e)@e)@main`),
		ok("-2*3", `((lhs:(op:"-" rhs:("2")@e)@e op:"*" rhs:("3")@e)@e)@main`),
		ok("2^-1", `((lhs:("2")@e op:"^" rhs:(op:"-" rhs:("1")@e)@e)@e)@main`),
		ok("3!!", `((lhs:(lhs:("3")@e op:"!")@e op:"!")@e)@main`),
		ok("(1+2)*3", `((lhs:(("(" x:(lhs:("1")@e op:"+" rhs:("2")@e)@e ")"))@e op:"*" rhs:("3")@e)@e)@main`),
		ok("1==2", `((lhs:("1")@e op:"==" rhs:("2")@e)@e)@main`),
		ok("1+1==2*1", `((lhs:(lhs:("1")@e op:"+" rhs:("1")@e)@e op:"==" rhs:(lhs:("2")@e op:"*" rhs:("1")@e)@e)@e)@main`),
		// Non-associative operators do not chain.
		fails("1==2==3", `1:5: syntax error: expected end of input, "!", "*", "+", "-", "/", "^", [0-9]`),
		fails("1+", `1:3: syntax error: expected "(", "-", [0-9]`),
		fails("", `expected "(", "-", [0-9]`),
	)
}

func TestOperatorsRulePositions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Arith)
	n, err := prog.Parse("e", " 1 + 2 * 3")
	if err != nil {
		t.Fatal(err)
	}
	if value.D(n).Start != 1 || value.D(n).End != 10 {
		t.Errorf("root [%d,%d)", value.D(n).Start, value.D(n).End)
	}
	mul := value.D(n).Children[2]
	if value.D(mul).Start != 5 || value.D(mul).End != 10 {
		t.Errorf("mul [%d,%d)", value.D(mul).Start, value.D(mul).End)
	}
	if op := value.D(n).Children[1]; value.D(op).Start != 3 || value.D(op).End != 4 {
		t.Errorf("op [%d,%d)", value.D(op).Start, value.D(op).End)
	}
}

func TestOperatorsRuleActions(t *testing.T) {
	t.Parallel()
	check(t, gentest.AST,
		ok("1+2-3", `Bin{L: Bin{L: Num{Text: "1"}, Op: "+", R: Num{Text: "2"}}, Op: "-", R: Num{Text: "3"}}`),
		ok("a ? b : c ? d : e", `Cond{C: "a"@ident, F: Cond{C: "c"@ident, F: "e"@ident, T: "d"@ident}, T: "b"@ident}`),
		ok("a ? b = c : d", `Cond{C: "a"@ident, F: "d"@ident, T: Bin{L: "b"@ident, Op: "=", R: "c"@ident}}`),
		ok("-a(1)[2]", `Neg{X: Index{I: Num{Text: "2"}, X: Call{Args: [Num{Text: "1"}], Fn: "a"@ident}}}`),
		ok("f()", `Call{Args: [], Fn: "f"@ident}`),
		// The comma operator is not parsed inside arguments.
		ok("f(a = 1, b), c", `Bin{L: Call{Args: [Bin{L: "a"@ident, Op: "=", R: Num{Text: "1"}} "b"@ident], Fn: "f"@ident}, Op: ",", R: "c"@ident}`),
		// "-" and "->" are distinguished by longest match even at different binding levels.
		ok("a->b-c", `Bin{L: "a"@ident, Op: "->", R: Bin{L: "b"@ident, Op: "-", R: "c"@ident}}`),
		ok("a-b->c", `Bin{L: Bin{L: "a"@ident, Op: "-", R: "b"@ident}, Op: "->", R: "c"@ident}`),
		ok("(a)", `"a"@ident`),
	)
}

func TestOperatorsRulePositionsWithActions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.AST)
	n, err := prog.Parse("e", "a + b + c")
	if err != nil {
		t.Fatal(err)
	}
	if value.D(n).Start != 0 || value.D(n).End != 9 {
		t.Errorf("root [%d,%d)", value.D(n).Start, value.D(n).End)
	}
	if l := n.Field("L").(*value.Node); value.D(l).Start != 0 || value.D(l).End != 5 {
		t.Errorf("left [%d,%d)", value.D(l).Start, value.D(l).End)
	}
}

func TestOperatorsRuleJuxtaposition(t *testing.T) {
	t.Parallel()
	check(t, `type App = { F Expr; X Expr }
type Expr ~ App | node[string]
rule main = x:e $$ => $x
rule e Expr = #operators {
    skip " "*
    operand [a-z]+ #atomic
    level { infix left "+" => App{F: $lhs, X: $rhs} }
    level { infix left _ => App{F: $lhs, X: $rhs} }
}`,
		ok("f x y", `App{F: App{F: "f"@e, X: "x"@e}, X: "y"@e}`),
		ok("f x + g", `App{F: App{F: "f"@e, X: "x"@e}, X: "g"@e}`),
	)
}

func TestOperatorsRuleCut(t *testing.T) {
	t.Parallel()
	src := `rule main = e "+"?
rule e = #operators {
    operand [0-9]+ #atomic
    level { infix left "+" #cut }
}`
	check(t, src,
		// The operator part is a Seq, so the Operator's child is a Seq too.
		ok("1+2", `((lhs:"1"@e op:("+") rhs:"2"@e)@e nil)@main`),
		// Without the cut, the "+" after "1" would match the outer "+"?, but the cut makes it fail.
		fails("1+", `1:3: syntax error: expected [0-9]`),
	)
	check(t, `rule main = e "+"?
rule e = #operators {
    operand [0-9]+ #atomic
    level { infix left "+" }
}`,
		ok("1+", `("1"@e "+")@main`),
	)
}

func TestOperatorsRulePrefixFallback(t *testing.T) {
	t.Parallel()
	check(t, `rule main = e
rule e = #operators {
    operand "!" / [0-9]+ #atomic
    level { prefix "!" }
}`,
		ok("!1", `(op:"!" rhs:"1"@e)@e`),
		// If no operand follows a prefix operator, it is reparsed as an operand.
		ok("!", `"!"@e`),
	)
}

func TestOperatorsRuleLevelMemo(t *testing.T) {
	t.Parallel()
	// The same position yields different results at different binding levels.
	check(t, `rule main = a:level(e, mul) "+" b:e
rule e = #operators {
    operand [0-9]+ #atomic
    level { infix left "+" }
    level mul { infix left "*" }
}`,
		ok("1*2+3+4", `(a:(lhs:"1"@e op:"*" rhs:"2"@e)@e "+" b:(lhs:"3"@e op:"+" rhs:"4"@e)@e)@main`),
	)
}

func TestOperatorsRuleRestrictedPrefix(t *testing.T) {
	t.Parallel()
	check(t, gentest.RestrictedPrefix,
		ok("~é", `Prefix{Op: "~", X: Atom{Text: "é"}}`),
		ok("~~x", `Prefix{Op: "~", X: Prefix{Op: "~", X: Atom{Text: "x"}}}`),
		ok("x*y", `Binary{L: Atom{Text: "x"}, Op: "*", R: Atom{Text: "y"}}`),
		ok("é!", `Postfix{Op: "!", X: Atom{Text: "é"}}`),
		ok("x^y^z", `Binary{L: Atom{Text: "x"}, Op: "^", R: Binary{L: Atom{Text: "y"}, Op: "^", R: Atom{Text: "z"}}}`),
		// A lower-level prefix still uses its own level for its right operand.
		ok("~é+y", `Prefix{Op: "~", X: Binary{L: Atom{Text: "é"}, Op: "+", R: Atom{Text: "y"}}}`),
		ok("x*~y+z", `Binary{L: Atom{Text: "x"}, Op: "*", R: Prefix{Op: "~", X: Binary{L: Atom{Text: "y"}, Op: "+", R: Atom{Text: "z"}}}}`),
		ok("x^~y+z", `Binary{L: Atom{Text: "x"}, Op: "^", R: Prefix{Op: "~", X: Binary{L: Atom{Text: "y"}, Op: "+", R: Atom{Text: "z"}}}}`),
		// Tail operators below the active minimum are left for the caller.
		fails("x+y", "end of input"),
		fails("x|y", "end of input"),
		fails("é?", "end of input"),
		fails("~é|y", "end of input"),
		fails("~é?", "end of input"),
		fails("~", "expected"),
	)

	// A restricted lookahead and an unrestricted call at the same position must
	// not share a result with a different minimum level, even after a prefix.
	check(t, strings.Replace(gentest.RestrictedPrefix, `rule main = x:level(e, mul) $$ => $x`,
		`rule main = &(level(e, mul) "|") x:e $$ => $x`, 1),
		ok("~é+y|z", `Binary{L: Prefix{Op: "~", X: Binary{L: Atom{Text: "é"}, Op: "+", R: Atom{Text: "y"}}}, Op: "|", R: Atom{Text: "z"}}`),
		fails("~é+y", `1:1: syntax error`),
	)
}

// TestOperatorsRuleEmptyPrefixAndPostfix checks that prefix and postfix operators that match the empty string
// are not applied, instead of being applied forever.
func TestOperatorsRuleEmptyPrefixAndPostfix(t *testing.T) {
	t.Parallel()
	check(t, `rule main = e $$
rule e = #operators {
    operand "x"
    level { prefix "-"? }
    level { postfix "!"? }
}`,
		ok("x", `("x"@e)@main`),
		ok("-x!", `((op:"-" rhs:(lhs:"x"@e op:"!")@e)@e)@main`),
		fails("", "expected"),
	)
}

// TestOperatorsRuleEmptyApplication checks that an operator application that consumes no input ends the
// loop, as an empty iteration ends a repetition: with an operand and an operator that can both
// match empty, the loop would never end.
func TestOperatorsRuleEmptyApplication(t *testing.T) {
	t.Parallel()
	check(t, `rule main = e $$
rule e = #operators {
    operand "a"?
    level { infix left _ }
}`,
		ok("a", `((lhs:"a"@e op:() nil)@e)@main`),
		ok("", `((nil op:() nil)@e)@main`))
	check(t, `rule main = e $$
rule e = #operators {
    operand "a"?
    level { postfix "!"? }
}`,
		ok("a!!", `((lhs:(lhs:"a"@e op:"!")@e op:"!")@e)@main`))
}
