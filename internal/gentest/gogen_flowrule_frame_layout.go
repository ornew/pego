package gentest

var TypedFrameLayoutCases = []Case{
	{"typed/LR_dead_capture_order", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = _|_ unused:"z" #atomic / p:expr ";" x:"é" #atomic / x:"a" #atomic / _|_ ignored:"b" #atomic => Doc{Text: $x, Missing: $ignored}
`, []string{"a", "a;é;é", "a;", "b", "z", "", "a?"}},
	{"typed/OperatorsRule_dead_capture_order", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand _|_ before:"b" #atomic / x:"é" #atomic / _|_ after:"z" #atomic => Doc{Text: $x, Missing: $after}
}
`, []string{"é", "b", "z", "", "é?"}},
	{"typed/OperatorsRule_infallible_alternative", `type Doc = { Missing *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand _ / ignored:"b" #atomic => Doc{Missing: $ignored}
}
`, []string{"", "b", "é"}},
	{"typed/OperatorsRule_dead_element_predicate", `type Atom = { Text string }
type Doc = { Items []*node[string]; N int }
type Expr ~ Atom | Doc
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand atom
    level {
        postfix "[" items:((_|_ ignored:"z" #atomic / x:[a-z] #atomic) {len($x) == 1 && $ignored == nil} -",") #repeat(3) "]" => Doc{Items: map($items, (e) => e.$x), N: len($items)}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, []string{"a", "a[b,c,d,]", "a[b,c,]", "a[b,é,d,]", "a[b,c,d,", "a[b,c,d,]?", ""}},
	{"typed/OperatorsRule_dead_projection", `type Doc = { Items []*node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand _|_ ignored:"z" #atomic / items:((_|_ unused:"z" #atomic / x:[a-z] #atomic) {$unused == nil} -",") #repeat(3) => Doc{Items: map($items, (e) => e.$x)}
}
`, []string{"a,b,c,", "a,b,", "a,b,é,", "z", "", "a,b,c,?"}},
	{"typed/OperatorsRule_dead_lookahead_capture", `type Doc = { Text *node[string]; Missing *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand &(_|_ before:"z" #atomic / x:"é" #atomic) x:"é" #atomic / _|_ after:"z" #atomic => Doc{Text: $x, Missing: $after}
}
`, []string{"é", "z", "", "é?"}},
}
