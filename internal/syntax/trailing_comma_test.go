package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// A comma-separated list in brackets stays on one line, unless it has a
// trailing comma or a comment that needs its own line. Then each element has a
// line and ends with a comma. The comma of a one-element tuple is part of the
// syntax and does not count as a trailing comma.
func TestTrailingCommaLists(t *testing.T) {
	// A comment between two tokens of one line, before an element of a list on
	// lines, cannot stay between them.
	inlineBeforeElement := map[string]bool{
		"call with an inline block comment and a trailing comma": true,
	}
	const prelude = "type S = { A int; B int }\n"
	for _, tc := range []struct {
		name, in, want string
	}{
		// Calls.
		{"call", "rule r = \"a\" => f(1, 2)\n", "rule r = \"a\" => f(1, 2)\n"},
		{"call over lines", "rule r = \"a\" => f(\n    1,\n    2\n)\n", "rule r = \"a\" => f(1, 2)\n"},
		{"call with a trailing comma", "rule r = \"a\" => f(1, 2,)\n", "rule r = \"a\" => f(\n    1,\n    2,\n)\n"},
		{"call with one argument and a trailing comma", "rule r = \"a\" => f(1,)\n", "rule r = \"a\" => f(\n    1,\n)\n"},
		{"call without arguments", "rule r = \"a\" => f()\n", "rule r = \"a\" => f()\n"},
		{"call with a line comment", "rule r = \"a\" => f(1, // c\n    2)\n", "rule r = \"a\" => f(\n    1, // c\n    2,\n)\n"},
		{"call with a comment line", "rule r = \"a\" => f(1,\n    // c\n    2)\n", "rule r = \"a\" => f(\n    1,\n    // c\n    2,\n)\n"},
		{"call with a comment before the closing parenthesis", "rule r = \"a\" => f(1, 2\n    // c\n)\n", "rule r = \"a\" => f(\n    1,\n    2,\n    // c\n)\n"},
		// A comment on the line of a comma, after it, trails the element before the
		// comma, and one that ends its line stays on that line.
		{"list with a line comment after the last comma", "rule r = \"a\" => []int{1, 2, // c\n}\n", "rule r = \"a\" => []int{\n    1,\n    2, // c\n}\n"},
		{"list with a line comment after a comma", "rule r = \"a\" => []int{1, // c\n    2}\n", "rule r = \"a\" => []int{\n    1, // c\n    2,\n}\n"},
		{"tuple with a line comment after a comma", "rule r = \"a\" => (1, // c\n    2)\n", "rule r = \"a\" => (\n    1, // c\n    2,\n)\n"},
		{"call with a block comment before a line break after the last element", "rule r = \"a\" => f(1, 2 /* c */\n)\n", "rule r = \"a\" => f(\n    1,\n    2, /* c */\n)\n"},
		{"call with a block comment before a line break and a comma", "rule r = \"a\" => f(1 /* c */\n, 2)\n", "rule r = \"a\" => f(\n    1, /* c */\n    2,\n)\n"},
		{"call with a block comment after a comma and before a line break", "rule r = \"a\" => f(1, /* c */\n    2)\n", "rule r = \"a\" => f(\n    1, /* c */\n    2,\n)\n"},
		{"constructor with a block comment before a line break", "rule r = \"a\" => N{x /* c */\n}\n", "rule r = \"a\" => N{\n    x, /* c */\n}\n"},
		// Two line comments around a comma cannot share a line, and a comment that
		// belongs to nothing must not become one that trails the element.
		{"tuple with a line comment before and after a comma", "rule r = \"a\" => (1 // a\n, // b\n)\n", "rule r = \"a\" => (\n    1 // a\n    , // b\n)\n"},
		{"call with a line comment before and after a comma", "rule r = \"a\" => f(1 // a\n, // b\n2)\n", "rule r = \"a\" => f(\n    1 // a\n    , // b\n    2,\n)\n"},
		{"call with a comment line before a comma", "rule r = \"a\" => f(1\n    // m\n    , 2)\n", "rule r = \"a\" => f(\n    1\n    // m\n    ,\n    2,\n)\n"},
		{"call with a block comment between an element and its comma", "rule r = \"a\" => f(1 /* c */, 2)\n", "rule r = \"a\" => f(1 /* c */, 2)\n"},
		{"attribute with a trailing comma after positional arguments", "rule r = \"a\" @x(1, 2,) \"b\"\n", "rule r = \"a\" @x(\n    1,\n    2,\n) \"b\"\n"},
		{"call with a block comment on a line of its own before an element", "rule r = \"a\" => f(\n    /* c */ 1, 2)\n", "rule r = \"a\" => f(\n    /* c */\n    1,\n    2,\n)\n"},
		{"call with an inline block comment", "rule r = \"a\" => f(1 /* c */, 2)\n", "rule r = \"a\" => f(1 /* c */, 2)\n"},
		{"call with an inline block comment and a trailing comma", "rule r = \"a\" => f(/* c */ 1, 2,)\n", "rule r = \"a\" => f(\n    /* c */\n    1,\n    2,\n)\n"},
		{"call with a block comment after an element and a trailing comma", "rule r = \"a\" => f(1 /* c */, 2,)\n", "rule r = \"a\" => f(\n    1 /* c */,\n    2,\n)\n"},
		{"nested calls", "rule r = \"a\" => f(g(1, 2,), 3)\n", "rule r = \"a\" => f(\n    g(\n        1,\n        2,\n    ),\n    3,\n)\n"},
		{"nested call with a trailing comma only in the outer call", "rule r = \"a\" => f(g(1, 2), 3,)\n", "rule r = \"a\" => f(\n    g(1, 2),\n    3,\n)\n"},
		// Constructors.
		{"struct constructor", prelude + "rule r = \"a\" => S{A: 1, B: 2}\n", prelude + "rule r = \"a\" => S{A: 1, B: 2}\n"},
		{"struct constructor over lines", prelude + "rule r = \"a\" => S{\n    A: 1,\n    B: 2\n}\n", prelude + "rule r = \"a\" => S{A: 1, B: 2}\n"},
		{"struct constructor with a trailing comma", prelude + "rule r = \"a\" => S{A: 1, B: 2,}\n", prelude + "rule r = \"a\" => S{\n    A: 1,\n    B: 2,\n}\n"},
		{"struct constructor with a line comment", prelude + "rule r = \"a\" => S{A: 1, // c\n    B: 2}\n", prelude + "rule r = \"a\" => S{\n    A: 1, // c\n    B: 2,\n}\n"},
		{"struct constructor with a block comment", prelude + "rule r = \"a\" => S{A: 1 /* c */, B: 2}\n", prelude + "rule r = \"a\" => S{A: 1 /* c */, B: 2}\n"},
		{"struct constructor with a nested trailing comma", prelude + "rule r = \"a\" => S{A: f(1,), B: 2}\n", prelude + "rule r = \"a\" => S{\n    A: f(\n        1,\n    ),\n    B: 2,\n}\n"},
		{"node constructor", "type N = node[string]\nrule r = \"a\" => N{$x}\n", "type N = node[string]\nrule r = \"a\" => N{$x}\n"},
		{"node constructor with a trailing comma", "type N = node[string]\nrule r = \"a\" => N{$x,}\n", "type N = node[string]\nrule r = \"a\" => N{\n    $x,\n}\n"},
		{"empty constructor", prelude + "rule r = \"a\" => S{}\n", prelude + "rule r = \"a\" => S{}\n"},
		// List constructors.
		{"list constructor", "rule r = \"a\" => []int{1, 2}\n", "rule r = \"a\" => []int{1, 2}\n"},
		{"list constructor over lines", "rule r = \"a\" => []int{\n    1,\n    2\n}\n", "rule r = \"a\" => []int{1, 2}\n"},
		{"list constructor with a trailing comma", "rule r = \"a\" => []int{1, 2,}\n", "rule r = \"a\" => []int{\n    1,\n    2,\n}\n"},
		{"list constructor with a comment", "rule r = \"a\" => []int{1, 2 // c\n}\n", "rule r = \"a\" => []int{\n    1,\n    2, // c\n}\n"},
		{"empty list constructor", "rule r = \"a\" => []int{}\n", "rule r = \"a\" => []int{}\n"},
		{"list constructor of tuples", "rule r = \"a\" => []int{(1, 2,), (3, 4)}\n", "rule r = \"a\" => []int{\n    (\n        1,\n        2,\n    ),\n    (3, 4),\n}\n"},
		// Tuples.
		{"tuple", "rule r = \"a\" => (1, 2)\n", "rule r = \"a\" => (1, 2)\n"},
		{"tuple over lines", "rule r = \"a\" => (\n    1,\n    2\n)\n", "rule r = \"a\" => (1, 2)\n"},
		{"tuple with a trailing comma", "rule r = \"a\" => (1, 2,)\n", "rule r = \"a\" => (\n    1,\n    2,\n)\n"},
		{"tuple with three elements and a trailing comma", "rule r = \"a\" => (1, 2, 3,)\n", "rule r = \"a\" => (\n    1,\n    2,\n    3,\n)\n"},
		{"tuple of one element", "rule r = \"a\" => (1,)\n", "rule r = \"a\" => (1,)\n"},
		{"tuple of one element over lines", "rule r = \"a\" => (\n    1,\n)\n", "rule r = \"a\" => (1,)\n"},
		{"tuple of one element with a line comment", "rule r = \"a\" => (1, // c\n)\n", "rule r = \"a\" => (\n    1, // c\n)\n"},
		{"tuple of one element with a block comment", "rule r = \"a\" => (1 /* c */,)\n", "rule r = \"a\" => (1 /* c */,)\n"},
		{"unit", "rule r = \"a\" => ()\n", "rule r = \"a\" => ()\n"},
		{"tuple with a line comment", "rule r = \"a\" => (1, 2 // c\n)\n", "rule r = \"a\" => (\n    1,\n    2, // c\n)\n"},
		{"tuple with a block comment", "rule r = \"a\" => (1, /* c */ 2)\n", "rule r = \"a\" => (1, /* c */ 2)\n"},
		{"tuple with a block comment and a trailing comma", "rule r = \"a\" => (1 /* c */, 2,)\n", "rule r = \"a\" => (\n    1 /* c */,\n    2,\n)\n"},
		{"tuple with a one-element tuple", "rule r = \"a\" => ((1,), 2)\n", "rule r = \"a\" => ((1,), 2)\n"},
		{"tuple with a nested trailing comma", "rule r = \"a\" => ((1, 2,), 3)\n", "rule r = \"a\" => (\n    (\n        1,\n        2,\n    ),\n    3,\n)\n"},
		{"tuple in a block", "rule r = \"a\" => {\n    x := (1, 2,)\n    x\n}\n", "rule r = \"a\" => {\n    x := (\n        1,\n        2,\n    )\n    x\n}\n"},
		// Types.
		{"tuple type", "type T ~ (int, string)\n", "type T ~ (int, string)\n"},
		{"tuple type with a trailing comma", "type T ~ (int, string,)\n", "type T ~ (\n    int,\n    string,\n)\n"},
		{"tuple type of one element", "type T ~ (int,)\n", "type T ~ (int,)\n"},
		{"tuple type of one element over lines", "type T ~ (\n    int,\n)\n", "type T ~ (int,)\n"},
		{"tuple type over lines", "type T ~ (\n    int,\n    string\n)\n", "type T ~ (int, string)\n"},
		{"tuple type with a comment", "type T ~ (\n    // c\n    int,\n    string\n)\n", "type T ~ (\n    // c\n    int,\n    string,\n)\n"},
		{"type arguments", "type T ~ node[string]\n", "type T ~ node[string]\n"},
		{"type arguments over lines", "type T ~ node[\n    string\n]\n", "type T ~ node[string]\n"},
		{"type arguments with a trailing comma", "type T ~ node[string,]\n", "type T ~ node[\n    string,\n]\n"},
		{"type arguments in a field", "type T = { F node[string,]; G int }\n", "type T = {\n    F node[\n        string,\n    ]\n    G int\n}\n"},
		{"tuple type in type arguments", "type T ~ node[(int, string,)]\n", "type T ~ node[\n    (\n        int,\n        string,\n    ),\n]\n"},
		{"tuple type in a struct", "type T = { F (int, string,); G int }\n", "type T = {\n    F (\n        int,\n        string,\n    )\n    G int\n}\n"},
		// Attribute arguments.
		{"attribute", "rule r = \"a\" @error(message=\"m\", code=1) \"b\"\n", "rule r = \"a\" @error(message=\"m\", code=1) \"b\"\n"},
		{"attribute with a trailing comma", "rule r = \"a\" @error(message=\"m\", code=1,) \"b\"\n", "rule r = \"a\" @error(\n    message=\"m\",\n    code=1,\n) \"b\"\n"},
		{"attribute over lines", "rule r = \"a\" @error(\n    message=\"m\",\n    code=1\n) \"b\"\n", "rule r = \"a\" @error(message=\"m\", code=1) \"b\"\n"},
		{"attribute with positional arguments and a trailing comma", "rule r = \"a\" @x(1, k=2,) \"b\"\n", "rule r = \"a\" @x(\n    1,\n    k=2,\n) \"b\"\n"},
		{"attribute with a single argument and a trailing comma", "rule r = \"a\" @error(message=\"m\",)\n", "rule r = \"a\" @error(\n    message=\"m\",\n)\n"},
		{"attribute in a group", "rule r = (\"a\" @error(message=\"m\",) \"b\") \"c\"\n", "rule r = (\n    \"a\" @error(\n        message=\"m\",\n    ) \"b\"\n) \"c\"\n"},
		// The lines of a list break the construct around it, which would read as lines next time.
		{"block", "rule r = \"a\" {f(1,)} \"b\"\n", "rule r = \"a\" {\n    f(\n        1,\n    )\n} \"b\"\n"},
		{"group with an action", "rule r = (\"a\" => f(1,)) \"b\"\n", "rule r = (\n    \"a\" => f(\n        1,\n    )\n) \"b\"\n"},
		{"operators", "rule e Expr = operators {\n    operand n:int => f(1,)\n    level { infix left \"+\" => g(1, 2,) }\n    level { infix left \"-\" => g(1, 2) }\n}\n", "rule e Expr = operators {\n    operand n:int => f(\n        1,\n    )\n    level {\n        infix left \"+\" => g(\n            1,\n            2,\n        )\n    }\n    level { infix left \"-\" => g(1, 2) }\n}\n"},
		// Lists that take no trailing comma keep their form.
		{"lambda parameters", "rule r = \"a\" => map(list(), (a, b) => a)\n", "rule r = \"a\" => map(list(), (a, b) => a)\n"},
		{"repeat bounds", "rule r = \"a\" #repeat(1, 2)\n", "rule r = \"a\" #repeat(1, 2)\n"},
		{"recover", "rule r = \"a\" #recover(skip=\",\" / \";\")\n", "rule r = \"a\" #recover(skip=\",\" / \";\")\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := checkFormat
			if inlineBeforeElement[tc.name] {
				check = checkFormatLines
			}
			got := check(t, tc.in)
			if got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			if again := check(t, got); again != got {
				t.Fatalf("format is not idempotent:\n%s\n%s", got, again)
			}
		})
	}
}

// The syntax of lambda parameters has no trailing comma.
func TestLambdaParametersTakeNoTrailingComma(t *testing.T) {
	if _, err := Parse("rule r = \"a\" => map(list(), (a, b,) => a)\n"); err == nil {
		t.Fatal("a trailing comma after lambda parameters parsed")
	}
}

// A grammar without source layout, such as one decoded from JSON, has no
// trailing commas and keeps its lists on one line.
func TestTrailingCommaNotSerialized(t *testing.T) {
	const src = "rule r = \"a\" => f(1, (2, 3,), []int{4,})\n"
	const want = "rule r = \"a\" => f(\n    1,\n    (\n        2,\n        3,\n    ),\n    []int{\n        4,\n    },\n)\n"
	if got := format(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	bare, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	const flat = "rule r = \"a\" => f(1, (2, 3), []int{4})\n"
	if got := grammar.Format(bare); got != flat {
		t.Fatalf("got:\n%s\nwant:\n%s", got, flat)
	}
}

// A comment after a list comma, on the comma's line, trails the element before
// the comma, like the comment after a separator trails the node before it. It
// replaces a trailing comment before the comma, which then has no owner.
func TestListCommaCommentTrailsElement(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"call", "f(1, // c\n    2)", " c\n"},
		{"last comma", "f(1, // c\n)", " c\n"},
		{"list constructor", "[]int{1, // c\n    2}", " c\n"},
		{"tuple", "(1, // c\n    2)", " c\n"},
		{"block comment", "f(1, /* c */\n    2)", " c "},
		{"before and after the comma", "f(1 // a\n    , // b\n    2)", " b\n"},
		{"before the comma", "f(1 // a\n    , 2)", " a\n"},
		{"inline block comment", "f(1, /* c */ 2)", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Parse("rule r = \"a\" => " + tc.src + "\n")
			if err != nil {
				t.Fatal(err)
			}
			var args []grammar.Term
			switch x := g.File.Decls[0].(*grammar.RuleDef).Action.(type) {
			case *grammar.Call:
				args = x.Args
			case *grammar.ListConstructor:
				args = x.Items
			case *grammar.TupleLit:
				args = x.Elems
			}
			if got := args[0].(grammar.SourceNode).Source().TrailingComments; got != tc.want {
				t.Fatalf("the trailing comment of the first element is %q, want %q", got, tc.want)
			}
		})
	}
}

// A block comment that stands on a line of its own, or that ends its line,
// is not between two tokens of one line, so a group around it is not written on
// one line, which would change the comment's owner.
func TestGroupBlockCommentsKeepOwners(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"on a line of its own before the element", "rule r = \"a\" => (\n    /* c */ 1\n)\n", "rule r = \"a\" => (\n    /* c */\n    1\n)\n"},
		{"ending the line of the element", "rule r = \"a\" => (1 /* c */\n)\n", "rule r = \"a\" => (\n    1 /* c */\n)\n"},
		{"ending the line of the opening parenthesis", "rule r = \"a\" => (/* c */\n    1)\n", "rule r = \"a\" => ( /* c */\n    1\n)\n"},
		{"between two tokens of a line", "rule r = \"a\" => (/* c */ 1 /* d */)\n", "rule r = \"a\" => ( /* c */ 1 /* d */)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// The comma of #repeat carries meaning, so it is not a trailing comma: it
// stays, on one line, and its bounds do not change.
func TestRepeatPragmaCommaIsNotTrailing(t *testing.T) {
	for _, tc := range []struct {
		pragma   string
		min, max int
	}{
		{"#repeat(2)", 2, 2},
		{"#repeat(2,)", 2, -1},
		{"#repeat(2, )", 2, -1},
		{"#repeat(,3)", 0, 3},
		{"#repeat(2,3)", 2, 3},
	} {
		t.Run(tc.pragma, func(t *testing.T) {
			g, err := Parse("rule r = \"a\" " + tc.pragma + " \"b\"\n")
			if err != nil {
				t.Fatal(err)
			}
			rep := g.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.Repeat)
			if rep.Min != tc.min || rep.Max != tc.max {
				t.Fatalf("bounds %d,%d, want %d,%d", rep.Min, rep.Max, tc.min, tc.max)
			}
			out := grammar.Format(g)
			if strings.Contains(out, "\n    ") {
				t.Fatalf("broken into lines:\n%s", out)
			}
			again, err := Parse(out)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			rep = again.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.Repeat)
			if rep.Min != tc.min || rep.Max != tc.max {
				t.Fatalf("formatted bounds %d,%d, want %d,%d\n%s", rep.Min, rep.Max, tc.min, tc.max, out)
			}
			if tc.max < 0 && !strings.Contains(out, "#repeat(2, )") {
				t.Fatalf("the comma was removed:\n%s", out)
			}
		})
	}
}
