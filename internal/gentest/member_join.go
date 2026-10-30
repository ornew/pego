package gentest

// JoinMemberRules are the types of the tests of member access on a join.
const JoinMemberRules = `type A = { F string; X int }
type B = { F int }
type C = { F string; In A }
type D = { In B }
type N = node[string]
type U ~ A | B
type S ~ A | C
type V ~ C | D
rule a = "a" => A{F: "a", X: 1}
rule b = "b" => B{F: 2}
rule c = "c" => C{F: "c", In: A{F: "ca", X: 3}}
rule d = "d" => D{In: B{F: 4}}
rule n N = [0-9]+ #atomic
rule w = [a-z]+ #atomic
rule u U = a / b
rule s S = a / c
rule v V = c / d
`

// JoinMemberCases returns the grammars and inputs of the tests of member access on a join, which every backend runs.
func JoinMemberCases() []Case {
	return []Case{
		{Name: "same type", Src: JoinMemberRules + "rule main = x:s $$ => $x.F\n", Inputs: []string{"a", "c", "x"}},
		{Name: "different types", Src: JoinMemberRules + "rule main = x:u $$ => $x.F\n", Inputs: []string{"a", "b", "x"}},
		{Name: "inline choice", Src: JoinMemberRules + "rule main = x:(a / b / c) $$ => $x.F\n", Inputs: []string{"a", "b", "c"}},
		{Name: "nested", Src: JoinMemberRules + "rule main = x:v $$ => $x.In.F\n", Inputs: []string{"c", "d", "x"}},
		{Name: "node members", Src: JoinMemberRules + "rule main = x:(n / w) $$ => $x.Start + $x.End\n", Inputs: []string{"12", "ab", ""}},
		{Name: "node and struct", Src: JoinMemberRules + "rule main = x:(n / a) $$ => $x.(N).Start\n", Inputs: []string{"12", "a"}},
		{Name: "predicate", Src: JoinMemberRules + "rule main = x:u ({$x.F == $x.F} \"!\" / \"?\") $$\n", Inputs: []string{"a!", "b?", "a?"}},
		{Name: "constructor", Src: JoinMemberRules + "type R = { V string | int }\nrule main = x:u $$ => R{V: $x.F}\n", Inputs: []string{"a", "b"}},
		{Name: "lambda", Src: JoinMemberRules + "rule main = xs:u+ $$ => map($xs.Value, (i) => i.F)\n", Inputs: []string{"ab", "ba", "a"}},
	}
}

// TypedJoinMemberCases read a scalar field every member of the join declares with the same type
// and every constructor sets, which the typed runtime can read back.
func TypedJoinMemberCases() []Case {
	rules := "type A = { F string; X int }\ntype C = { F string }\ntype S ~ A | C\n" +
		"rule a = \"a\" => A{F: \"a\", X: 1}\nrule c = \"c\" => C{F: \"c\"}\nrule s S = a / c\n"
	return []Case{
		{Name: "typed same field", Src: rules + "rule main = x:s $$ => $x.F\n", Inputs: []string{"a", "c", "x"}},
		{Name: "typed constructor", Src: rules + "type R = { V string }\nrule main = x:s $$ => R{V: $x.F}\n", Inputs: []string{"a", "c"}},
		{Name: "typed list", Src: rules + "rule main = xs:s+ $$ => map($xs.Value, (i) => i.F)\n", Inputs: []string{"ac", "ca"}},
		{Name: "typed predicate", Src: rules + "rule main = x:s ({$x.F == \"a\"} \"!\" / \"?\") $$\n", Inputs: []string{"a!", "c?", "a?"}},
	}
}
