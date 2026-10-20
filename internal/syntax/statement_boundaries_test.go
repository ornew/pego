package syntax

import (
	"strings"
	"testing"
)

// A newline ends a statement, and an expression crosses a newline only inside
// its own open delimiter.
func TestStatementBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		pos, msg  string // empty when the source is accepted
	}{
		{"struct fields on lines", "type T = {\n a string\n b string\n}\n", "", ""},
		{"struct fields with semicolons", "type T = { a string; b string }\n", "", ""},
		{"struct field semicolon then newline", "type T = { a string;\n b string }\n", "", ""},
		{"struct trailing semicolon", "type T = { a string; }\n", "", ""},
		{"action statements with semicolons", "rule a = b => {x; y}\n", "", ""},
		{"action statement semicolon then newline", "rule a = b => {x;\n y}\n", "", ""},
		{"operators items with semicolons", "rule a = operators { operand \"x\"; operand \"y\" }\n", "", ""},
		{"double semicolon in a struct", "type T = { a string;; b string }\n", "1:21", "expected identifier"},
		{"semicolon on its own line in a struct", "type T = {\n a string\n ;\n b string\n}\n", "3:2", "expected identifier"},
		{"semicolon after a comment in a struct", "type T = { a string // t\n; b string }\n", "2:1", "expected identifier"},
		{"semicolon on its own line in an action block", "rule a = b => {\n x\n ;\n y\n}\n", "3:2", ""},
		{"semicolon after a comment in an action block", "rule a = b => {x // t\n; y}\n", "2:1", ""},
		{"newline between = and { of a type", "type T =\n{ a string }\n", "2:1", "multiline expressions require their own open delimiter"},
		{"comment between = and { of a type", "type T = // c\n{ a string }\n", "2:1", "multiline expressions require their own open delimiter"},
		{"newline between => and an action block", "rule a = b =>\n{x; y}\n", "2:1", "multiline expressions require their own open delimiter"},
		{"newline between => and an action", "rule a = b =>\nx\n", "2:1", "multiline expressions require their own open delimiter"},
		{"newline between operators and {", "rule a = operators\n{ operand \"x\" }\n", "2:1", ""},
		{"newline between => and an action inside a group", "rule a = (b =>\n{x; y})\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if tc.pos == "" {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.HasPrefix(err.Error(), tc.pos+":") || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("got %q, want position %s and message %q", err, tc.pos, tc.msg)
			}
		})
	}
}
