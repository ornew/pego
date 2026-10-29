package gentest

var TypedLocalLayoutCases = []Case{
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
