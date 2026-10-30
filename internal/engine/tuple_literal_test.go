package engine

import (
	"testing"
)

func TestTupleLiteralValues(t *testing.T) {
	t.Parallel()
	check(t, `rule main = -"i" i / -"o" o / -"u" u / -"m" m / -"l" l / -"n" n
rule i = a:"a" b:"b" => ($a, $b)[1]
rule o = a:"a" => ($a,)
rule u = "" => ()
rule m = a:"a" => (1, "s", true, nil, ($a, ()))
rule l = a:"a" => list(($a, 1), (2, $a))
rule n = xs:[a-z]+ => (len($xs), map($xs, (x) => (x, 1)))`,
		ok("iab", `("b")@main`),
		ok("oa", `(("a"))@main`),
		ok("u", `(())@main`),
		ok("ma", `((1 "s" true nil ("a" ())))@main`),
		ok("la", `([("a" 1) (2 "a")])@main`),
		ok("nab", `((2 [("a" 1) ("b" 1)]))@main`),
	)
}

func TestTupleLiteralDeclaredType(t *testing.T) {
	t.Parallel()
	check(t, `rule main (node[string], node[string]) = a:"a" b:"b" => ($a, $b)`,
		ok("ab", `("a" "b")`),
	)
	check(t, `type R = { P (int, string) }
rule main R = "a" => R{P: (1, "s")}`,
		ok("a", `R{P: (1 "s")}`),
	)
}

func TestTupleLiteralElementErrors(t *testing.T) {
	t.Parallel()
	check(t, `rule main = -"e" e
rule e = a:"a" => (1 / 0, $a)
`,
		fails("ea", "division by zero"),
	)
}

func TestTupleLiteralTypeInference(t *testing.T) {
	t.Parallel()
	// The inferred type is the tuple of the element types, so it fits a declared tuple elementwise
	// and reads back through an index.
	check(t, `type R = { P (int, (string, bool)) }
rule pair (int, (string, bool)) = "a" => (1, ("s", true))
rule main R = "a" => R{P: (1, ("s", false))}
`, ok("a", `R{P: (1 ("s" false))}`))
}
