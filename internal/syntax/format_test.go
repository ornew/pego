package syntax

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// format parses src and formats it.
func format(t *testing.T, src string) string {
	t.Helper()
	g, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	return grammar.Format(g)
}

// lexComments returns the texts of the comments in src, sorted.
func lexComments(t *testing.T, src string) []string {
	t.Helper()
	errs := &errorList{}
	var cs []string
	for _, tok := range lex(src, errs) {
		if tok.lineComment != nil {
			cs = append(cs, tok.lineComment.Text)
		}
		for _, c := range tok.comments {
			cs = append(cs, c.Text)
		}
	}
	if len(errs.list) > 0 {
		t.Fatal(errs.list)
	}
	slices.Sort(cs)
	return cs
}

// checkFormat checks that formatting src keeps its grammar and comments and
// is idempotent, and returns the result.
func checkFormat(t *testing.T, src string) string {
	t.Helper()
	g, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	// The parser keeps every comment.
	var kept []string
	for _, c := range g.AllComments() {
		kept = append(kept, c.Text)
	}
	slices.Sort(kept)
	want := lexComments(t, src)
	if !slices.Equal(kept, want) {
		t.Errorf("comments in the AST\n got %q\nwant %q", kept, want)
	}
	out := grammar.Format(g)
	g2, err := Parse(out)
	if err != nil {
		t.Fatalf("formatted source does not parse: %v\n%s", err, out)
	}
	if a, b := mustJSON(t, g), mustJSON(t, g2); a != b {
		t.Errorf("formatted source has a different grammar\n%s", out)
	}
	if got := lexComments(t, out); !slices.Equal(got, want) {
		t.Errorf("comments after formatting\n got %q\nwant %q\n%s", got, want, out)
	}
	if again := grammar.Format(g2); again != out {
		t.Errorf("formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", out, again)
	}
	return out
}

func mustJSON(t *testing.T, g *grammar.Grammar) string {
	t.Helper()
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFormatSource(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{
			name: "package and leading comments",
			in: `

// header

// about the package
package demo // trailing


// about A
type A terminal
def a = "a"
`,
			want: `// header

// about the package
package demo // trailing

// about A
type A terminal
def a = "a"
`,
		},
		{
			name: "blank lines collapse",
			in:   "def a = x\n\n\n\ndef b = y\ndef c = z\n\n",
			want: "def a = x\n\ndef b = y\ndef c = z\n",
		},
		{
			name: "struct fields",
			in: `type Op struct { // op
    // the left operand

    Left  Node   // left
    Op    OpType   // operator
    Right Node


    // end
}
type P struct { X A, Y B }  // pair
type E struct {}
`,
			want: `type Op struct { // op
    // the left operand

    Left  Node   // left
    Op    OpType // operator
    Right Node

    // end
}
type P struct { X A, Y B } // pair
type E struct {}
`,
		},
		{
			name: "rule body lines",
			in: `def expr: Node =
    // capture the operands
    l:term rest:(op:binop r:term)*
    // fold them
    -> foldl($l, $rest, (acc, i) => $i.r) // done
`,
			want: `def expr: Node =
    // capture the operands
    l:term rest:(op:binop r:term)*
    // fold them
    -> foldl($l, $rest, (acc, i) => $i.r) // done
`,
		},
		{
			name: "multi-line choice",
			in: `def value = object // first
    / array b
      c // continued
    // before the last
    / "null"
`,
			want: `def value = object // first
    / array b
      c // continued
    // before the last
    / "null"
`,
		},
		{
			name: "attribute on its own line",
			in: `def stmt = s:(a / b)
    // skip to the next statement
    #recover(skip=(?^;)+ ";"?) -> $s
`,
			want: `def stmt = s:(a / b)
    // skip to the next statement
    #recover(skip=(?^;)+ ";"?) -> $s
`,
		},
		{
			name: "pratt",
			in: `def e = pratt { // operators
    // operands
    operand num // number
    skip ws
    level { infix left "+" -> $lhs } // additive
    level postfix {

        // calls
        postfix "(" ")" // call
        // end of level
    }
    // end of pratt
}
`,
			want: `def e = pratt { // operators
    skip ws
    // operands
    operand num                      // number
    level { infix left "+" -> $lhs } // additive
    level postfix {
        // calls
        postfix "(" ")" // call
        // end of level
    }
    // end of pratt
}
`,
		},
		{
			name: "comments in joined lines",
			in: `def a = x (y // inside
    z) // after
def b = (p // one
    / q) // two
`,
			want: `// inside
def a = x (y z) // after
// one
def b = p / q // two
`,
		},
		{
			name: "blank line ends field alignment",
			in:   "type T struct {\n    A X\n\n    Long Y\n    B Z\n}\n",
			want: "type T struct {\n    A X\n\n    Long Y\n    B    Z\n}\n",
		},
		{
			name: "pratt body on its own line",
			in:   "def e =\n    // operators\n    pratt { operand x level { prefix \"-\" } }\n",
			want: "def e =\n    // operators\n    pratt {\n        operand x\n        level { prefix \"-\" }\n    }\n",
		},
		{
			name: "end of file",
			in:   "def a = x\n// last\n\n// very last\n\n",
			want: "def a = x\n// last\n\n// very last\n",
		},
		{
			name: "only comments",
			in:   "// nothing here\n",
			want: "// nothing here\n",
		},
		{
			name: "crlf",
			in:   "def a = x // c\r\n\r\ndef b = y\r\n",
			want: "def a = x // c\n\ndef b = y\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// Comments and layout are not part of the JSON form of a grammar.
func TestCommentsNotInJSON(t *testing.T) {
	withComments := `// file
package p // pkg

type A struct { // a
    X B // x
}
def r = a // r
    / b
    -> $0 // act
def e = pratt { // e
    operand x // x
}
`
	plain := "package p\ntype A struct { X B }\ndef r = a / b -> $0\ndef e = pratt { operand x }\n"
	g1, err := Parse(withComments)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := mustJSON(t, g1), mustJSON(t, g2); a != b {
		t.Errorf("JSON differs:\n%s\n---\n%s", a, b)
	}
}

// Formatting every example grammar keeps its grammar and comments.
func TestFormatExamples(t *testing.T) {
	var paths []string
	err := filepath.WalkDir("../../examples", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".pego") {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no example grammars")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			checkFormat(t, string(src))
		})
	}
}

// Formatting a grammar without layout information (for example, one
// decoded from JSON) is stable too.
func TestFormatWithoutLayout(t *testing.T) {
	g, err := Parse(calc)
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
	out := grammar.Format(bare)
	if out2 := format(t, out); out2 != out {
		t.Errorf("not stable:\n%s\n---\n%s", out, out2)
	}
	if !strings.Contains(out, "type Op struct {\n    Left  Node\n    Op    OpType\n    Right Node\n}\n\ntype Node") {
		t.Errorf("got:\n%s", out)
	}
}
