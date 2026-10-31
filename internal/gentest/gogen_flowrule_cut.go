package gentest

import (
	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
)

var TypedDirectCutCases = []Case{
	{"typed/direct_cut_choice", `type Doc = { Text node[string] }
rule main Doc = v:("a" #cut "b" / "a" "c") #atomic $$ => Doc{Text: $v}
`, []string{"ab", "ac", "a", "abc", ""}},
	{"typed/direct_cut_nested_choice", `type Doc = { Text node[string] }
rule main Doc = v:("a" ("b" #cut "c" / "b" "d") "x" / "abcy") #atomic $$ => Doc{Text: $v}
`, []string{"abcx", "abcy", "abdx", "abc", ""}},
	{"typed/direct_cut_optional", `type Doc = { Text node[string] }
rule main Doc = v:(("b" #cut "c")? "b") #atomic $$ => Doc{Text: $v}
`, []string{"bcb", "b", "bb", "", "bc"}},
	{"typed/direct_cut_repeat", `type Doc = { Text node[string] }
rule main Doc = v:(("b" #cut "c") #repeat(0, 2) "b") #atomic $$ => Doc{Text: $v}
`, []string{"bcbcb", "b", "bcb", "bcbc", "", "bcbcbcb"}},
	{"typed/direct_cut_projection", `type Doc = { Items []node[string] }
rule main Doc = items:(-"," #cut x:[a-z] #atomic) #repeat(0, 3) ";" $$ => Doc{Items: map($items, (e) => e.$x)}
`, []string{";", ",a;", ",a,b;", ",;", ",a,b,c;", ",a,b,c,d;", ""}},
	{"typed/direct_cut_lookahead", `type Doc = { Text node[string] }
rule main Doc = v:(&("a" #cut "b") "ab" / !("a" #cut "b") "ac") #atomic $$ => Doc{Text: $v}
`, []string{"ab", "ac", "ad", "", "abc"}},
	{"typed/direct_cut_callee", `type Doc = { Text node[string] }
rule main Doc = v:(part "x" / "ac") #atomic $$ => Doc{Text: $v}
rule part = "a" #cut "b"
`, []string{"abx", "ac", "ab", "ad", ""}},
	{"typed/direct_cut_error", `type Doc = { Text node[string] }
rule main Doc = v:(("é" #cut "x") @error(message="expected committed word") / "éy") #atomic $$ => Doc{Text: $v}
`, []string{"éx", "éy", "é", "x", ""}},
	{"typed/direct_cut_memo_capture", `type Pair = { Word node[string]; N int }
type Doc = { Pair Pair }
var n int
rule main Doc = p:pair "!" $$ / p:pair "?" $$ => Doc{Pair: $p}
rule pair Pair = word:[a-z]+ #atomic #cut {n := len($word)} ":" [0-9]+ => Pair{Word: $word, N: n}
`, []string{"abc:12!", "abc:12?", "abc:x?", "abc:12", ""}},
	{"typed/direct_cut_outer_rollback", `type Doc = { Text node[string]; N int }
var n int
rule main Doc = {n := 0} (v:"a" #atomic {n := 1} ("b" #cut "c")? "x" / v:"ab" #atomic {n := 2}) $$ => Doc{Text: $v, N: n}
`, []string{"ab", "abcx", "ax", "abd", ""}},
	{"typed/direct_cut_repeat_rollback", `type Doc = { Text node[string]; N int }
var n int
rule main Doc = {n := 0} (v:"a" #atomic {n := 1} ("b" #cut {n := n + 1} "c") #repeat(0, 2) "x" / v:"ab" #atomic {n := 2}) $$ => Doc{Text: $v, N: n}
`, []string{"ab", "abcx", "abcbcx", "ax", "abd", ""}},
	{"typed/direct_cut_captured_lookahead", `type Doc = { Text node[string] }
rule main Doc = (&probe:("a" #cut "b") #atomic v:"abx" #atomic / v:"aby" #atomic) $$ => Doc{Text: $v}
`, []string{"abx", "aby", "ab", "ac", ""}},
}

// DirectCuts reports whether the expression contains a cut.
func DirectCuts(e grammar.Expr) bool {
	found := false
	compile.WalkExpr(e, func(e grammar.Expr) {
		if _, ok := e.(*grammar.Cut); ok {
			found = true
		}
	})
	return found
}
