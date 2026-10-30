package gentest

// IndexCases returns the grammars and inputs of the index tests, which every backend runs.
func IndexCases() []Case {
	return []Case{
		{Name: "first and last", Src: `rule main = xs:item+ $$ => list($xs[0], $xs[len($xs) - 1])
rule item = [a-z]`, Inputs: []string{"abc", "a", "", "1"}},
		{Name: "out of range", Src: `rule main = xs:item* $$ => $xs[2]
rule item = [a-z]`, Inputs: []string{"abc", "ab", ""}},
		{Name: "negative", Src: `rule main = xs:item* $$ => $xs[0 - 1]
rule item = [a-z]`, Inputs: []string{"a", ""}},
		{Name: "nil list", Src: `rule main = xs:(item+)? $$ => list($xs)[0]
rule item = [a-z]`, Inputs: []string{"a", ""}},
		{Name: "nested", Src: `rule main = xs:item+ $$ => list($xs, list($xs[0]))[1][0]
rule item = [a-z]`, Inputs: []string{"ab", ""}},
		{Name: "lambda parameter", Src: `rule main = xs:item+ $$ => map(list($xs), (l) => l[0])
rule item = [a-z]`, Inputs: []string{"ab", ""}},
		{Name: "predicate", Src: `rule main = xs:item+ ({text($xs[0]) == "a"} "!" / "?") $$
rule item = [a-z]`, Inputs: []string{"ab!", "ab?", "ba!", "ba?", ""}},
		{Name: "predicate error rollback", Src: `rule main = xs:item+ ({$xs[5] == nil} "!" / "?") $$
rule item = [a-z]`, Inputs: []string{"ab!", "ab?", ""}},
		{Name: "dynamic index", Src: `var i int
rule main = {i := 1} xs:item+ $$ => $xs[i]
rule item = [a-z]`, Inputs: []string{"ab", "a", ""}},
	}
}
