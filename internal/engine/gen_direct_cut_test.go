package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var typedDirectCutCases = []genCase{
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
rule main Doc = p:pair "!" $$ / p:pair "?" $$ => Doc{Pair: $p}
rule pair Pair = word:[a-z]+ #atomic #cut {n := len($word)} ":" [0-9]+ => Pair{Word: $word, N: n}
`, []string{"abc:12!", "abc:12?", "abc:x?", "abc:12", ""}},
	{"typed/direct_cut_outer_rollback", `type Doc = { Text node[string]; N int }
rule main Doc = {n := 0} (v:"a" #atomic {n := 1} ("b" #cut "c")? "x" / v:"ab" #atomic {n := 2}) $$ => Doc{Text: $v, N: n}
`, []string{"ab", "abcx", "ax", "abd", ""}},
	{"typed/direct_cut_repeat_rollback", `type Doc = { Text node[string]; N int }
rule main Doc = {n := 0} (v:"a" #atomic {n := 1} ("b" #cut {n := n + 1} "c") #repeat(0, 2) "x" / v:"ab" #atomic {n := 2}) $$ => Doc{Text: $v, N: n}
`, []string{"ab", "abcx", "abcbcx", "ax", "abd", ""}},
	{"typed/direct_cut_captured_lookahead", `type Doc = { Text node[string] }
rule main Doc = (&probe:("a" #cut "b") #atomic v:"abx" #atomic / v:"aby" #atomic) $$ => Doc{Text: $v}
`, []string{"abx", "aby", "ab", "ac", ""}},
}

func TestGeneratedTypedDirectCuts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range typedDirectCutCases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := Generate(g, GenOptions{Package: "cuts", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(string(code), "TParse(trules") {
			t.Fatalf("%s: typed runtime must be exercised", c.name)
		}
		if !strings.Contains(string(code), "// main, invoked as by invoke (body inlined)") &&
			!strings.Contains(string(code), "// main, called as by invokePlain (body inlined)") {
			t.Fatalf("%s: main must use the direct typed route", c.name)
		}
		if strings.Contains(string(code), "disableTypedCuts") {
			t.Fatalf("%s: generation switch must not enter the runtime", c.name)
		}
		if c.name == "typed/direct_cut_callee" && !strings.Contains(string(code), "// part, called as by invokePlain (body inlined)") {
			t.Fatal("callee containing cuts must use the direct typed route")
		}
		reference, err := Generate(g, GenOptions{Package: "cuts", Start: "main", Types: true, disableTypedCuts: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range compile(t, c.src).rules {
			if !directCuts(r.def.Expr) {
				continue
			}
			inlined := false
			for _, route := range []string{"invoked as by invoke", "called as by invokePlain"} {
				marker := "// " + r.name + ", " + route + " (body inlined)"
				inlined = inlined || strings.Contains(string(code), marker)
				if strings.Contains(string(reference), marker) {
					t.Fatalf("%s: disabled cut optimization must retain the general %s body", c.name, r.name)
				}
			}
			if !inlined {
				t.Fatalf("%s: cut-bearing %s must use the direct typed route", c.name, r.name)
			}
		}
	}
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), typedDirectCutCases, GenOptions{disableTypedCuts: true})
}
