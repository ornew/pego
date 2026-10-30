package gentest

// CastRules is the grammar of the cast tests.
const CastRules = "type Num = { Text string }\ntype P = { N Num }\nrule n Num = t:([0-9]+) #atomic => Num{Text: text($t)}\nrule w = [a-z]+\nrule pair = n:n \"=\" => P{N: $n}\n"

// CastCases returns the grammars and inputs of the cast tests, which every backend runs.
func CastCases() []Case {
	return []Case{
		{Name: "terminal", Src: "rule main = x:(n / w) $$ => $x.(Num)\n" + CastRules, Inputs: []string{"12", "ab", ""}},
		{Name: "list", Src: "rule main = x:(n+ / w) $$ => $x.([]Num)\n" + CastRules, Inputs: []string{"1", "ab", ""}},
		{Name: "optional", Src: "rule main = x:(n / w)? $$ => $x.(*Num)\n" + CastRules, Inputs: []string{"1", "", "ab"}},
		{Name: "union", Src: "rule main = x:(n / w / pair) $$ => $x.(Num | P)\n" + CastRules, Inputs: []string{"1", "1=", "ab"}},
		{Name: "field", Src: "rule main = x:(pair / w) $$ => $x.(P).N\n" + CastRules, Inputs: []string{"1=", "ab"}},
		{Name: "predicate", Src: "rule main = x:(n / w) ({$x.(Num) == $x} \"!\" / \"?\") $$\n" + CastRules, Inputs: []string{"1!", "a?", "a!", "1?"}},
		{Name: "constructor", Src: "rule main = x:(n / w) $$ => P{N: $x.(Num)}\n" + CastRules, Inputs: []string{"1", "a"}},
		{Name: "top", Src: "rule main = x:(n / w) $$ => $x.(_)\n" + CastRules, Inputs: []string{"1", "a"}},
		// The error names where the action started, after a newline, in code points and bytes.
		{Name: "position", Src: "rule main = \"\\u00e9\\n\" r\nrule r = x:(n / w) $$ => $x.(Num)\n" + CastRules, Inputs: []string{"\u00e9\n1", "\u00e9\nab"}},
		{Name: "int", Src: "rule main = x:w $$ {len(text($x)).(int) > 1} => $x\n" + CastRules, Inputs: []string{"ab", "a"}},
	}
}
