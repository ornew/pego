package compile

import (
	"strings"
	"testing"
)

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
