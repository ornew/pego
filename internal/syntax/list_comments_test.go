package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// Every comma-separated list keeps its comments with their owners and follows
// the trailing comma: the lists of types, the arguments of attributes and the
// parameters of lambdas as much as the lists of terms.
func TestFormatListComments(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"result type argument", "rule r node[int // c\n] = \"a\"\n", "rule r node[\n    int, // c\n] = \"a\"\n"},
		{"result type block comment", "rule r node[int /* c */] = \"a\"\n", "rule r node[int /* c */] = \"a\"\n"},
		{"result type tuple", "rule r (int, // c\n string) = \"a\"\n", "rule r (\n    int, // c\n    string,\n) = \"a\"\n"},
		{"tuple type", "type A ~ (int, // c\n string)\n", "type A ~ (\n    int, // c\n    string,\n)\n"},
		{"field type argument", "type T = { F node[int // c\n] }\n", "type T = {\n    F node[\n        int, // c\n    ]\n}\n"},
		{"field type tuple on a line", "type T = { F (int /* b */, string) }\n", "type T = { F (int /* b */, string) }\n"},
		{"attribute line comment", "rule r = \"a\" @custom(1, // c\n 2)\n", "rule r = \"a\" @custom(\n    1, // c\n    2,\n)\n"},
		{"attribute block comment before the comma", "rule r = \"a\" @custom(1 /* b */, 2,)\n", "rule r = \"a\" @custom(\n    1 /* b */,\n    2,\n)\n"},
		{"attribute leading block comment", "rule r = \"a\" @custom(/* b */ 1, 2)\n", "rule r = \"a\" @custom( /* b */ 1, 2)\n"},
		{"attribute comment after the last argument", "rule r = \"a\" @custom(1, k=2, // c\n)\n", "rule r = \"a\" @custom(\n    1,\n    k=2, // c\n)\n"},
		{"lambda comment", "rule r = \"a\" => map(x, (a, // c\n b) => a)\n", "rule r = \"a\" => map(\n    x,\n    (\n        a, // c\n        b,\n    ) => a,\n)\n"},
		{"lambda trailing comma", "rule r = \"a\" => map(x, (a, b,) => a)\n", "rule r = \"a\" => map(\n    x,\n    (\n        a,\n        b,\n    ) => a,\n)\n"},
		// A block comment on the line of a delimiter or a comma leads the element
		// after it and trails the element before it.
		{"leading after the opener", "rule r = \"a\" => f(/* b */ 1, 2,)\n", "rule r = \"a\" => f(\n    /* b */\n    1,\n    2,\n)\n"},
		{"leading after the comma", "rule r = \"a\" => f(1, /* b */ 2,)\n", "rule r = \"a\" => f(\n    1,\n    /* b */ 2,\n)\n"},
		{"trailing before the closer", "rule r = \"a\" => f(1, 2, /* b */)\n", "rule r = \"a\" => f(\n    1,\n    2, /* b */\n)\n"},
		{"type leading after the opener", "rule r (/* b */ int, string,) = \"a\"\n", "rule r (\n    /* b */\n    int,\n    string,\n) = \"a\"\n"},
		{"type trailing before the closer", "rule r (int, string, /* b */) = \"a\"\n", "rule r (\n    int,\n    string, /* b */\n) = \"a\"\n"},
		// The comma line decides who trails: a comment after the comma trails the
		// element, also when the element has a comment before the comma.
		{"comment before and after the comma", "rule r (string // l\n    , /* p */ /* q */ string) = \"a\"\n", "rule r (\n    string // l\n    , /* p */ /* q */ string,\n) = \"a\"\n"},
		// Blank lines stay, with no whitespace on them.
		{"blank lines around a comment", "rule r = \"a\" => f(1,\n\n    // r\n\n    2)\n", "rule r = \"a\" => f(\n    1,\n\n    // r\n\n    2,\n)\n"},
		{"blank line before the first comment", "rule r (\n\n    // r\n\n    int, string) = \"a\"\n", "rule r (\n\n    // r\n\n    int,\n    string,\n) = \"a\"\n"},
		// An empty argument is written nil.
		{"attribute nil argument", "rule r = \"a\" @error(nil, \"bad\")\n", "rule r = \"a\" @error(nil, \"bad\")\n"},
		// A comment before the brace of a struct stays inside it.
		{"struct closing comment", "type T = { F (int, string) /* b */ }\n", "type T = { F (int, string) /* b */ }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if problem := checkRandomFormat(tc.in, ast); problem != "" {
				t.Fatalf("%s", problem)
			}
			got := checkFormat(t, tc.in)
			if got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			for i, line := range strings.Split(got, "\n") {
				if strings.TrimRight(line, " \t") != line {
					t.Errorf("line %d has trailing whitespace: %q", i+1, line)
				}
			}
		})
	}
}

// A comment on a line after the comma's belongs to the next element or to
// nothing, never to the element before the comma.
func TestFormatListCommentOnLaterLine(t *testing.T) {
	ast, err := Parse("rule r = \"a\" => f(1,\n\n    // r\n\n    2)\n")
	if err != nil {
		t.Fatal(err)
	}
	arg := ast.Rules()[0].Action.(*grammar.Call).Args[0].(grammar.SourceNode).Source()
	if arg.TrailingComments != "" {
		t.Fatalf("the first argument trails %q", arg.TrailingComments)
	}
}

// The comments inside #repeat(...) stay there, and its bounds keep the form
// they were written in.
func TestFormatRepeatPragmaComments(t *testing.T) {
	const in = "rule r = \"a\" #repeat(\n  // c\n  2, 3) \"b\"\n"
	const want = "rule r = \"a\" #repeat(\n    // c\n    2, 3\n) \"b\"\n"
	if got := checkFormat(t, in); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A comma where an argument belongs is reported as such, also after a keyword
// argument.
func TestAttributeDoubledComma(t *testing.T) {
	for _, src := range []string{
		"rule r = \"a\" @custom(k=1,,)\n",
		"rule r = \"a\" @custom(1,,2)\n",
		"rule r = \"a\" @custom(, 1)\n",
	} {
		_, err := Parse(src)
		if err == nil || !strings.Contains(err.Error(), "expected an attribute argument, found ','") {
			t.Errorf("%q: got %v", src, err)
		}
	}
}
