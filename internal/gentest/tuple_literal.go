package gentest

// TupleLiteralCases are grammars of tuple literals with their inputs. They join the corpus that
// every backend and generated parser is compared on.
var TupleLiteralCases = []Case{
	{"tuple literal of declared tuple type", `rule main (node[string], node[string]) = a:"a" b:"b" => ($a, $b,)`,
		[]string{"ab", "a", "", "abb"}},
	{"tuple literal in struct fields", `type R = { P (int, string); Q (node[string], (int, bool)); U () }
rule main R = a:"a" => R{P: (1, "s"), Q: ($a, (2, true)), U: ()}`, []string{"a", "", "b"}},
	{"tuple literal values", `rule main = -"i" i / -"o" o / -"u" u / -"m" m / -"l" l / -"n" n / -"e" e
rule i = a:"a" b:"b" => ($a, $b)[1]
rule o = a:"a" => ($a,)
rule u = "" => ()
rule m = a:"a" => (1, "s", true, nil, ($a, ()))
rule l = a:"a" => list(($a, 1), (2, $a))
rule n = xs:[a-z]+ => (len($xs), map($xs, (x) => (x, 1)))
rule e = a:"a" => (1 / 0, $a)
`,
		[]string{"iab", "ia", "oa", "u", "ma", "la", "nabc", "n", "ea", ""}},
}
