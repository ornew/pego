package compile

import (
	"strings"
	"testing"
)

// TestTextFieldComparisonsRejected checks the comparisons the checker rejects.
func TestTextFieldComparisonsRejected(t *testing.T) {
	t.Parallel()
	const prelude = "type Name = { Text string }\ntype Ops = { Eq bool; Ne bool; Lt bool; Le bool; Gt bool; Ge bool }\n" +
		"rule word = [a-z]+\nrule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}\n"
	for _, tc := range []struct{ rule, want string }{
		{`"?" a:name? "!" => Ops{Eq: $a.Text == "x"}`, "cannot access .Text of *Name: the value may be nil"},
		{`"#" a:name => Ops{Eq: $a.Missing == "x"}`, "Name has no field Missing"},
		{`"~" a:word => Ops{Lt: $a.Value < "m"}`, "invalid operands []node[string] and string for <"},
		{`"=" a:name => Ops{Ge: $a.Text >= 1}`, "invalid operands string and int for >="},
		{`"&" a:name b:name => Ops{Gt: $a.Text > text($b)}`, "text: invalid argument"},
	} {
		if got := compileMessage(t, prelude+"rule main = "+tc.rule); !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want an error containing %q", tc.rule, got, tc.want)
		}
	}
}
