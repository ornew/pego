package gentest

const ConstructorGrammar = `type Int = { Text string }
type Alias ~ Int
type Values = { Items []Int; Nested [][]Int; Maybe []*Int; Default Int; Empty []Int }
rule main Values = v1:"😀" => Values{Items: []Int{Int{Text: "0"}, Alias{Text: text($v1)}}, Nested: [][]Int{[]Int{}, []Int{Int{Text: "1"}}}, Maybe: []*Int{nil, Int{Text: "2"}}, Default: Int{Text: "0"}, Empty: []Int{}}
`

func ConstructorCases() []Case {
	return []Case{
		{"typed node constructors", ConstructorGrammar, []string{"😀", "", "x"}},
		{"named node types", NodeTypeGrammar, []string{"", "ab", "ab 12 c3", "<1,x>", "ab<12,y>[1]!bc", "12 ?", "[]!a", "ab 12<1,x>[3]"}},
		{"named node type run-time conversions", `type S = node[string]
type L = node[[]S]
type Out = { A S; B L }
rule main Out = x:anything xs:more* $$ => Out{A: S{$x.(node[string])}, B: L{$xs}}
rule anything node[_] = "a" / "b" "c"
rule more S = -"," x:[a-z] => $x`, []string{"a", "a,x,y", "bc", "bc,x", "", "a,"}},
		{"terminal constructor root", `type Int = { Text string }
type Alias ~ Int
rule main Alias = "é" => Alias{Text: "0"}`, []string{"é", "", "e"}},
		{"typed list root", `type T = { Text string }
rule main []T = "x" => []T{T{Text: "0"}}`, []string{"x", "", "xx"}},
		{"typed nullable union list", `type T = { Text string }
type R = { Values []*(T | node[string]) }
rule main R = x:"é" => R{Values: []*(T | node[string]){nil, T{Text: "0"}, $x}}`, []string{"é", "", "x"}},
		{"terminal constructor value error", `type Int = { Text string }
var value int
rule main = {value := 1} "x" => Int{Text: list(value, "a")[0].(string)}`, []string{"x", ""}},
		{"terminal constructor predicate error", `type Int = { Text string }
var value int
rule main = {value := 1} {Int{Text: list(value, "a")[0].(string)} != nil} "x" / "x"`, []string{"x", ""}},
	}
}
