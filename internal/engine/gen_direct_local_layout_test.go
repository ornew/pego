package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var typedLocalLayoutCases = []genCase{
	{"typed/local_dead_before_live", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = _|_ ignored:"z" #atomic / x:"é" #atomic => Doc{Text: $x, Missing: $ignored}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_dead_after_live", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = x:"é" #atomic / _|_ ignored:"z" #atomic => Doc{Text: $x, Missing: $ignored}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_infallible", `type Doc = { Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = _ / ignored:"z" #atomic => Doc{Missing: $ignored}
`, []string{"", "z", "é"}},
	{"typed/local_dead_predicate", `type Doc = { Text node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = (_|_ ignored:"z" #atomic / _) {$ignored == nil} x:"é" #atomic => Doc{Text: $x}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_element_predicates", `type Doc = { Items []*node[string]; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = (
    _|_ ignored:"z" #atomic / items:((_|_ unused:"z" #atomic / x:"é" #atomic) {$unused == nil && len($x) == 1} -",") #repeat(1, 3)
) => Doc{Items: map($items, (e) => e.$x), Missing: $ignored}
`, []string{"é,", "é,é,é,", "é,é,", "é,z,", "é,é,é,é,", "z", "", "é"}},
	{"typed/local_element_dead_predicate", `type Doc = { Items []node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = items:((_|_ unused:"z" #atomic / _) {$unused == nil} x:"é" #atomic -",") #repeat(1, 3) => Doc{Items: map($items, (e) => e.$x)}
`, []string{"é,", "é,é,é,", "é,é,", "é,z,", "é,é,é,é,", "", "é"}},
	{"typed/local_element_unused_projection", `type Doc = { Items []*node[string]; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = (
    _|_ ignored:"z" #atomic / items:((_|_ unused:"z" #atomic / x:"é" #atomic) -",") #repeat(1, 3)
) => Doc{Items: map($items, (e) => e.$unused), Missing: $ignored}
`, []string{"é,", "é,é,é,", "é,é,", "z", "", "é"}},
	{"typed/local_owned_element_reset", `type Item = { Text *node[string]; Missing *node[string] }
type Doc = { Items []Item; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = (
    _|_ ignored:"z" #atomic / items:((_|_ unused:"z" #atomic / x:"é" #atomic / "a") {$unused == nil} -",") #repeat(1, 3)
) => Doc{Items: map($items, (e) => Item{Text: e.$x, Missing: e.$unused}), Missing: $ignored}
`, []string{"é,a,é,", "a,é,a,", "a,", "é,", "é,é,é,é,", "é,z,", "", "é"}},
	{"typed/local_optional_cut", `type Doc = { Text *node[string]; Missing *node[string]; N int }
var n int
rule main Doc = {n := 0} e:body $$ => $e
rule body Doc = (
    _|_ ignored:"z" #atomic / (x:"é" #atomic #cut {n := n + 1} "!")? "x"
) => Doc{Text: $x, Missing: $ignored, N: n}
`, []string{"x", "é!x", "é?x", "éx", "é!", "é", "", "é!xx"}},
	{"typed/local_choice_cut", `type Doc = { Text *node[string]; Missing *node[string]; N int }
var n int
rule main Doc = {n := 0} e:body $$ => $e
rule body Doc = (
    _|_ ignored:"z" #atomic / x:"é" #atomic #cut {n := n + 1} "!" / x:"é" #atomic "?"
) => Doc{Text: $x, Missing: $ignored, N: n}
`, []string{"é!", "é?", "é", "z", "", "é!!"}},
	{"typed/local_lookahead", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = &(_|_ ignored:"z" #atomic / x:"é" #atomic) x:"é" #atomic => Doc{Text: $x, Missing: $ignored}
`, []string{"é", "z", "", "é?"}},
	{"typed/local_error", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:body $$ => $e
rule body Doc = (
    _|_ ignored:"z" #atomic / (x:"é" #atomic "!") @error(message="bang")
) => Doc{Text: $x, Missing: $ignored}
`, []string{"é!", "é?", "é", "z", "", "é!!"}},
}

func TestGeneratedTypedLocalLayouts(t *testing.T) { eachMode(t, testGeneratedTypedLocalLayouts) }

func testGeneratedTypedLocalLayouts(t *testing.T) {
	for _, c := range typedLocalLayoutCases {
		ast, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		opts := GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true}
		code, err := generateCode(ast, opts)
		if err != nil || !strings.Contains(string(code), "rt.TParse(trules") || !localBodyInlined(code) {
			t.Fatalf("%s: typed local body unavailable: %v", c.name, err)
		}
	}
	if !testing.Short() {
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		testGeneratedTypesCorpus(t, goBin, typedLocalLayoutCases, GenOptions{})
		testGeneratedTypesCorpus(t, goBin, typedLocalLayoutCases, GenOptions{convertTypes: true})
	}
}

func localBodyInlined(code []byte) bool {
	lines := strings.Split(string(code), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "// body, ") && strings.Contains(line, "(body inlined)") &&
			i+1 < len(lines) && strings.HasPrefix(lines[i+1], "func i") && strings.Contains(lines[i+1], "(p *rt.TParser)") {
			return true
		}
	}
	return false
}

func TestGeneratedTypedLocalLayoutControls(t *testing.T) {
	eachMode(t, testGeneratedTypedLocalLayoutControls)
}

func testGeneratedTypedLocalLayoutControls(t *testing.T) {
	for _, path := range []string{"../../examples/calculator/calc.pego", "../../parsers/typescript/typescript.pego"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ast, err := parseTest(string(src))
		if err != nil {
			t.Fatal(err)
		}
		opts := GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true}
		if _, err := generateCode(ast, opts); err != nil {
			t.Fatal(err)
		}
	}
}
