package engine

import (
	"strings"
	"testing"
)

// tupleLiteralCases are grammars of tuple literals with their inputs. They join the corpus that
// every backend and generated parser is compared on.
var tupleLiteralCases = []genCase{
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

func TestTupleLiteralErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"too few elements", `rule main (node[string], node[string]) = a:"a" => ($a,)`, "not assignable"},
		{"too many elements", `rule main (node[string], node[string]) = a:"a" => ($a, $a, $a)`, "not assignable"},
		{"wrong element type", `rule main (int, string) = a:"a" => ($a, 1)`, "not assignable"},
		{"not a tuple", `rule main (int, int) = "a" => 1`, "not assignable"},
		{"field arity", `type R = { P (int, int) }
rule main R = "a" => R{P: (1, 2, 3)}`, "cannot use"},
		{"field element type", `type R = { P (int, string) }
rule main R = "a" => R{P: (1, 2)}`, "cannot use"},
		{"function element", `rule main = "a" => ((x) => x, 1)`, "functions can only be passed"},
		{"unit for pair", `rule main (int, int) = "a" => ()`, "not assignable"},
		{"undefined capture in element", `rule main = "a" => (1, $x)`, "$x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want it to contain %q", got, tc.want)
			}
		})
	}
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
