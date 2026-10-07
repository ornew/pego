package engine

import (
	"testing"
)

func TestTerminals(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		check(t, `def main = "abc"`,
			ok("abc", `"abc"@main`),
			fails("abd", `1:1: syntax error: expected "abc"`),
			fails("ab", `expected "abc"`),
			fails("abcd", `1:4: syntax error: expected end of input`),
		)
	})
	t.Run("unicode", func(t *testing.T) {
		check(t, `def main = "日本" (?ぁ-ん)`,
			ok("日本ご", `(Seq "日本" "ご")@main`),
			fails("日本ア", `1:3: syntax error: expected (?ぁ-ん)`),
		)
	})
	t.Run("character class", func(t *testing.T) {
		check(t, `def main = (?a-c0-9_)+`,
			ok("a0_c9", `["a" "0" "_" "c" "9"]@main`),
			fails("ad", `1:2: syntax error: expected (?a-c0-9_), end of input`),
		)
	})
	t.Run("negated class", func(t *testing.T) {
		check(t, `def main = "\"" @(?^"\\)* "\""`,
			ok(`"hi there"`, `(Seq "\"" "hi there" "\"")@main`),
			fails(`"a\"`, `expected "\""`),
		)
	})
	t.Run("any", func(t *testing.T) {
		check(t, `def main = . . .`,
			ok("a日\n", `(Seq "a" "日" "\n")@main`),
			fails("ab", `1:3: syntax error: expected any character`),
		)
	})
	t.Run("top", func(t *testing.T) {
		check(t, `def main = "a" _`,
			ok("a", `(Seq "a" "")@main`),
		)
	})
	t.Run("bottom", func(t *testing.T) {
		check(t, `def main = "a" / _|_`,
			ok("a", `"a"@main`),
			fails("b", `expected "a"`),
		)
	})
}

func TestSequenceAndChoice(t *testing.T) {
	check(t, `def main = "a" ("b" / "c" "d") "e"`,
		ok("abe", `(Seq "a" "b" "e")@main`),
		ok("acde", `(Seq "a" (Seq "c" "d") "e")@main`),
		fails("ace", `1:3: syntax error: expected "d"`),
		fails("axe", `1:2: syntax error: expected "b", "c"`),
	)
	// A choice builds no node and returns the value of the chosen alternative as is.
	check(t, `
def main = a / b
def a = "a"
def b = "b" "b"`,
		ok("a", `"a"@a`),
		ok("bb", `(Seq "b" "b")@b`),
	)
	// Even if an earlier alternative fails partway through, the next alternative is tried from the
	// original position.
	check(t, `def main = "a" "b" "c" / "a" "b" / "a"`,
		ok("ab", `(Seq "a" "b")@main`),
		ok("a", `"a"@main`),
	)
}

func TestRepetition(t *testing.T) {
	check(t, `def main = "a"*`,
		ok("", `[]@main`),
		ok("aaa", `["a" "a" "a"]@main`),
	)
	check(t, `def main = "a"+`,
		ok("a", `["a"]@main`),
		fails("", `expected "a"`),
	)
	check(t, `def main = "a"{2,3} "b"`,
		ok("aab", `(Seq ["a" "a"] "b")@main`),
		ok("aaab", `(Seq ["a" "a" "a"] "b")@main`),
		fails("ab", `1:2: syntax error: expected "a"`),
		fails("aaaab", `1:4: syntax error: expected "b"`),
	)
	check(t, `def main = "a"{2}`,
		ok("aa", `["a" "a"]@main`),
		fails("aaa", `expected end of input`),
	)
	check(t, `def main = "a"{2,}`,
		ok("aaaa", `["a" "a" "a" "a"]@main`),
		fails("a", `expected "a"`),
	)
	t.Run("nullable body terminates", func(t *testing.T) {
		check(t, `def main = ("a"?)* "b"`,
			ok("aab", `(Seq ["a" "a" nil] "b")@main`),
			ok("b", `(Seq [nil] "b")@main`),
		)
	})
	t.Run("explicit {0,1} is a list", func(t *testing.T) {
		check(t, `def main = "a"{0,1} "b"`,
			ok("b", `(Seq [] "b")@main`),
			ok("ab", `(Seq ["a"] "b")@main`),
		)
	})
}

func TestOptional(t *testing.T) {
	check(t, `def main = "a"? "b"`,
		ok("ab", `(Seq "a" "b")@main`),
		ok("b", `(Seq nil "b")@main`),
	)
}

func TestLookahead(t *testing.T) {
	check(t, `def main = !"if" @(?a-z)+`,
		fails("iffy", `1:1: syntax error`),
		ok("abc", `(Seq "abc")@main`),
	)
	check(t, `def main = &"a" (?a-z)`,
		ok("a", `(Seq "a")@main`),
		fails("b", `1:1: syntax error`),
	)
	// Failures inside a lookahead are not reported as expectations.
	check(t, `def main = !"x" "y"`,
		fails("z", `1:1: syntax error: expected "y"`),
	)
}

func TestAtomicAndDiscard(t *testing.T) {
	check(t, `def main = @("a" (?0-9)+) -" "+ "b"`,
		ok("a12   b", `(Seq "a12" "b")@main`),
	)
	check(t, `
def main = word (-s word)+
def s = " "+
def word = @(?a-z)+`,
		ok("ab cd  ef", `(Seq "ab"@word [(Seq "cd"@word) (Seq "ef"@word)])@main`),
	)
}

func TestCut(t *testing.T) {
	check(t, `def main = "a" -- "b" / "a" -- "c"`,
		ok("ab", `(Seq "a" "b")@main`),
		fails("ac", `1:2: syntax error: expected "b"`),
	)
	// A cut commits only the innermost choice.
	check(t, `def main = ("a" -- "b" / "x") / "a" "c"`,
		ok("ac", `(Seq "a" "c")@main`),
	)
	// A failure after passing a cut inside a repetition element fails the whole repetition.
	check(t, `def main = ("(" -- "x" ")")* "("`,
		ok("(x)(x)", `error: 1:7: syntax error: expected "("`),
		fails("(x)(", `1:5: syntax error: expected "x"`),
	)
	// A cut does not propagate outside the rule.
	check(t, `
def main = a / "a" "c"
def a = "a" -- "b"`,
		ok("ac", `(Seq "a" "c")@main`),
	)
}

func TestAnchors(t *testing.T) {
	check(t, `def main = ^^ "a" $$`,
		ok("a", `(Seq "a")@main`),
	)
	check(t, `def main = (^ @(?a-z)+ $ "\n"?)*`,
		ok("ab\ncd\n", `[(Seq "ab" "\n") (Seq "cd" "\n")]`+"@main"),
	)
	check(t, `def main = "a" ^ "b"`,
		fails("ab", `1:2: syntax error: expected beginning of line`),
	)
	check(t, `def main = "a" $ "b"`,
		fails("ab", `1:2: syntax error: expected end of line`),
	)
}

func TestRuleNodes(t *testing.T) {
	check(t, `
def main = a b c
def a = "a"
def b = x
def x = "b"
def c = @("c"+)`,
		// b passes the rule through, so it returns x's node as is.
		ok("abcc", `(Seq "a"@a "b"@x "cc"@c)@main`),
	)
}

func TestTerminalType(t *testing.T) {
	check(t, `
type Number terminal
def main = n:number "+" number
def number: Number = (?0-9)+`,
		ok("12+3", `(Seq Number"12"@number "+" Number"3"@number n=Number"12"@number)@main`),
	)
}

func TestLeftRecursion(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		check(t, `
def main = expr
def expr = expr "+" term / term
def term = @(?0-9)+`,
			ok("1", `"1"@term`),
			ok("1+2+3", `(Seq (Seq "1"@term "+" "2"@term)@expr "+" "3"@term)@expr`),
			fails("1+", `1:3: syntax error: expected (?0-9)`),
		)
	})
	t.Run("indirect", func(t *testing.T) {
		check(t, `
def main = a
def a = b "x" / "y"
def b = a "z" / c
def c = a "w" / "v"`,
			ok("y", `"y"@a`),
			ok("vx", `(Seq "v"@c "x")@a`),
			ok("yzx", `(Seq (Seq "y"@a "z")@b "x")@a`),
			ok("ywx", `(Seq (Seq "y"@a "w")@c "x")@a`),
			ok("yzxwx", `(Seq (Seq (Seq (Seq "y"@a "z")@b "x")@a "w")@c "x")@a`),
		)
	})
	t.Run("precedence climbing", func(t *testing.T) {
		check(t, `
def main = add
def add = add "+" mul / add "-" mul / mul
def mul = mul "*" num / num
def num = @(?0-9)`,
			ok("1+2*3-4", `(Seq (Seq "1"@num "+" (Seq "2"@num "*" "3"@num)@mul)@add "-" "4"@num)@add`),
		)
	})
	t.Run("nullable prefix", func(t *testing.T) {
		check(t, `
def main = a
def a = "x"? a "y" / "z"`,
			ok("zyy", `(Seq nil (Seq nil "z"@a "y")@a "y")@a`),
			// The inner a also greedily consumes every "y", so input starting with "x" does not match.
			fails("xzy", `1:4: syntax error: expected "y"`),
		)
	})
	t.Run("in lookahead", func(t *testing.T) {
		check(t, `
def main = &e e
def e = e "1" / "0"`,
			ok("011", `(Seq (Seq (Seq "0"@e "1")@e "1")@e)@main`),
		)
	})
}

func TestNullableLeftRecursionTerminates(t *testing.T) {
	check(t, `
def main = a "b"
def a = a "x" / _`,
		ok("xxb", `(Seq (Seq (Seq ""@a "x")@a "x")@a "b")@main`),
	)
}

// TestMemoKeyedByVariables checks that a rule that reads variables is memoized per value of those
// variables: after the first alternative fails, r is called again at the same position with a
// different value of n.
func TestMemoKeyedByVariables(t *testing.T) {
	src := `
def main = [n = 1] r "x" / [n = 2] r .*
def r = [n == 1] "a" / [n == 2] "b"`
	check(t, src,
		ok("ax", `(Seq (Seq "a")@r "x")@main`),
		ok("b", `(Seq (Seq "b")@r [])@main`),
		fails("ay", "expected"), // reusing the n == 1 result for n == 2 would accept "ay"
	)
	prog := compile(t, src)
	if r := prog.byName["r"]; !r.memo || len(r.vars) != 1 || r.vars[0] != "n" {
		t.Errorf("r: memo %v, vars %v", r.memo, r.vars)
	}
}
