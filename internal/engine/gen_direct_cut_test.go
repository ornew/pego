package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var typedDirectCutCases = []genCase{
	{"typed/direct_cut_choice", `
type Doc struct { Text Match }
def main: Doc = v:@("a" -- "b" / "a" "c") $$ -> new Doc{Text: $v}
`, []string{"ab", "ac", "a", "abc", ""}},
	{"typed/direct_cut_nested_choice", `
type Doc struct { Text Match }
def main: Doc = v:@("a" ("b" -- "c" / "b" "d") "x" / "abcy") $$ -> new Doc{Text: $v}
`, []string{"abcx", "abcy", "abdx", "abc", ""}},
	{"typed/direct_cut_optional", `
type Doc struct { Text Match }
def main: Doc = v:@(("b" -- "c")? "b") $$ -> new Doc{Text: $v}
`, []string{"bcb", "b", "bb", "", "bc"}},
	{"typed/direct_cut_repeat", `
type Doc struct { Text Match }
def main: Doc = v:@(("b" -- "c"){0,2} "b") $$ -> new Doc{Text: $v}
`, []string{"bcbcb", "b", "bcb", "bcbc", "", "bcbcbcb"}},
	{"typed/direct_cut_projection", `
type Doc struct { Items []Match }
def main: Doc = items:(-"," -- x:@(?a-z)){0,3} ";" $$ -> new Doc{Items: map($items, (e) => $e.x)}
`, []string{";", ",a;", ",a,b;", ",;", ",a,b,c;", ",a,b,c,d;", ""}},
	{"typed/direct_cut_lookahead", `
type Doc struct { Text Match }
def main: Doc = v:@(&("a" -- "b") "ab" / !("a" -- "b") "ac") $$ -> new Doc{Text: $v}
`, []string{"ab", "ac", "ad", "", "abc"}},
	{"typed/direct_cut_callee", `
type Doc struct { Text Match }
def main: Doc = v:@(part "x" / "ac") $$ -> new Doc{Text: $v}
def part = "a" -- "b"
`, []string{"abx", "ac", "ab", "ad", ""}},
	{"typed/direct_cut_error", `
type Doc struct { Text Match }
def main: Doc = v:@(("é" -- "x") #error(message="expected committed word") / "éy") $$ -> new Doc{Text: $v}
`, []string{"éx", "éy", "é", "x", ""}},
	{"typed/direct_cut_memo_capture", `
type Pair struct { Word Match, N int }
type Doc struct { Pair Pair }
def main: Doc = (p:pair "!" $$ / p:pair "?" $$) -> new Doc{Pair: $p}
def pair: Pair = word:@(?a-z)+ -- [n = len($word)] ":" (?0-9)+ -> new Pair{Word: $word, N: n}
`, []string{"abc:12!", "abc:12?", "abc:x?", "abc:12", ""}},
	{"typed/direct_cut_outer_rollback", `
type Doc struct { Text Match, N int }
def main: Doc = [n = 0] (v:@"a" [n = 1] ("b" -- "c")? "x" / v:@"ab" [n = 2]) $$ -> new Doc{Text: $v, N: n}
`, []string{"ab", "abcx", "ax", "abd", ""}},
	{"typed/direct_cut_repeat_rollback", `
type Doc struct { Text Match, N int }
def main: Doc = [n = 0] (v:@"a" [n = 1] ("b" -- [n = n + 1] "c"){0,2} "x" / v:@"ab" [n = 2]) $$ -> new Doc{Text: $v, N: n}
`, []string{"ab", "abcx", "abcbcx", "ax", "abd", ""}},
	{"typed/direct_cut_captured_lookahead", `
type Doc struct { Text Match }
def main: Doc = (&(probe:@("a" -- "b")) v:@"abx" / v:@"aby") $$ -> new Doc{Text: $v}
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
		if !strings.Contains(string(code), "tparse(trules") {
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
