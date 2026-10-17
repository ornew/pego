package syntax

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
	copy := g.Clone()
	stripSource(reflect.ValueOf(copy))
	data, err := grammar.MarshalJSON(copy)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFormatMinusBoundaries(t *testing.T) {
	for _, src := range []string{
		`rule main = - -"a"`,
		`rule main = - - -"a"`,
		`rule main = (- -"a" / "b") $$`,
		`rule main = (- #cut "a" "b" / "a" "c") $$`,
		`rule main = xs:(x:- -"a")* $$`,
		`rule main = "a" {- -1 == 1}`,
		"type P = { N int }\nrule main P = \"a\" => P{N: - -1}",
		"type P = { N int }\nrule main P = \"a\" => P{N: - - -1}",
	} {
		t.Run(src, func(t *testing.T) { checkFormat(t, src) })
	}
}

var formattedGrammarSink string

func BenchmarkFormatGrammar(b *testing.B) {
	for _, name := range []string{"../../examples/minilang/minilang.pego", "../../parsers/yaml/yaml.pego"} {
		b.Run(filepath.Base(name), func(b *testing.B) {
			data, err := os.ReadFile(name)
			if err != nil {
				b.Fatal(err)
			}
			g, err := Parse(string(data))
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				formattedGrammarSink = grammar.Format(g)
			}
		})
	}
}

func TestFormatSource(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{
			name: "package and leading comments",
			in: `// header

// about the package
package demo // trailing

// about A
type A terminal
rule a = "a"
`,
			want: `// header

// about the package
package demo // trailing

// about A
type A terminal
rule a = "a"
`,
		},
		{
			name: "blank lines collapse",
			in:   "rule a = x\n\nrule b = y\nrule c = z\n",
			want: "rule a = x\n\nrule b = y\nrule c = z\n",
		},
		{
			name: "struct fields",
			in: `type Op = { // op
    // the left operand

    Left  Node   // left
    Op    OpType // operator
    Right Node

    // end
}
type P = { X A; Y B } // pair
type E = {}
`,
			want: `type Op = { // op
    // the left operand

    Left  Node   // left
    Op    OpType // operator
    Right Node

    // end
}
type P = { X A; Y B } // pair
type E = {}
`,
		},
		{
			name: "rule body lines",
			in: `rule expr Node = (
    // capture the operands
    l:term rest:(op:binop r:term)*
    // fold them
) => foldl($l, $rest, (acc, i) => i.r) // done
`,
			want: `rule expr Node = (
    // capture the operands
    l:term rest:(op:binop r:term)*
    // fold them
) => foldl($l, $rest, (acc, i) => i.r) // done
`,
		},
		{
			name: "multi-line choice",
			in: `rule value = ( // first
    object
    / array b
      c // continued
    // before the last
    / "null"
)
`,
			want: `rule value = ( // first
    object
    / array b
      c // continued
    // before the last
    / "null"
)
`,
		},
		{
			name: "attribute on its own line",
			in: `rule stmt = (
    s:(a / b)
    // skip to the next statement
    #recover(skip=[^;]+ ";"?)
) => $s
`,
			want: `rule stmt = (
    s:(a / b)
    // skip to the next statement
    #recover(skip=[^;]+ ";"?)
) => $s
`,
		},
		{
			name: "operatorsRule",
			in: `rule e = operators { // operators
    skip ws
    // operands
    operand num                      // number
    level { infix left "+" => $lhs } // additive
    level postfix {
        // calls
        postfix "(" ")" // call
        // end of level
    }
    // end of operatorsRule
}
`,
			want: `rule e = operators { // operators
    skip ws
    // operands
    operand num                      // number
    level { infix left "+" => $lhs } // additive
    level postfix {
        // calls
        postfix "(" ")" // call
        // end of level
    }
    // end of operatorsRule
}
`,
		},
		{
			name: "comments in joined lines",
			in: `// inside
rule a = x (y z) // after
// one
rule b = p / q // two
`,
			want: `// inside
rule a = x (y z) // after
// one
rule b = p / q // two
`,
		},
		{
			name: "blank line ends field alignment",
			in:   "type T = {\n    A X\n\n    Long Y\n    B    Z\n}\n",
			want: "type T = {\n    A X\n\n    Long Y\n    B    Z\n}\n",
		},
		{
			name: "operatorsRule body on its own line",
			in:   "rule e = operators {\n    operand x\n    level { prefix \"-\" }\n}\n",
			want: "rule e = operators {\n    operand x\n    level { prefix \"-\" }\n}\n",
		},
		{
			name: "end of file",
			in:   "rule a = x\n// last\n\n// very last\n",
			want: "rule a = x\n// last\n\n// very last\n",
		},
		{
			name: "only comments",
			in:   "// nothing here\n",
			want: "// nothing here\n",
		},
		{
			name: "crlf",
			in:   "rule a = x // c\n\nrule b = y\n",
			want: "rule a = x // c\n\nrule b = y\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// Normalized owned comments are serialized; formatter trivia is not.
func TestCommentsAndSourceInfoInJSON(t *testing.T) {
	g, err := Parse("// file\npackage p // pkg\nrule main = \"x\" // result\n")
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"leadingComments": " file\n"`) || !strings.Contains(string(data), `"trailingComments": " result\n"`) {
		t.Fatalf("owned comments absent: %s", data)
	}
	if strings.Contains(string(data), "// file") || strings.Contains(string(data), "PackageBreak") {
		t.Fatalf("formatter trivia serialized: %s", data)
	}
	decoded, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.PackageSource.LeadingComments != " file\n" || decoded.Rules()[0].TrailingComments != " result\n" {
		t.Fatal("owned comments lost")
	}
}

// Source coordinates and documentation do not change executable AST structure.
func stripSource(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	if v.Type() == reflect.TypeFor[grammar.SourceInfo]() {
		v.SetZero()
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			stripSource(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			stripSource(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			stripSource(v.Index(i))
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			stripSource(iter.Value())
		}
	}
}

// Formatting every example grammar keeps its grammar and comments.
func TestFormatExamples(t *testing.T) {
	var paths []string
	for _, root := range []string{"../../examples", "../../parsers"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".pego") {
				paths = append(paths, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
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
	if !strings.Contains(out, "type Op = {\n    Left  Node\n    Op    OpType\n    Right Node\n}\n\ntype Node") {
		t.Errorf("got:\n%s", out)
	}
}
