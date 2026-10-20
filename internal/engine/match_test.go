package engine

import (
	"testing"
)

func TestTerminals(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		check(t, `rule main = "abc"`,
			ok("abc", `"abc"@main`),
			fails("abd", `1:1: syntax error: expected "abc"`),
			fails("ab", `expected "abc"`),
			fails("abcd", `1:4: syntax error: expected end of input`),
		)
	})
	t.Run("unicode", func(t *testing.T) {
		check(t, `rule main = "日本" [ぁ-ん]`,
			ok("日本ご", `("日本" "ご")@main`),
			fails("日本ア", `1:3: syntax error: expected [ぁ-ん]`),
		)
	})
	t.Run("character class", func(t *testing.T) {
		check(t, `rule main = [a-c0-9_]+`,
			ok("a0_c9", `["a" "0" "_" "c" "9"]@main`),
			fails("ad", `1:2: syntax error: expected [a-c0-9_], end of input`),
		)
	})
	t.Run("negated class", func(t *testing.T) {
		check(t, `rule main = "\"" [^"\\]* #atomic "\""`,
			ok(`"hi there"`, `("\"" "hi there" "\"")@main`),
			fails(`"a\"`, `expected "\""`),
		)
	})
	t.Run("any", func(t *testing.T) {
		check(t, `rule main = . . .`,
			ok("a日\n", `("a" "日" "\n")@main`),
			fails("ab", `1:3: syntax error: expected any character`),
		)
	})
	t.Run("top", func(t *testing.T) {
		check(t, `rule main = "a" _`,
			ok("a", `("a")@main`),
		)
	})
	t.Run("bottom", func(t *testing.T) {
		check(t, `rule main = "a" / _|_`,
			ok("a", `"a"@main`),
			fails("b", `expected "a"`),
		)
	})
}

func TestSequenceAndChoice(t *testing.T) {
	check(t, `rule main = "a" ("b" / "c" "d") "e"`,
		ok("abe", `("a" "b" "e")@main`),
		ok("acde", `("a" ("c" "d") "e")@main`),
		fails("ace", `1:3: syntax error: expected "d"`),
		fails("axe", `1:2: syntax error: expected "b", "c"`),
	)
	// A choice builds no node and returns the value of the chosen alternative as is.
	check(t, `rule main = a / b
rule a = "a"
rule b = "b" "b"`,
		ok("a", `"a"@a`),
		ok("bb", `("b" "b")@b`),
	)
	// Even if an earlier alternative fails partway through, the next alternative is tried from the
	// original position.
	check(t, `rule main = "a" "b" "c" / "a" "b" / "a"`,
		ok("ab", `("a" "b")@main`),
		ok("a", `"a"@main`),
	)
}

func TestRepetition(t *testing.T) {
	check(t, `rule main = "a"*`,
		ok("", `[]@main`),
		ok("aaa", `["a" "a" "a"]@main`),
	)
	check(t, `rule main = "a"+`,
		ok("a", `["a"]@main`),
		fails("", `expected "a"`),
	)
	check(t, `rule main = "a" #repeat(2, 3) "b"`,
		ok("aab", `(["a" "a"] "b")@main`),
		ok("aaab", `(["a" "a" "a"] "b")@main`),
		fails("ab", `1:2: syntax error: expected "a"`),
		fails("aaaab", `1:4: syntax error: expected "b"`),
	)
	check(t, `rule main = "a" #repeat(2)`,
		ok("aa", `["a" "a"]@main`),
		fails("aaa", `expected end of input`),
	)
	check(t, `rule main = "a" #repeat(2, )`,
		ok("aaaa", `["a" "a" "a" "a"]@main`),
		fails("a", `expected "a"`),
	)
	t.Run("nullable body terminates", func(t *testing.T) {
		check(t, `rule main = "a"?* "b"`,
			ok("aab", `(["a" "a" nil] "b")@main`),
			ok("b", `([nil] "b")@main`),
		)
	})
	t.Run("explicit {0,1} is a list", func(t *testing.T) {
		check(t, `rule main = "a" #repeat(0, 1) "b"`,
			ok("b", `([] "b")@main`),
			ok("ab", `(["a"] "b")@main`),
		)
	})
}

func TestOptional(t *testing.T) {
	check(t, `rule main = "a"? "b"`,
		ok("ab", `("a" "b")@main`),
		ok("b", `(nil "b")@main`),
	)
}

func TestLookahead(t *testing.T) {
	check(t, `rule main = !"if" [a-z]+ #atomic`,
		fails("iffy", `1:1: syntax error`),
		ok("abc", `("abc")@main`),
	)
	check(t, `rule main = &"a" [a-z]`,
		ok("a", `("a")@main`),
		fails("b", `1:1: syntax error`),
	)
	// Failures inside a lookahead are not reported as expectations.
	check(t, `rule main = !"x" "y"`,
		fails("z", `1:1: syntax error: expected "y"`),
	)
}

func TestAtomicAndDiscard(t *testing.T) {
	check(t, `rule main = ("a" [0-9]+) #atomic -" "+ "b"`,
		ok("a12   b", `("a12" "b")@main`),
	)
	check(t, `rule main = word (-s word)+
rule s = " "+
rule word = [a-z]+ #atomic`,
		ok("ab cd  ef", `("ab"@word [("cd"@word) ("ef"@word)])@main`),
	)
}

func TestCut(t *testing.T) {
	check(t, `rule main = "a" #cut "b" / "a" #cut "c"`,
		ok("ab", `("a" "b")@main`),
		fails("ac", `1:2: syntax error: expected "b"`),
	)
	// A cut commits only the innermost choice.
	check(t, `rule main = ("a" #cut "b" / "x") / "a" "c"`,
		ok("ac", `("a" "c")@main`),
	)
	// A failure after passing a cut inside a repetition element fails the whole repetition.
	check(t, `rule main = ("(" #cut "x" ")")* "("`,
		ok("(x)(x)", `error: 1:7: syntax error: expected "("`),
		fails("(x)(", `1:5: syntax error: expected "x"`),
	)
	// A cut does not propagate outside the rule.
	check(t, `rule main = a / "a" "c"
rule a = "a" #cut "b"`,
		ok("ac", `("a" "c")@main`),
	)
}

func TestAnchors(t *testing.T) {
	check(t, `rule main = ^^ "a" $$`,
		ok("a", `("a")@main`),
	)
	check(t, `rule main = (^ [a-z]+ #atomic $ "\n"?)*`,
		ok("ab\ncd\n", `[("ab" "\n") ("cd" "\n")]@main`),
	)
	check(t, `rule main = "a" ^ "b"`,
		fails("ab", `1:2: syntax error: expected beginning of line`),
	)
	check(t, `rule main = "a" $ "b"`,
		fails("ab", `1:2: syntax error: expected end of line`),
	)
}

func TestRuleNodes(t *testing.T) {
	check(t, `rule main = a b c
rule a = "a"
rule b = x
rule x = "b"
rule c = "c"+ #atomic`,
		// b passes the rule through, so it returns x's node as is.
		ok("abcc", `("a"@a "b"@x "cc"@c)@main`),
	)
}

func TestTerminalType(t *testing.T) {
	check(t, `type Number = { Text string }
rule main = n:number "+" number
rule number Number = t:([0-9]+) #atomic => Number{Text: text($t)}`,
		ok("12+3", `(n:Number{Text: "12"} "+" Number{Text: "3"})@main`),
	)
}

func TestLeftRecursion(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		check(t, `rule main = expr
rule expr = expr "+" term / term
rule term = [0-9]+ #atomic`,
			ok("1", `"1"@term`),
			ok("1+2+3", `(("1"@term "+" "2"@term)@expr "+" "3"@term)@expr`),
			fails("1+", `1:3: syntax error: expected [0-9]`),
		)
	})
	t.Run("indirect", func(t *testing.T) {
		check(t, `rule main = a
rule a = b "x" / "y"
rule b = a "z" / c
rule c = a "w" / "v"`,
			ok("y", `"y"@a`),
			ok("vx", `("v"@c "x")@a`),
			ok("yzx", `(("y"@a "z")@b "x")@a`),
			ok("ywx", `(("y"@a "w")@c "x")@a`),
			ok("yzxwx", `(((("y"@a "z")@b "x")@a "w")@c "x")@a`),
		)
	})
	t.Run("precedence climbing", func(t *testing.T) {
		check(t, `rule main = add
rule add = add "+" mul / add "-" mul / mul
rule mul = mul "*" num / num
rule num = [0-9] #atomic`,
			ok("1+2*3-4", `(("1"@num "+" ("2"@num "*" "3"@num)@mul)@add "-" "4"@num)@add`),
		)
	})
	t.Run("nullable prefix", func(t *testing.T) {
		check(t, `rule main = a
rule a = "x"? a "y" / "z"`,
			ok("zyy", `(nil (nil "z"@a "y")@a "y")@a`),
			// The inner a also greedily consumes every "y", so input starting with "x" does not match.
			fails("xzy", `1:4: syntax error: expected "y"`),
		)
	})
	t.Run("in lookahead", func(t *testing.T) {
		check(t, `rule main = &e e
rule e = e "1" / "0"`,
			ok("011", `((("0"@e "1")@e "1")@e)@main`),
		)
	})
}

func TestNullableLeftRecursionTerminates(t *testing.T) {
	check(t, `rule main = a "b"
rule a = a "x" / _`,
		ok("xxb", `(((nil "x")@a "x")@a "b")@main`),
	)
}

// TestMemoKeyedByVariables checks that a rule that reads variables is memoized per value of those
// variables: after the first alternative fails, r is called again at the same position with a
// different value of n.
func TestMemoKeyedByVariables(t *testing.T) {
	src := `var n int
rule main = {n := 1} r "x" / {n := 2} r .*
rule r = {n == 1} "a" / {n == 2} "b"`
	check(t, src,
		ok("ax", `(("a")@r "x")@main`),
		ok("b", `(("b")@r [])@main`),
		fails("ay", "expected"), // reusing the n == 1 result for n == 2 would accept "ay"
	)
	prog := compile(t, src)
	if r := prog.byName["r"]; !r.memo || len(r.vars) != 1 || r.vars[0] != "n" {
		t.Errorf("r: memo %v, vars %v", r.memo, r.vars)
	}
}
