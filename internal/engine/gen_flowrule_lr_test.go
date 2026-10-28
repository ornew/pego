package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

var typedLRBodyCases = []genCase{
	{"typed/LR_direct", `type Number = { Text string }
type Add = { Left Expr; Right Number }
type Expr ~ Number | Add
rule main Expr = e:expr $$ => $e
rule expr Expr = add / number
rule add Expr = l:expr "+" r:number => Add{Left: $l, Right: $r}
rule number Number = t:([0-9]+) #atomic => Number{Text: text($t)}
`, []string{"1", "1+2+3", "1+", "1++2", "", "1x"}},
	{"typed/LR_indirect", `type Doc = { Text node[string] }
rule main Doc = x:a #atomic $$ => Doc{Text: $x}
rule a = b "x" / "y"
rule b = a "z" / c
rule c = a "w" / "v"
`, []string{"y", "vx", "yzxwx", "yz", "", "v"}},
	{"typed/LR_hidden", `type Doc = { Text node[string] }
rule main Doc = x:expr #atomic $$ => Doc{Text: $x}
rule expr = "" expr "x" / "é"
`, []string{"é", "éxx", "éx?", "", "x"}},
	{"typed/LR_nullable_seed", `type Doc = { N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = step / seed
rule step Doc = p:(expr #atomic) "a" => Doc{N: len(text($p)) + 1}
rule seed Doc = "" => Doc{N: 0}
`, []string{"", "a", "aaa", "a?", "b"}},
	{"typed/LR_no_improvement", `type Doc = { N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = step / seed
rule step Doc = p:(expr #atomic) => Doc{N: len(text($p)) + 1}
rule seed Doc = "a" => Doc{N: 0}
`, []string{"a", "", "aa", "b"}},
	{"typed/LR_projected_repeat", `type Doc = { Items []node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = p:expr ";" items:(x:[a-z] #atomic -",") #repeat(1, 3) / items:(x:[a-z] #atomic -",") #repeat(1, 3) => Doc{Items: map($items, (e) => e.$x)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a,b,c,d,", "a,;c,?", ""}},
	{"typed/LR_element_frames", `type Doc = { N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = p:expr ";" items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(1, 3) / items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(1, 3) => Doc{N: len($items)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a,b,c,d,", "a,;c,?", ""}},
	{"typed/LR_rollback_environment", `type Doc = { Text node[string]; N int }
var n int
rule main Doc = e:expr $$ => $e
rule expr Doc = (p:expr "+" {n := 1} x:[a-z] #atomic "!" / x:"é" #atomic {n := 2}) => Doc{Text: $x, N: n}
`, []string{"é", "é+a!", "é+a!+b!", "é+a", "é+a!+", "", "a"}},
	{"typed/LR_lookahead_memo", `type Doc = { Text node[string] }
rule main Doc = &(expr "!") e:expr "!" $$ => $e
rule expr Doc = p:expr "+" &x:[a-z] #atomic x:[a-z] #atomic / x:[a-z] #atomic => Doc{Text: $x}
`, []string{"a!", "a+b+c!", "a+!", "a+b?", "", "é!"}},
	{"typed/LR_unreachable_capture", `type Doc = { Text *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = p:expr "x" x:"" #atomic / x:"a" #atomic / _|_ ignored:"b" #atomic => Doc{Text: $x}
`, []string{"a", "axx", "b", "", "a?"}},
	{"typed/LR_positional_action", `type Doc = { Text string }
rule main Doc = e:expr $$ => $e
rule expr Doc = v1:(expr "+" "é" #atomic / "é" #atomic) => Doc{Text: text($v1)}
`, []string{"é", "é+é+é", "é+", "é+é?", "", "a"}},
	{"typed/LR_framed_cuts", `type Doc = { Text node[string] }
rule main Doc = x:expr #atomic $$ => Doc{Text: $x}
rule expr = expr "+" #cut "a" / "a"
`, []string{"a", "a+a+a", "a+", "a+a?", "", "b"}},
	{"typed/LR_recovery", `type Doc = { Text node[string] }
rule main Doc = x:expr #atomic $$ => Doc{Text: $x}
rule expr = (expr "+" "a" / "a") #recover(skip=";")
`, []string{"a", "a+a+a", "a+", ";", "a+a?", "", "b"}},
}

func TestGeneratedTypedLRBodies(t *testing.T) { eachMode(t, testGeneratedTypedLRBodies) }

func testGeneratedTypedLRBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range typedLRBodyCases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := gen.GenOptions{Package: "lrbody", Start: "main", Types: true, Recognize: true}
		code, err := generateCode(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(string(code), "TParse(trules") {
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
		for _, r := range compile(t, c.src).rules {
			if r.leader && strings.Contains(string(code), "// "+r.name+", invoked as by invoke (body inlined)") {
				t.Fatalf("%s: leader %s must keep the growth wrapper", c.name, r.name)
			}
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, typedLRBodyCases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, typedLRBodyCases, gen.GenOptions{ConvertTypes: true})
}
