package syntax

import (
	"fmt"
	"testing"
)

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

// A block comment over several lines that ends on the line of the next
// declaration, field or package clause belongs to that one, detached from it,
// not to the node before it. Formatting writes it on lines of its own before
// the next one, set off by blank lines, so that it keeps its owner.
func TestFormatKeepsCommentBeforeNodeOnItsLastLine(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"before a rule", "rule a = \"x\" /* c\n */ rule b = \"y\"\n", "rule a = \"x\"\n\n/* c\n */\n\nrule b = \"y\"\n"},
		{"before a rule after the package", "package p /* c\n */ rule b = \"y\"\n", "package p\n\n/* c\n */\n\nrule b = \"y\"\n"},
		{"before a variable", "var v = 1 /* c\n */ var w = 2\n", "var v = 1\n\n/* c\n */\n\nvar w = 2\n"},
		{"before a field", "type T = { A int /* c\n */ B int }\n", "type T = {\n    A int\n\n    /* c\n */\n\n    B int\n}\n"},
		{"before a field on lines", "type T = {\n A int /* c\n */ B int\n}\n", "type T = {\n    A int\n\n    /* c\n */\n\n    B int\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.src); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// The F rule applies only to a declaration, and only when the multi-line block
// comment is the only one before it. A comment before the closing brace of a
// struct or operators rule, or the first of several comments, still trails the
// node before it.
func TestFormatKeepsTrailingBlockBeforeBraceAndGroups(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{"before the brace of a struct", "package p\ntype T = {\n    A int /* c\n */\n}\nrule r = \"a\"\n"},
		{"before the brace of a struct on one line", "type T = {\n    A int /* c\n */\n}\n"},
		{"before the brace of an operators rule", "rule ops = operators {\n    operand \"n\" /* c\n */\n}\n"},
		{"first of two groups", "package p\nrule a = \"x\" /* c\n */ /* d */ rule b = \"y\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkFormat(t, tc.src)
		})
	}
	for i, src := range []string{
		"package p\ntype T = {\n    A int /* c\n */ }\nrule r = \"a\"\n",
		"type T = { A int /* c\n */ }\n",
		"rule ops = operators {\n    operand \"n\" /* c\n */ }\n",
		"package p\nrule a = \"x\" /* c\n */ /* d */ rule b = \"y\"\n",
	} {
		t.Run(fmt.Sprintf("brace on the comment line %d", i), func(t *testing.T) { checkFormat(t, src) })
	}
}
