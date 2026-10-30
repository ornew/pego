package gentest

// ValueResultCases have rules and list elements whose values are ints, strings and bools.
func ValueResultCases() []Case {
	return []Case{
		{Name: "int rule", Src: "rule main = d:[0-9]+ #atomic $$ => len($d)", Inputs: []string{"123", "7"}},
		{Name: "bool rule", Src: "rule main bool = d:[0-9]* #atomic $$ => len($d) > 1", Inputs: []string{"123", "1"}},
		{Name: "map text", Src: "rule main = xs:([a-z] #atomic)+ $$ => map($xs.Value, (x) => text(x))", Inputs: []string{"abc", "a"}},
		{Name: "list scalars", Src: "rule main = \"a\" $$ => list(1, \"s\", true, nil)", Inputs: []string{"a"}},
		{Name: "typed list", Src: "rule main = \"a\" $$ => []int{1, 2}", Inputs: []string{"a"}},
		{Name: "int elements", Src: "rule n int = d:[0-9]+ #atomic => len($d)\nrule main = xs:(n -\",\"?)+ $$ => map($xs.Value, (x) => x[0] + 1)", Inputs: []string{"1,22,333"}},
		{Name: "string field", Src: "type T = { A []string }\nrule main = xs:([a-z] #atomic)+ $$ => T{A: map($xs.Value, (x) => text(x))}", Inputs: []string{"ab"}},
		{Name: "fold ints", Src: "rule main = xs:([0-9]+ #atomic -\" \"?)+ $$ => foldl(0, map($xs.Value, (x) => len(x[0])), (a, b) => a + b)", Inputs: []string{"1 22 333"}},
		{Name: "filter and index", Src: "rule main = \"a\" $$ => filter(list(1, 2, 3), (x) => x > 1)[1]", Inputs: []string{"a"}},
		{Name: "concat", Src: "rule main = \"a\" $$ => concat(list(\"x\"), []string{\"y\"}, map(list(1, 2), (x) => x * 2))", Inputs: []string{"a"}},
	}
}
