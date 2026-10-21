package syntax

import "testing"

// Comments between the tokens of a rule expression stay where they are, so
// formatting never moves a comment to another node. checkFormat verifies the
// owners; the expected text pins the places.
func TestFormatKeepsCommentsBetweenTokens(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"after an attribute", `rule r = "a" @custom /* c */ "b"`, `rule r = "a" @custom /* c */ "b"`},
		{"between attributes", `rule r = "a" @custom /* c */ @other "b"`, `rule r = "a" @custom /* c */ @other "b"`},
		{"after a pragma", `rule r = "a" #atomic /* c */ "b"`, `rule r = "a" #atomic /* c */ "b"`},
		{"after a repeat", `rule r = "a"* /* c */ "b"`, `rule r = "a"* /* c */ "b"`},
		{"before a repeat", `rule r = "a" /* c */ * "b"`, `rule r = "a" /* c */ * "b"`},
		{"after an optional", `rule r = "a"? /* c */ "b"`, `rule r = "a"? /* c */ "b"`},
		{"around a choice", `rule r = "a" /* c */ / /* d */ "b"`, `rule r = "a" /* c */ / /* d */ "b"`},
		{"around an action arrow", `rule r = "a" /* c */ => /* d */ 1`, `rule r = "a" /* c */ => /* d */ 1`},
		{"before the parenthesis of a recovery", `rule r = "a" #recover(skip="b" /* c */) "c"`, `rule r = "a" #recover(skip="b" /* c */) "c"`},
		{"before the bracket of an index", `rule r = "a" => x[0 /* c */]`, `rule r = "a" => x[0 /* c */]`},
		{"before the bracket of an index, a line comment", "rule r = \"a\" => x[0 // c\n]", "rule r = \"a\" => x[\n    0 // c\n]"},
		{"in the brackets of an index, on lines of their own", "rule r = \"a\" => x[\n    // c\n    0\n]", "rule r = \"a\" => x[\n    // c\n    0\n]"},
		{"around a binary operator", `rule r = "a" => (1 /* c */ + /* d */ 2)`, `rule r = "a" => (1 /* c */ + /* d */ 2)`},
		{"after the brace of a block", "rule r = \"a\" { // c\n    x := 1\n    y := 2\n}", "rule r = \"a\" { // c\n    x := 1\n    y := 2\n}"},
		{"after the brace of a one-statement block", "rule r = \"a\" { // c\n    x := 1\n}", "rule r = \"a\" { // c\n    x := 1\n}"},
		{"around the semicolons of a block", `rule r = "a" { x := 1 /* c */ ; /* d */ y := 2 /* e */ ; }`, "rule r = \"a\" {\n    x := 1 /* c */; /* d */ y := 2 /* e */;\n}"},
		{"a blank line before the first comment of a block", "rule r = \"a\" {\n\n    // lead\n\n    x := 1\n}", "rule r = \"a\" {\n\n    // lead\n\n    x := 1\n}"},
		{"on a line of its own after a binary operator", "rule r = \"a\" => (1 +\n    // c\n    2)", "rule r = \"a\" => (\n    1 +\n    // c\n    2\n)"},
		{"on a line of its own before a binary operator", "rule r = \"a\" => (1\n    // c\n    + 2)", "rule r = \"a\" => (\n    1\n    // c\n    + 2\n)"},
		{"on a line of its own after a slash in a group", "rule r = ( \"a\" /\n    // c\n    \"b\" ) \"c\"", "rule r = (\n    \"a\"\n    /\n      // c\n      \"b\"\n) \"c\""},
		{"before a slash in a group, a block comment", "rule r = ( \"a\" /* c */ /\n    \"b\" ) \"c\"", "rule r = (\n    \"a\" /* c */ / \"b\"\n) \"c\""},
		{"on lines of their own in a recovery", "rule r = \"a\" #recover(skip=\n    // c\n    \"b\"\n    // d\n) \"c\"", "rule r = \"a\" #recover(skip=\n    // c\n    \"b\"\n    // d\n) \"c\""},
		{"a comment across lines before a repeat pragma", "rule r = ( \"a\" /* x\n y */ #repeat(2) ) \"b\"", "rule r = (\n    \"a\" /* x\n y */ #repeat(2)\n) \"b\""},
		{"a comment across lines before an atomic pragma", "rule r = ( \"a\" /* x\n y */ #atomic ) \"b\"", "rule r = (\n    \"a\" /* x\n y */ #atomic\n) \"b\""},
		{"a comment across lines before a recover pragma", "rule r = ( \"a\" /* x\n y */ #recover(skip=\"b\") ) \"c\"", "rule r = (\n    \"a\" /* x\n y */ #recover(skip=\"b\")\n) \"c\""},
		{"a comment across lines in a group", "rule r = ( \"a\" /* x\n y */ \"b\" ) \"c\"", "rule r = (\n    \"a\" /* x\n y */ \"b\"\n) \"c\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in+"\n"); got != tc.want+"\n" {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want+"\n")
			}
		})
	}
}
