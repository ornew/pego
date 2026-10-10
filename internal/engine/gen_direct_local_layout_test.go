package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var typedLocalLayoutCases = []genCase{
	{"typed/local_dead_before_live", `
type Doc struct { Text *Match, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / x:@"é") -> new Doc{Text:$x, Missing:$ignored}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_dead_after_live", `
type Doc struct { Text *Match, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = (x:@"é" / (_|_ ignored:@"z")) -> new Doc{Text:$x, Missing:$ignored}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_infallible", `
type Doc struct { Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = (_ / ignored:@"z") -> new Doc{Missing:$ignored}
`, []string{"", "z", "é"}},
	{"typed/local_dead_predicate", `
type Doc struct { Text Match }
def main: Doc = e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / _) [$ignored==nil] x:@"é" -> new Doc{Text:$x}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_element_predicates", `
type Doc struct { Items []*Match, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / items:(((_|_ unused:@"z") / x:@"é") [$unused==nil && len($x)==1] -","){1,3})
    -> new Doc{Items:map($items,(e)=>$e.x), Missing:$ignored}
`, []string{"é,", "é,é,é,", "é,é,", "é,z,", "é,é,é,é,", "z", "", "é"}},
	{"typed/local_element_dead_predicate", `
type Doc struct { Items []Match }
def main: Doc = e:body $$ -> $e
def body: Doc = items:(((_|_ unused:@"z") / _) [$unused==nil] x:@"é" -","){1,3} -> new Doc{Items:map($items,(e)=>$e.x)}
`, []string{"é,", "é,é,é,", "é,é,", "é,z,", "é,é,é,é,", "", "é"}},
	{"typed/local_element_unused_projection", `
type Doc struct { Items []*Match, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / items:(((_|_ unused:@"z") / x:@"é") -","){1,3})
    -> new Doc{Items:map($items,(e)=>$e.unused), Missing:$ignored}
`, []string{"é,", "é,é,é,", "é,é,", "z", "", "é"}},
	{"typed/local_owned_element_reset", `
type Item struct { Text *Match, Missing *Match }
type Doc struct { Items []Item, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / items:(((_|_ unused:@"z") / x:@"é" / "a") [$unused==nil] -","){1,3})
    -> new Doc{Items:map($items,(e)=>new Item{Text:$e.x, Missing:$e.unused}), Missing:$ignored}
`, []string{"é,a,é,", "a,é,a,", "a,", "é,", "é,é,é,é,", "é,z,", "", "é"}},
	{"typed/local_optional_cut", `
type Doc struct { Text *Match, Missing *Match, N int }
def main: Doc = [n=0] e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / (x:@"é" -- [n=n+1] "!")? "x")
    -> new Doc{Text:$x, Missing:$ignored, N:n}
`, []string{"x", "é!x", "é?x", "éx", "é!", "é", "", "é!xx"}},
	{"typed/local_choice_cut", `
type Doc struct { Text *Match, Missing *Match, N int }
def main: Doc = [n=0] e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / x:@"é" -- [n=n+1] "!" / x:@"é" "?")
    -> new Doc{Text:$x, Missing:$ignored, N:n}
`, []string{"é!", "é?", "é", "z", "", "é!!"}},
	{"typed/local_lookahead", `
type Doc struct { Text Match, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = &((_|_ ignored:@"z") / x:@"é") x:@"é" -> new Doc{Text:$x, Missing:$ignored}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_error", `
type Doc struct { Text *Match, Missing *Match }
def main: Doc = e:body $$ -> $e
def body: Doc = ((_|_ ignored:@"z") / (x:@"é" "!") #error(message="bang"))
    -> new Doc{Text:$x, Missing:$ignored}
`, []string{"é!", "é?", "é", "z", "", "é!!"}},
}

func TestGeneratedTypedLocalLayouts(t *testing.T) {
	for _, c := range typedLocalLayoutCases {
		ast, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatal(err)
		}
		opts := GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true}
		code, err := Generate(ast, opts)
		if err != nil || !strings.Contains(string(code), "tparse(trules") || !localBodyInlined(code) {
			t.Fatalf("%s: typed local body unavailable: %v", c.name, err)
		}
		opts.disableTypedLocalLayouts = true
		reference, err := Generate(ast, opts)
		if err != nil || localBodyInlined(reference) || strings.Contains(string(code), "disableTypedLocalLayouts") {
			t.Fatalf("%s: invalid generation-only control: %v", c.name, err)
		}
		opts.Types = false
		nodeReference, err := Generate(ast, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedLocalLayouts = false
		node, err := Generate(ast, opts)
		if err != nil || !bytes.Equal(nodeReference, node) {
			t.Fatalf("%s: local layout control changes Node output: %v", c.name, err)
		}
	}
	if !testing.Short() {
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		testGeneratedTypesCorpus(t, goBin, typedLocalLayoutCases, GenOptions{disableTypedLocalLayouts: true})
		testGeneratedTypesCorpus(t, goBin, typedLocalLayoutCases, GenOptions{convertTypes: true})
	}
}

func localBodyInlined(code []byte) bool {
	lines := strings.Split(string(code), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "// body, ") && strings.Contains(line, "(body inlined)") &&
			i+1 < len(lines) && strings.HasPrefix(lines[i+1], "func (p *tparser)") {
			return true
		}
	}
	return false
}

func TestGeneratedTypedLocalLayoutControls(t *testing.T) {
	for _, path := range []string{"../../examples/calculator/calc.pego", "../../parsers/typescript/typescript.pego"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ast, err := syntax.Parse(string(src))
		if err != nil {
			t.Fatal(err)
		}
		opts := GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true}
		code, err := Generate(ast, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedLocalLayouts = true
		reference, err := Generate(ast, opts)
		if err != nil || !bytes.Equal(code, reference) {
			t.Fatalf("%s: representative control output changed: %v", path, err)
		}
	}
}
