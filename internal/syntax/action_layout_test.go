package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// Calls, constructors, list constructors and tuples own the layout of their
// delimiters and elements, so a comment inside an action expression stays
// where it was written, relative to the tokens around it, and a multiline
// layout keeps its indentation.
func TestActionExpressionLayout(t *testing.T) {
	const prelude = "type S = { A int; B int }\n"
	for _, tc := range []struct {
		name, in, want string
	}{
		{
			name: "group in an action block",
			in:   prelude + "rule r = \"a\" => {\n    x := (\n        // lead\n        1\n    )\n    x\n}\n",
			want: prelude + "rule r = \"a\" => {\n    x := (\n        // lead\n        1\n    )\n    x\n}\n",
		},
		{
			name: "comment-only unit in an action block",
			in:   prelude + "rule r = \"a\" => {\n    x := (\n        // lead\n    )\n    x\n}\n",
			want: prelude + "rule r = \"a\" => {\n    x := (\n        // lead\n    )\n    x\n}\n",
		},
		{
			name: "tuple in an action block",
			in:   prelude + "rule r = \"a\" => {\n    x := (\n        1,\n        2\n    )\n    x\n}\n",
			want: prelude + "rule r = \"a\" => {\n    x := (1, 2)\n    x\n}\n",
		},
		{
			name: "nested multiline terms in an action block",
			in:   prelude + "rule r = \"a\" => {\n    x := f(\n        // lead\n        g(\n            // inner\n            1\n        )\n    )\n    x\n}\n",
			want: prelude + "rule r = \"a\" => {\n    x := f(\n        // lead\n        g(\n            // inner\n            1,\n        ),\n    )\n    x\n}\n",
		},
		{
			name: "comment after a tuple element",
			in:   "rule r = \"a\" => (1 /* c */, 2)\n",
			want: "rule r = \"a\" => (1 /* c */, 2)\n",
		},
		{
			name: "comment after the last tuple element",
			in:   "rule r = \"a\" => (1, 2 /* c */,)\n",
			want: "rule r = \"a\" => (\n    1,\n    2 /* c */,\n)\n",
		},
		{
			name: "comment after the only tuple element",
			in:   "rule r = \"a\" => (1 /* c */,)\n",
			want: "rule r = \"a\" => (1 /* c */,)\n",
		},
		{
			name: "comment before a tuple element",
			in:   "rule r = \"a\" => (1, /* c */ 2)\n",
			want: "rule r = \"a\" => (1, /* c */ 2)\n",
		},
		{
			name: "call with only a comment",
			in:   "rule r = \"a\" => f(\n    // lead\n)\n",
			want: "rule r = \"a\" => f(\n    // lead\n)\n",
		},
		{
			name: "comment after a call argument",
			in:   "rule r = \"a\" => f(1 /* c */, 2)\n",
			want: "rule r = \"a\" => f(1 /* c */, 2)\n",
		},
		{
			name: "comment before a call argument",
			in:   "rule r = \"a\" => f(1, /* c */ 2)\n",
			want: "rule r = \"a\" => f(1, /* c */ 2)\n",
		},
		{
			name: "comment before the first call argument",
			in:   "rule r = \"a\" => f(/* c */ 1, 2)\n",
			want: "rule r = \"a\" => f(/* c */ 1, 2)\n",
		},
		{
			name: "leading and trailing line comments in a call",
			in:   "rule r = \"a\" => f(\n    // lead\n    1,\n    2, // tail\n    // end\n)\n",
			want: "rule r = \"a\" => f(\n    // lead\n    1,\n    2, // tail\n    // end\n)\n",
		},
		{
			name: "line comment after a call argument",
			in:   "rule r = \"a\" => f(\n    1, // one\n    2\n)\n",
			want: "rule r = \"a\" => f(\n    1, // one\n    2,\n)\n",
		},
		{
			name: "line comment before a comma",
			in:   "rule r = \"a\" => f(\n    1 // one\n    , 2\n)\n",
			want: "rule r = \"a\" => f(\n    1, // one\n    2,\n)\n",
		},
		{
			name: "line comment before a trailing comma",
			in:   "rule r = \"a\" => f(\n    1, 2 // two\n    ,\n)\n",
			want: "rule r = \"a\" => f(\n    1,\n    2, // two\n)\n",
		},
		{
			name: "block comment with lines in a call",
			in:   "rule r = \"a\" => f(\n    1, /* a\n  b */ 2\n)\n",
			want: "rule r = \"a\" => f(\n    1,\n    /* a\n  b */ 2,\n)\n",
		},
		{
			name: "comment delimiters in a string",
			in:   "rule r = \"a\" => f(\n    // c\n    \"a//b\", \"/*\",\n    \"x\"\n)\n",
			want: "rule r = \"a\" => f(\n    // c\n    \"a//b\",\n    \"/*\",\n    \"x\",\n)\n",
		},
		{
			name: "call with a trailing comma has one argument per line",
			in:   "rule r = \"a\" => f(\n    1,\n    2,\n)\n",
			want: "rule r = \"a\" => f(\n    1,\n    2,\n)\n",
		},
		{
			name: "struct literal with a leading comment",
			in:   prelude + "rule r = \"a\" => S{\n    // lead\n    A: 1,\n    B: 2,\n}\n",
			want: prelude + "rule r = \"a\" => S{\n    // lead\n    A: 1,\n    B: 2,\n}\n",
		},
		{
			name: "struct literal with a comment on the opening line",
			in:   prelude + "rule r = \"a\" => S{ // open\n    A: 1\n}\n",
			want: prelude + "rule r = \"a\" => S{ // open\n    A: 1,\n}\n",
		},
		{
			name: "comment after a field",
			in:   prelude + "rule r = \"a\" => S{A: 1 /* a */, B: 2}\n",
			want: prelude + "rule r = \"a\" => S{A: 1 /* a */, B: 2}\n",
		},
		{
			name: "comment before a field",
			in:   prelude + "rule r = \"a\" => S{A: 1, /* b */ B: 2}\n",
			want: prelude + "rule r = \"a\" => S{A: 1, /* b */ B: 2}\n",
		},
		{
			name: "comment before a closing brace",
			in:   prelude + "rule r = \"a\" => S{A: 1, B: 2 /* b */}\n",
			want: prelude + "rule r = \"a\" => S{A: 1, B: 2 /* b */}\n",
		},
		{
			name: "positional constructor",
			in:   "type N = node[string]\nrule r = \"a\" => N{\n    // lead\n    $x\n}\n",
			want: "type N = node[string]\nrule r = \"a\" => N{\n    // lead\n    $x,\n}\n",
		},
		{
			name: "empty list constructor with a comment",
			in:   prelude + "rule r = \"a\" => []S{ // lead\n}\n",
			want: prelude + "rule r = \"a\" => []S{ // lead\n}\n",
		},
		{
			name: "empty list constructor with a comment line",
			in:   prelude + "rule r = \"a\" => []S{\n    // lead\n}\n",
			want: prelude + "rule r = \"a\" => []S{\n    // lead\n}\n",
		},
		{
			name: "list constructor with a leading comment",
			in:   prelude + "rule r = \"a\" => []S{\n    // lead\n    1,\n}\n",
			want: prelude + "rule r = \"a\" => []S{\n    // lead\n    1,\n}\n",
		},
		{
			name: "list constructor with comments around items",
			in:   prelude + "rule r = \"a\" => []S{\n    1 /* a */, /* b */ 2, // c\n    // end\n}\n",
			want: prelude + "rule r = \"a\" => []S{\n    1 /* a */,\n    /* b */ 2, // c\n    // end\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkFormat(t, tc.in)
			if got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			if again := checkFormat(t, got); again != got {
				t.Fatalf("format is not idempotent:\n%s\n%s", got, again)
			}
		})
	}
}

// The new layout fields are not serialized and are not shared by a clone.
func TestActionExpressionLayoutCloneAndJSON(t *testing.T) {
	g, err := Parse("rule r = \"a\" => f(\n    // lead\n    1 /* after */, 2\n)\n")
	if err != nil {
		t.Fatal(err)
	}
	call := g.Rules()[0].Action.(*grammar.Call)
	if call.OpenBreak == nil || call.BodyBreak == nil || call.CloseBreak == nil {
		t.Fatalf("call layout: %+v", call)
	}
	if len(call.BodyBreak.Comments) != 1 || call.BodyBreak.Comments[0].Text != "// lead" {
		t.Fatalf("body comments: %+v", call.BodyBreak.Comments)
	}
	after := call.Args[0].(grammar.SourceNode).Source().Layout
	if after == nil || len(after.After) != 1 || after.After[0].Text != "/* after */" {
		t.Fatalf("comment after the first argument: %+v", after)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "after") {
		t.Fatalf("layout serialized: %s", data)
	}
	clone := g.Clone()
	copied := clone.Rules()[0].Action.(*grammar.Call)
	if copied.BodyBreak == call.BodyBreak || copied.BodyBreak.Comments[0] == call.BodyBreak.Comments[0] {
		t.Fatal("body break alias in clone")
	}
	copiedAfter := copied.Args[0].(grammar.SourceNode).Source().Layout
	if copiedAfter == after || copiedAfter.After[0] == after.After[0] {
		t.Fatal("after layout alias in clone")
	}
	if grammar.Format(clone) != grammar.Format(g) {
		t.Fatal("clone changed formatting")
	}
}

// A grammar without layout, such as one decoded from JSON, formats the calls,
// constructors and tuples on one line.
func TestActionExpressionWithoutLayout(t *testing.T) {
	g, err := Parse("type S = { A int }\nrule r = \"a\" => f(S{A: 1}, []S{S{A: 2}}, (1, 2))\n")
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
	const want = "type S = {\n    A int\n}\n\nrule r = \"a\" => f(S{A: 1}, []S{S{A: 2}}, (1, 2))\n"
	if got := grammar.Format(bare); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A list constructor can be indexed. The formatter parenthesizes it, which keeps
// the "[" of the index after a token that can be indexed.
func TestActionExpressionIndexedListConstructor(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"rule r = \"a\" => []int{1, 2}[0]\n", "rule r = \"a\" => ([]int{1, 2})[0]\n"},
		{"rule r = \"a\" => []int{1 /* a */, 2}[0]\n", "rule r = \"a\" => ([]int{1 /* a */, 2})[0]\n"},
		{"rule r = \"a\" => []int{\n    // lead\n    1,\n}[0]\n", "rule r = \"a\" => (\n    []int{\n        // lead\n        1,\n    }\n)[0]\n"},
		{"rule r = \"a\" => f(1 /* a */, 2)[0]\n", "rule r = \"a\" => f(1 /* a */, 2)[0]\n"},
	} {
		got := format(t, tc.in)
		if got != tc.want {
			t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
		}
		if again := format(t, got); again != got {
			t.Errorf("format is not idempotent:\n%s\n%s", got, again)
		}
	}
}
