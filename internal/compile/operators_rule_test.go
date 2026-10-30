package compile

import (
	"strings"
	"testing"
)

func TestOperatorsRuleCompileErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule e = #operators {
    operand e "x" / "1"
}`, `operand of e calls e at its start (left recursion)`},
		{`rule e = #operators {
    operand "1"
    level { infix left lhs:"+" }
}`, `capture name lhs is reserved in operatorsRule operators`},
		{`rule e = #operators {
    operand "1"
    level { prefix "-" => $lhs }
}`, `undefined capture $lhs`},
		{`rule e = #operators {
    operand "1"
    level a {}
    level a {}
}`, `duplicate level name a`},
		{`rule e = #operators {
    level { prefix "-" }
}`, `operatorsRule needs at least one operand`},
		{`rule main = level(e, nope)
rule e = #operators {
    operand "1"
}`, `rule e has no level nope`},
		{`rule main = "(" operators rule { operand "1" } ")"`, `syntax error`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			var got string
			if strings.Contains(tc.want, "syntax error") {
				got = "syntax error"
			} else {
				got = compileError(t, tc.src)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}
