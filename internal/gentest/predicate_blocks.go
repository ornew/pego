package gentest

// PredicateBlockCase is a grammar written with predicate blocks (Block), the same grammar written with
// adjacent blocks (Adjacent), and the inputs to run both on.
type PredicateBlockCase struct {
	Name, Block, Adjacent string
	Inputs                []string
}

// PredicateBlockCases returns the predicate block cases.
func PredicateBlockCases() []PredicateBlockCase {
	return []PredicateBlockCase{
		{"whole body", `rule main = {true; true}`, `rule main = {true} {true}`, []string{"", "a"}},
		{"captured block", `var n int
rule main = x:{n := 1; n == 1} "a" $$ => $x`, `var n int
rule main = x:({n := 1} {n == 1}) "a" $$ => $x`, []string{"a", "", "aa"}},
		{"choice blocks", `rule main = ({false; true} / {true; true}) $$`, `rule main = (({false} {true}) / ({true} {true})) $$`, []string{"", "a"}},
		{"nullable block repetition", `var n int
rule main = {n := 0} ({n := n + 1; true})* {n == 1} $$`, `var n int
rule main = {n := 0} ({n := n + 1} {true})* {n == 1} $$`, []string{"", "a"}},
		{"bindings", `var x int
var y int
rule main = {x := 1; y := x + 1; y == 2} "é" $$`, `var x int
var y int
rule main = {x := 1} {y := x + 1} {y == 2} "é" $$`, []string{"é", "", "a", "éé"}},
		{"capture", `var s string
rule main = c:"é" {s := text($c); s == "é"} $$ => $c`, `var s string
rule main = c:"é" {s := text($c)} {s == "é"} $$ => $c`, []string{"é", "a", "", "éx"}},
		{"positional values", `var x int
rule main = v1:"a" {x := 1; x == 1} v2:"b" $$ => list($v1, $v2)`, `var x int
rule main = v1:"a" {x := 1} {x == 1} v2:"b" $$ => list($v1, $v2)`, []string{"ab", "a", "ac", ""}},
		{"choice rollback", `var x int
rule main = {x := 0} ("a" {x := 1; x == 1} "!" / "a" {x == 0; true} "?") $$`, `var x int
rule main = {x := 0} ("a" {x := 1} {x == 1} "!" / "a" {x == 0} {true} "?") $$`, []string{"a!", "a?", "a", "b"}},
		{"optional rollback", `var x int
rule main = {x := 0} ("a" {x := 1; false})? {x == 0; true} "a" $$`, `var x int
rule main = {x := 0} ("a" {x := 1} {false})? {x == 0} {true} "a" $$`, []string{"a", "", "aa"}},
		{"predicate failure", `rule main = "a" {1 / 0; true} "b" $$`, `rule main = "a" {1 / 0} {true} "b" $$`, []string{"ab", "a", ""}},
		{"short circuit", `rule main = "a" {false; 1 / 0} "b" $$`, `rule main = "a" {false} {1 / 0} "b" $$`, []string{"ab", "a", ""}},
		{"rule environment", `var x int
rule main = {x := 1; true} (n "!" / n "?") $$
rule n = {x == 1; true} "é"`, `var x int
rule main = {x := 1} {true} (n "!" / n "?") $$
rule n = {x == 1} {true} "é"`, []string{"é!", "é?", "é", "a"}},
		{"repetition", `var x int
rule main = {x := 0} ("a" {x := x + 1; true})* {x == 2; true} $$`, `var x int
rule main = {x := 0} ("a" {x := x + 1} {true})* {x == 2} {true} $$`, []string{"aa", "a", "aaa", ""}},
		{"lookahead", `var x int
rule main = {x := 0} &("a" {x := 1; true}) {x == 0; true} "a" $$`, `var x int
rule main = {x := 0} &("a" {x := 1} {true}) {x == 0} {true} "a" $$`, []string{"a", "", "b"}},
		{"cut", `var x int
rule main = "a" #cut {x := 1; x == 1} "!" $$ / "a" "?" $$`, `var x int
rule main = "a" #cut {x := 1} {x == 1} "!" $$ / "a" "?" $$`, []string{"a!", "a?", "a", ""}},
		{"recovery", `var x int
rule main = ("a" {x := 1; false} ";") #recover(skip="?") $$`, `var x int
rule main = ("a" {x := 1} {false} ";") #recover(skip="?") $$`, []string{"?", "a?", "a;", ""}},
	}
}

// PredicateBlockGeneratedCases returns the predicate block cases as generator cases.
func PredicateBlockGeneratedCases() []Case {
	var cases []Case
	for _, fixture := range PredicateBlockCases() {
		cases = append(cases, Case{Name: fixture.Name, Src: fixture.Block, Inputs: fixture.Inputs})
	}
	return cases
}
