package python_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/python"
)

// TestIndentationState covers scoped block state, including the deepest block
// CPython accepts. It runs unchanged on both Go integer widths.
func TestIndentationState(t *testing.T) {
	nested := func(n int, stmt string) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString(strings.Repeat(" ", i) + "if True:\n")
		}
		b.WriteString(strings.Repeat(" ", n) + stmt + "\n")
		return b.String()
	}
	cases := []struct {
		name, src string
		ok        bool
	}{
		{"nested and restored", "if True:\n    if False:\n        pass\n    else:\n        pass\n    pass\nelse:\n    pass\npass\n", true},
		{"tabs", "if True:\n\tif False:\n\t\tpass\n\tpass\npass\n", true},
		{"equal columns with inconsistent tabs", "if True:\n\tpass\n        pass\n", false},
		{"greater columns with inconsistent tabs", "if True:\n        if False:\n\t pass\n", false},
		{"unmatched dedent", "if True:\n    pass\n  pass\n", false},
		{"form feed reset", "if True:\n \t\f    pass\n    pass\n", true},
		{"decorators", "if True:\n    @first\n    @second\n    def f():\n        pass\n    pass\n", true},
		{"match cases", "match value:\n    case 1:\n        pass\n    case _:\n        pass\npass\n", true},
		{"99 blocks", nested(99, "pass"), true},
		{"100 blocks", nested(100, "pass"), false},
		{"deep f-string formats", nested(99, `f"{value:{width:{precision}}}"`), true},
		{"deep t-string formats", nested(98, `t"{value:{width:{precision}}}"`), true},
		{"deep raw strings", nested(99, `rf"{value}"; fr"{value}"; rt"{value}"; tr"{value}"`), true},
		{"deep nested strings", nested(99, `f"{f'{value}'}"; t"{t'{value}'}"`), true},
		{"excess f-string formats", nested(99, `f"{value:{width:{precision:{extra}}}}"`), false},
		{"excess t-string formats", nested(98, `t"{value:{width:{precision:{extra}}}}"`), false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, unit := range []python.Unit{python.CodePoints, python.Bytes} {
				_, moduleErr := python.ParseModule(tt.src, unit)
				_, astErr := python.ParseAST(tt.src, unit)
				_, nodeErr := python.Parse(tt.src, unit)
				recognizeErr := python.Recognize(tt.src, unit)
				for name, err := range map[string]error{"module": moduleErr, "AST": astErr, "Node": nodeErr, "recognize": recognizeErr} {
					if (err == nil) != tt.ok {
						t.Errorf("%s/%v: error = %v; accept = %v", name, unit, err, tt.ok)
					}
				}
			}
		})
	}
}
