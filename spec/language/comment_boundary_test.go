package language_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

const commentOwnerExample = "// orphan\n\n// foo\npackage foo // trailing\n// trailing\n\n// B2\n// B2\n\n// B1\n// B1\nrule B = \"b\" // trailing\n// trailing\n"

// The boundary grammar is an executable design fixture: ownership is produced
// by grammar recognition and construction actions, without semantic analysis.
func TestSourceCommentBoundaryGrammar(t *testing.T) {
	src, err := os.ReadFile("testdata/comment-boundaries.pego")
	if err != nil {
		t.Fatal(err)
	}
	parsers := map[string]*pego.Parser{}
	for _, rule := range []string{"start", "after", "owner_example"} {
		p, err := pego.CompileSource(string(src), rule)
		if err != nil {
			t.Fatal(err)
		}
		parsers[rule] = p
	}
	cases := []struct {
		name, rule, input, trailing, leading string
		detached                             []string
	}{
		{"empty start", "start", "next", "", "", nil},
		{"file paragraphs", "start", "// orphan\n\n// foo\nnext", "", " foo\n", []string{" orphan\n"}},
		{"comment-only blank line", "start", "// first\n//\n// third\nnext", "", " first\n\n third\n", nil},
		{"inline line flushed", "after", " // trailing\n// next\nnext", " trailing\n", " next\n", nil},
		{"package to rule", "after", " // trailing\n// trailing\n\n// B2\n// B2\n\n// B1\n// B1\nnext", " trailing\n", " B1\n B1\n", []string{" trailing\n", " B2\n B2\n"}},
		{"baz to moo", "after", "\n// baz\n// another baz\n\n// moo\n//\n// another moo\nnext", " baz\n another baz\n", " moo\n\n another moo\n", nil},
		{"corge to grault", "after", "\n/* Block attached\n * to corge. */\n/* Block attached to\n * grault. */\nnext", " Block attached\n to corge. ", " Block attached to\n grault. ", nil},
		{"standalone leading", "after", "\n// next\nnext", "", " next\n", nil},
		{"standalone block leading", "after", "\n/* next */ next", "", " next ", nil},
		{"blank disables trailing", "after", "\n\n// detached\n\n// next\nnext", "", " next\n", []string{" detached\n"}},
		{"block switches to line", "after", "\n/* previous */\n// next\nnext", " previous ", " next\n", nil},
		{"line switches to block", "after", "\n// previous\n/* next */\nnext", " previous\n", " next ", nil},
		{"inline block", "after", " /* previous */\n// next\nnext", " previous ", " next\n", nil},
		{"ambiguous block", "after", " /* detached */ next", "", "", []string{" detached "}},
		{"ambiguous multiline block", "after", " /* first\n * second */ next", "", "", []string{" first\n second "}},
		{"empty ambiguous block", "after", " /**/ next", "", "", []string{""}},
		{"two inline blocks", "after", " /* previous */ /* next */ next", " previous ", " next ", nil},
		{"file-start inline block", "start", "/* next */ next", "", " next ", nil},
		{"empty group still detached", "start", "/**/\n\n// next\nnext", "", " next\n", []string{""}},
		{"block groups stay separate", "start", "/* first */\n/* second */\n// next\nnext", "", " next\n", []string{" first ", " second "}},
		{"nested block stays whole", "start", "/* outer /* nested */ tail */\nnext", "", " outer /* nested */ tail ", nil},
		{"nested block normalization", "start", "/* outer\n * /* nested\n * content */ tail\n * end */\nnext", "", " outer\n /* nested\n content */ tail\n end ", nil},
		{"nested closing on own line", "start", "/* outer\n * /* nested\n * content\n */ tail\n * end */\nnext", "", " outer\n /* nested\n content\n*/ tail\n end ", nil},
		{"EOF standalone trailing", "after", "\n// previous\n", " previous\n", "", nil},
		{"EOF inline continuation unowned", "after", " // previous\n// unowned\n", " previous\n", "", []string{" unowned\n"}},
		{"EOF detached unowned", "after", "\n\n// unowned\n", "", "", []string{" unowned\n"}},
		{"closing standalone trailing", "after", "\n// previous\n}", " previous\n", "", nil},
		{"closing inline continuation unowned", "after", " // previous\n// unowned\n}", " previous\n", "", []string{" unowned\n"}},
		{"closing ambiguous inline block", "after", " /* unowned */ }", "", "", []string{" unowned "}},
		{"closing inline block on prior line", "after", " /* previous */\n}", " previous ", "", nil},
		{"EOF inline block", "after", " /* previous */", " previous ", "", nil},
		{"CRLF", "start", "// first\r\n// second\r\nnext", "", " first\r\n second\r\n", nil},
		{"CR", "start", "// first\r// second\rnext", "", " first\r second\r", nil},
		{"no final newline", "after", " // previous", " previous", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					n, err := parsers[tc.rule].Parse(tc.input, pego.WithUnit(unit), pego.WithBackend(backend))
					if err != nil {
						t.Fatalf("%v/%v: %v", unit, backend, err)
					}
					if n.Field("Trailing") != tc.trailing || n.Field("Leading") != tc.leading {
						t.Fatalf("%v/%v: trailing=%q leading=%q, want %q and %q", unit, backend, n.Field("Trailing"), n.Field("Leading"), tc.trailing, tc.leading)
					}
					if n.Field("HasNext") != strings.HasSuffix(tc.input, "next") {
						t.Fatalf("%v/%v: incorrect next-owner presence: %v", unit, backend, n.Field("HasNext"))
					}
					var got []string
					for _, c := range elems(sourceField(t, n, "Detached")) {
						got = append(got, c.Field("Text").(string))
					}
					if !reflect.DeepEqual(got, tc.detached) {
						t.Fatalf("%v/%v: detached=%q, want %q", unit, backend, got, tc.detached)
					}
				}
			}
		})
	}
	t.Run("declaration owners", func(t *testing.T) {
		n, err := parsers["owner_example"].Parse(commentOwnerExample)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []struct {
			field, name, leading, trailing string
			detached                       []string
		}{
			{"Package", "foo", " foo\n", " trailing\n", []string{" orphan\n"}},
			{"Rule", "B", " B1\n B1\n", " trailing\n", []string{" trailing\n", " B2\n B2\n"}},
		} {
			element := sourceField(t, n, want.field)
			comments := sourceField(t, element, "Comments")
			var detached []string
			for _, c := range elems(sourceField(t, comments, "Detached")) {
				detached = append(detached, c.Field("Text").(string))
			}
			if element.Field("Name") != want.name || comments.Field("Leading") != want.leading || comments.Field("Trailing") != want.trailing || !reflect.DeepEqual(detached, want.detached) {
				t.Fatalf("unexpected %s owner: name=%v leading=%q trailing=%q detached=%q", want.field, element.Field("Name"), comments.Field("Leading"), comments.Field("Trailing"), detached)
			}
		}
	})
}

func TestSourceCommentBoundaryGeneratedGo(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the generated comment-owner consumer")
	}
	src, err := os.ReadFile("testdata/comment-boundaries.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := pego.ParseGrammar(string(src))
	if err != nil {
		t.Fatal(err)
	}
	code, err := pego.GenerateGo(g, "comments", "owner_example", pego.WithTypes())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"go.mod":     []byte("module comments\n\ngo 1.27.1\n"),
		"input.pego": []byte(commentOwnerExample),
		"parser_test.go": []byte(`package comments
import("os";"testing")
func TestOwners(t *testing.T){
 src,err:=os.ReadFile("input.pego");if err!=nil{t.Fatal(err)}
 ast,err:=ParseAST(string(src));if err!=nil{t.Fatal(err)}
 if ast.Package.Comments.Leading!=" foo\n"||ast.Package.Comments.Trailing!=" trailing\n"{t.Fatal("package ownership")}
 if ast.Rule.Comments.Leading!=" B1\n B1\n"||ast.Rule.Comments.Trailing!=" trailing\n"{t.Fatal("rule ownership")}
 ds:=ast.Rule.Comments.Detached
 if len(ds)!=2||ds[0].Text!=" trailing\n"||ds[1].Text!=" B2\n B2\n"{t.Fatal("detached ownership")}
}
`),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range code {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=3m", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated comment consumer: %v\n%s", err, out)
	}
}
