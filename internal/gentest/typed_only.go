package gentest

// OrdinaryOnlyConverted needs the Node result of its rules: an action reads a field of a struct, so the
// typed entry points convert the Node that the rule functions build instead of building the values
// directly.
const OrdinaryOnlyConverted = `type Item = { Names []string; N int }
rule main Item = i:item "!" => Item{Names: $i.Names, N: len($i.Names)}
rule item Item = n:[a-z]+ => Item{Names: list(text($n)), N: 0}`

var OrdinaryOnlyConvertedInputs = []string{"abc!", "!", "abc", "", "é!", "ab\xff!", "abc!x", "a!b!", "ab !"}

// TypedOnlyConverted are grammars whose typed entry points convert Nodes (an action reads a field of a struct), so
// that a typed-only parser keeps the Node rules and its entry points run them: a plain sequence, recovery, a
// fold, a left-recursive leader, a cut with an environment and an Operators Rule.
var TypedOnlyConverted = []Case{
	{Name: "converted sequence", Src: OrdinaryOnlyConverted, Inputs: OrdinaryOnlyConvertedInputs},
	{Name: "recovery", Src: `type Item = { Name node[string] }
type Box = { Items node[_]; F Item | error; FT string }
type Out = { Box Box; Start int; End int; Text string; FNil bool }
rule main Out = b:box => Out{Box: $b, Start: $b.Items.Start, End: $b.Items.End, Text: text($b.Items), FNil: $b.F == nil}
rule box Box = "(" xs:item* ")" fw:(f:fitem "") => Box{Items: $xs, F: $f, FT: text($fw)}
rule item Item = " "* i:item2 => $i
rule item2 Item = n:"x" #atomic => Item{Name: $n}
rule fitem Item | error = yitem #recover(skip=[a-z]+)
rule yitem Item = n:"y" #atomic => Item{Name: $n}`,
		Inputs: []string{"(x x)y", "(xx)y", "( x )q", "(x)", "(x x", "(x x)zzy", "", "(é)y"}},
	{Name: "fold", Src: `type Op = { Left Node; Op node[string]; Right Node }
type Node ~ Op | node[string]
type Args = { Items []node[string]; N int; S string }
rule main Args = e:(expr #atomic) ";" a:args => Args{Items: concat($a.Items, $a.Items), N: len($a.Items) * 2 - 1, S: text($e) + "!"}
rule expr Node = l:num rest:(op:("+" / "-") #atomic r:num)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.$op, Right: i.$r})
rule args Args = first:num rest:(-"," x:num)* => Args{Items: concat(list($first), map($rest, (r) => r.$x)), N: len($rest)}
rule num = [0-9]+ #atomic`,
		Inputs: []string{"1+2-3;4,5,6", "1;2", "1-;2", "", "1+2;", "12+3;4,", ";"}},
	{Name: "left recursion", Src: `type E = { Items []node[string]; N int }
rule main E = e:expr "!" => E{Items: $e.Items, N: len($e.Items)}
rule expr E = (l:expr "," x:item => E{Items: concat($l.Items, list($x)), N: 0}) / (x:item => E{Items: list($x), N: 0})
rule item = [a-z]+ #atomic`,
		Inputs: []string{"a!", "a,b,c!", "a,b", ",a!", "a,,b!", "", "a,b,!", "ab,cd!x"}},
	{Name: "cut and variables", Src: `type R = { Names node[_]; N int }
var n int = 0
rule main R = i:item "!" => R{Names: $i.Names, N: n}
rule item R = ("a" #cut {n := n + 1} xs:name+ => R{Names: $xs, N: 0}) / ("b" {n := n + 2} xs:name* => R{Names: $xs, N: 0})
rule name = [a-z] #atomic ","?`,
		Inputs: []string{"ab,c,!", "b!", "bx!", "a!", "a", "c!", "bxy,z!", ""}},
	{Name: "operators", Src: `type It = { L []string }
rule main It = e:expr $$ => It{L: concat($e.L, list("!"))}
rule expr It = #operators {
    skip " "*
    operand x:num => $x
    level { infix left o:("+" / "-") => It{L: concat($lhs.L, list(text($o)), $rhs.L)} }
    level { infix right o:("*" / "/") => It{L: concat(list("["), $lhs.L, list(text($o)), $rhs.L, list("]"))} }
}
rule num It = d:[0-9]+ #atomic => It{L: list(text($d))}`,
		Inputs: []string{"1", "1 + 2", "1 - 2 * 3", "1 * 2 / 3 + 4", "1 +", "", " 1  *  2", "1 + 2 +"}},
}
