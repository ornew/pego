package gentest

// node[T] takes any value type T, and tuple types (A, B) are value types: they are checked
// statically, and at run time where a value may not fit, the same way on every backend and in
// generated parsers.
func NodeTypeCases() []Case {
	return []Case{
		{Name: "tuple node field", Src: "type P = { V node[(node[string], node[string])] }\nrule main P = v:(\"a\" \"b\") $$ => P{V: $v}", Inputs: []string{"ab", "a"}},
		{Name: "list node field", Src: "type P = { V node[[]node[string]] }\nrule main P = v:[a-z]* $$ => P{V: $v}", Inputs: []string{"ab", "", "1"}},
		{Name: "nested node field", Src: "type P = { V node[(node[string], node[(node[string], node[string])])] }\nrule main P = v:(\"a\" bc) $$ => P{V: $v}\nrule bc = \"b\" \"c\"", Inputs: []string{"abc", "ab"}},
		{Name: "tuple value field", Src: "type P = { T (node[string], node[string]) }\nrule main P = v:(\"a\" \"b\") $$ => P{T: $v.Value}", Inputs: []string{"ab"}},
		{Name: "optional tuple field", Src: "type P = { T *(node[string], node[string]) }\nrule main P = ab / c\nrule ab P = v:(\"a\" \"b\") $$ => P{T: $v.Value}\nrule c P = \"c\" $$ => P{T: nil}", Inputs: []string{"ab", "c", "a"}},
		{Name: "optional tuple list field", Src: "type P = { T []*(node[string], node[string]) }\nrule main P = v:(\"a\" \"b\") $$ => P{T: []*(node[string], node[string]){$v.Value, nil}}", Inputs: []string{"ab", "a"}},
		{Name: "union field", Src: "type P = { V node[(node[string], node[string])] | node[[]node[string]] }\nrule main P = v:(\"a\" \"b\" / \"c\"*) $$ => P{V: $v}", Inputs: []string{"ab", "cc", ""}},
		{Name: "rule type", Src: "rule main node[(node[string], node[[]node[string]])] = \"a\" \"b\"* $$", Inputs: []string{"abb", "a", "b"}},
		{Name: "cast tuple node", Src: "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).(node[(node[string], node[string])])", Inputs: []string{"ab", "cc", ""}},
		{Name: "cast list node", Src: "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).(node[[]node[string]])", Inputs: []string{"ab", "cc", ""}},
		{Name: "node of int", Src: "type P = { V *node[int] }\nrule main P = \"a\" $$ => P{V: nil}", Inputs: []string{"a"}},
	}
}
