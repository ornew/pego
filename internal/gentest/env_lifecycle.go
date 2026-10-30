package gentest

var EnvBindingCases = []Case{
	{"binding choice rollback", `type V = { X int; Y int }
var x int
var y int
rule main = {x := 1} {y := 2} ("a" {x := 3} {y := 4} "x" / "a" {x == 1} {y == 2} "b") $$ => V{X: x, Y: y}`, []string{"ab", "ax", "a", ""}},
	{"binding rule scope", `type V = { X int; Y int }
var x int
var y int
rule main = {x := 1} {y := 2} inner {x == 1} {y == 2} "b" $$ => V{X: x, Y: y}
rule inner = {x := 3} {y := 4} {x == 3} {y == 4} "a"`, []string{"ab", "ax", ""}},
	{"binding positive lookahead effects", `type V = { X int; Y int }
var x int
var y int
rule main = {x := 1} {y := 2} &({x := 3} {y := 4} "a") {x == 3} {y == 4} "a" $$ => V{X: x, Y: y}`, []string{"a", "aa", ""}},
	{"binding absent after rollback", `type V = { X int }
var x int
var y int
rule main = {x := 1} ({y := 2} "b" / {y := 3} {y == 3} "a") $$ => V{X: x}`, []string{"a", "b", ""}},
	{"binding negative lookahead rollback", `type V = { X int; Y int }
var x int
var y int
rule main = {x := 1} {y := 2} !({x := 3} {y := 4} "b") {x == 1} {y == 2} "a" $$ => V{X: x, Y: y}`, []string{"a", "b", ""}},
}

// BoundStream is the grammar of the test that streams a long input through bound variables.
const BoundStream = `var limit int
var i int
var j int
rule main = {limit := 1} ({i := 1} {j := 2} {i := 3} -"a")* #stream {limit == 1} $$`
