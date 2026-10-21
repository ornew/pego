package syntax

import "testing"

// A block comment over several lines that starts on the line of a node and is
// followed by a line break trails that node, as one over a single line does.
// checkFormat verifies that formatting keeps the owner.
func TestFormatKeepsTrailingBlockOverLines(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{"after a rule", "rule r = \"c\" /* x\n y */\nrule q = \"a\"\n"},
		{"after a field", "type T = {\n    A int /* x\n y */\n    B int\n}\n"},
		{"after the package", "package p /* x\n y */\nrule q = \"a\"\n"},
		{"after a type", "type T ~ int /* x\n y */\nrule q = \"a\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.src); got != tc.src {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.src)
			}
		})
	}
}
