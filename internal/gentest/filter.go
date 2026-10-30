package gentest

// FilterIdentitySource is the grammar of the filter tests that check the identity of the nodes a filter keeps.
const FilterIdentitySource = `type Out = { First node[string]; Kept []node[string]; Empty []node[string] }
rule main = first:"a" second:"b" $$ => Out{First: $first, Kept: filter(list($first, $second, $first), (x) => text(x) == "a"), Empty: filter(nil, (x) => true)}`

// FilterCases returns the grammars and inputs of the filter tests, which every backend runs.
func FilterCases() []Case {
	return []Case{
		{Name: "identity and nil input", Src: FilterIdentitySource, Inputs: []string{"ab", "a", "ac", ""}},
		{Name: "nil elements", Src: `type Out = { Items []*node[string] }
rule main = x:"é" $$ => Out{Items: filter(list(nil, $x, nil, $x), (item) => item == nil || text(item) == "é")}`, Inputs: []string{"é", "a", ""}},
		{Name: "remove all", Src: `rule main = v1:"é" $$ => filter(list($v1), (item) => false)`, Inputs: []string{"é", "", "a"}},
		{Name: "keep order", Src: `rule main = x:[a-z]* #atomic $$ => filter(list($x, nil, $x), (item) => item != nil)`, Inputs: []string{"ab", "", "é"}},
		{Name: "nested lambda", Src: `rule main = v1:"a" $$ => filter(list($v1), (item) => len(filter(list(item, nil), (inner) => inner != nil)) == 1)`, Inputs: []string{"a", "", "ab"}},
		{Name: "predicate error rollback", Src: `rule main = a:"a" ({len(filter(list($a), (item) => 1 / 0 == 0)) == 1} "!" / "?") $$`, Inputs: []string{"a?", "a!", "a", ""}},
		{Name: "value error", Src: `rule main = v1:"a" $$ => filter(list($v1), (item) => 1 / 0 == 0)`, Inputs: []string{"a", "", "ab"}},
		{Name: "dynamic nonbool", Src: `rule main = v1:"a" d:one $$ => filter(list($v1), (item) => $d.(bool))
rule one _ = _ => 1`, Inputs: []string{"a", "", "ab"}},
		{Name: "dynamic nonbool predicate", Src: `rule main = a:"a" d:one ({len(filter(list($a), (item) => $d.(bool))) == 1} "!" / "?") $$
rule one _ = _ => 1`, Inputs: []string{"a?", "a!", "a", ""}},
	}
}
