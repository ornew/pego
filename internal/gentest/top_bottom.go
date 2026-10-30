package gentest

// TopBottomRules is the grammar of the top and bottom tests.
const TopBottomRules = "type Num = { Text string }\nrule n Num = t:([0-9]+) #atomic => Num{Text: text($t)}\nrule w = [a-z]+\n"

// TopBottomCases returns the grammars and inputs of the top and bottom tests, which every backend runs.
func TopBottomCases() []Case {
	return []Case{
		{Name: "top field", Src: "type T = { A _ }\nrule main = x:(n / w) $$ => T{A: $x}\n" + TopBottomRules, Inputs: []string{"1", "a"}},
		// text applies to nodes of string values, so both alternatives are plain nodes here.
		{Name: "top primitive", Src: "type T = { A _ }\nrule main = x:(n / w) $$ => T{A: len(text($x))}\nrule n = [0-9]+ #atomic\nrule w = [a-z]+\n", Inputs: []string{"12", "a"}},
		{Name: "top cast", Src: "type T = { A _ }\nrule main = x:(n / w) $$ => T{A: $x}.A.(Num)\n" + TopBottomRules, Inputs: []string{"1", "a"}},
		{Name: "top compare", Src: "type T = { A _ }\nrule main = x:(n / w) {T{A: $x}.A == $x} $$\n" + TopBottomRules, Inputs: []string{"1", "a"}},
		// A struct is not a node, so the meet is node[string].
		{Name: "meet field", Src: "type T = { A (Num | node[string] | int) & node[_] }\nrule main = x:([a-z]+ #atomic) $$ => T{A: $x}\n" + TopBottomRules, Inputs: []string{"a", "1"}},
		{Name: "top list", Src: "type T = { A []_ }\ntype U = { F _ }\nrule main = x:(n / w)+ $$ => T{A: U{F: $x}.F.([]_)}\n" + TopBottomRules, Inputs: []string{"1", "1a"}},
		{Name: "top list mismatch", Src: "type T = { A []_ | int }\ntype U = { F _ }\nrule main = x:(n / w) $$ => T{A: U{F: $x}.F.([]_ | int)}\n" + TopBottomRules, Inputs: []string{"1", "a"}},
		{Name: "meet list", Src: "type T = { A [](Num | node[string]) & []node[_] }\nrule main = xs:[a-z]+ $$ => T{A: $xs.Value}\n" + TopBottomRules, Inputs: []string{"ab", ""}},
	}
}
