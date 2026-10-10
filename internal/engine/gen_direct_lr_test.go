package engine

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var typedLRBodyCases = []genCase{
	{"typed/LR_direct", `
type Number terminal
type Add struct { Left Expr, Right Number }
type Expr = Number | Add
def main: Expr = e:expr $$ -> $e
def expr: Expr = add / number
def add: Expr = l:expr "+" r:number -> new Add{Left: $l, Right: $r}
def number: Number = (?0-9)+
`, []string{"1", "1+2+3", "1+", "1++2", "", "1x"}},
	{"typed/LR_indirect", `
type Doc struct { Text Match }
def main: Doc = x:@a $$ -> new Doc{Text: $x}
def a = b "x" / "y"
def b = a "z" / c
def c = a "w" / "v"
`, []string{"y", "vx", "yzxwx", "yz", "", "v"}},
	{"typed/LR_hidden", `
type Doc struct { Text Match }
def main: Doc = x:@expr $$ -> new Doc{Text: $x}
def expr = "" expr "x" / "é"
`, []string{"é", "éxx", "éx?", "", "x"}},
	{"typed/LR_nullable_seed", `
type Doc struct { N int }
def main: Doc = e:expr $$ -> $e
def expr: Doc = step / seed
def step: Doc = p:expr "a" -> new Doc{N: len(text($p)) + 1}
def seed: Doc = "" -> new Doc{N: 0}
`, []string{"", "a", "aaa", "a?", "b"}},
	{"typed/LR_no_improvement", `
type Doc struct { N int }
def main: Doc = e:expr $$ -> $e
def expr: Doc = step / seed
def step: Doc = p:expr -> new Doc{N: len(text($p)) + 1}
def seed: Doc = "a" -> new Doc{N: 0}
`, []string{"a", "", "aa", "b"}},
	{"typed/LR_projected_repeat", `
type Doc struct { Items []Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = (p:expr ";" items:(x:@(?a-z) -","){1,3}
             / items:(x:@(?a-z) -","){1,3}) -> new Doc{Items: map($items, (e) => $e.x)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a,b,c,d,", "a,;c,?", ""}},
	{"typed/LR_element_frames", `
type Doc struct { N int }
def main: Doc = e:expr $$ -> $e
def expr: Doc = (p:expr ";" items:(x:@(?a-z) [len($x) == 1] -","){1,3}
             / items:(x:@(?a-z) [len($x) == 1] -","){1,3}) -> new Doc{N: len($items)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a,b,c,d,", "a,;c,?", ""}},
	{"typed/LR_rollback_environment", `
type Doc struct { Text Match, N int }
def main: Doc = e:expr $$ -> $e
def expr: Doc = [n = 0] (p:expr "+" [n = 1] x:@(?a-z) "!" / x:@"é" [n = 2]) -> new Doc{Text: $x, N: n}
`, []string{"é", "é+a!", "é+a!+b!", "é+a", "é+a!+", "", "a"}},
	{"typed/LR_lookahead_memo", `
type Doc struct { Text Match }
def main: Doc = &(expr "!") e:expr "!" $$ -> $e
def expr: Doc = (p:expr "+" &(x:@(?a-z)) x:@(?a-z)
             / x:@(?a-z)) -> new Doc{Text: $x}
`, []string{"a!", "a+b+c!", "a+!", "a+b?", "", "é!"}},
	{"typed/LR_unreachable_capture", `
type Doc struct { Text *Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = (p:expr "x" x:@""
             / x:@"a"
             / (_|_ ignored:@"b")) -> new Doc{Text: $x}
`, []string{"a", "axx", "b", "", "a?"}},
	{"typed/LR_positional_action", `
type Doc struct { Text string }
def main: Doc = e:expr $$ -> $e
def expr: Doc = (expr "+" @"é" / @"é") -> new Doc{Text: text($0)}
`, []string{"é", "é+é+é", "é+", "é+é?", "", "a"}},
	{"typed/LR_framed_cuts", `
type Doc struct { Text Match }
def main: Doc = x:@expr $$ -> new Doc{Text: $x}
def expr = expr "+" -- "a" / "a"
`, []string{"a", "a+a+a", "a+", "a+a?", "", "b"}},
	{"typed/LR_recovery_fallback", `
type Doc struct { Text Match }
def main: Doc = x:@expr $$ -> new Doc{Text: $x}
def expr = (expr "+" "a" / "a") #recover(skip=";")
`, []string{"a", "a+a+a", "a+", ";", "a+a?", "", "b"}},
}

func TestGeneratedTypedLRBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range typedLRBodyCases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "lrbody", Start: "main", Types: true, Recognize: true}
		code, err := Generate(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(string(code), "tparse(trules") {
			t.Fatalf("%s: typed runtime is not exercised", c.name)
		}
		inlined := strings.Contains(string(code), "(typed LR body inlined)")
		if strings.HasSuffix(c.name, "_fallback") {
			if inlined {
				t.Fatalf("%s: unsupported body must retain the general route", c.name)
			}
		} else if !inlined {
			t.Fatalf("%s: no unfinished typed LR body", c.name)
		}
		opts.disableTypedLRBodies = true
		ref, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(ref), "(typed LR body inlined)") || strings.Contains(string(code), "disableTypedLRBodies") {
			t.Fatalf("%s: invalid generation-time control", c.name)
		}
		for _, r := range compile(t, c.src).rules {
			if r.leader && strings.Contains(string(code), "// "+r.name+", invoked as by invoke (body inlined)") {
				t.Fatalf("%s: leader %s must keep the growth wrapper", c.name, r.name)
			}
		}
		// The selector must not change Node parsing or recognition output.
		opts.Types = false
		nodeRef, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedLRBodies = false
		node, err := Generate(g, opts)
		if err != nil || !bytes.Equal(nodeRef, node) {
			t.Fatalf("%s: typed control changes Node code: %v", c.name, err)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, typedLRBodyCases, GenOptions{disableTypedLRBodies: true})
	testGeneratedTypesCorpus(t, goBin, typedLRBodyCases, GenOptions{convertTypes: true})
}
