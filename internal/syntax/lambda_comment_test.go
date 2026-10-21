package syntax

import "testing"

// A comment between "=>" and the body of a lambda leads the body. The body
// goes on a line of its own, one level in from the parameters, and a line
// ends without a space.
func TestLambdaBodyAfterComment(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"line comment", "rule r = \"a\" => ((b) => // c\n 1)\n", "rule r = \"a\" => (\n    (b) => // c\n        1\n)\n"},
		{"line comment on its own line", "rule r = \"a\" => ((b) =>\n // c\n 1)\n", "rule r = \"a\" => (\n    (b) =>\n        // c\n        1\n)\n"},
		{"block comment ending the line", "rule r = \"a\" => ((b) => /* c */\n 1)\n", "rule r = \"a\" => (\n    (b) => /* c */\n        1\n)\n"},
		{"block comment before the body", "rule r = \"a\" => ((b) => /* c */ 1)\n", "rule r = \"a\" => ((b) => /* c */ 1)\n"},
		{"argument of a call", "rule r = \"a\" => map(x, (b) => // c\n 1)\n", "rule r = \"a\" => map(\n    x,\n    (b) => // c\n        1,\n)\n"},
		{"body over lines", "rule r = \"a\" => ((b) => // c\n f(1,\n 2,))\n", "rule r = \"a\" => (\n    (b) => // c\n        f(\n            1,\n            2,\n        )\n)\n"},
		{"binary body", "rule r = \"a\" => ((b) => // c\n b + 1)\n", "rule r = \"a\" => (\n    (b) => // c\n        b + 1\n)\n"},
		{"binary body over lines", "rule r = \"a\" => ((b) => // c\n b +\n 1)\n", "rule r = \"a\" => (\n    (b) => // c\n        b + 1\n)\n"},
		{"index body", "rule r = \"a\" => ((b) => // c\n x[0])\n", "rule r = \"a\" => (\n    (b) => // c\n        x[0]\n)\n"},
		{"call index body", "rule r = \"a\" => ((b) => // c\n f(1)[0])\n", "rule r = \"a\" => (\n    (b) => // c\n        f(1)[0]\n)\n"},
		{"double index body", "rule r = \"a\" => ((b) => // c\n x[0][1])\n", "rule r = \"a\" => (\n    (b) => // c\n        x[0][1]\n)\n"},
		{"index body with a trailing block", "rule r = \"a\" => ((b) => // c\n x[0] /* z */)\n", "rule r = \"a\" => (\n    (b) => // c\n        x[0] /* z */\n)\n"},
		{"index binary body", "rule r = \"a\" => ((b) => // c\n x[0] + 1)\n", "rule r = \"a\" => (\n    (b) => // c\n        x[0] + 1\n)\n"},
		{"call body after a block comment", "rule r = \"a\" => ((b) => /* c */ f(1,\n 2,))\n", "rule r = \"a\" => (\n    (b) => /* c */ f(\n        1,\n        2,\n    )\n)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
