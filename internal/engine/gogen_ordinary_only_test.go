package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// The parsers generated without work controls (the form most parsers have) and with typed-only entry
// points are checked against the parsers generated with every entry point and the work-controlled form:
// a program in each package prints what its entry points return for a set of inputs, and the prints
// must be the same. A mutated grammar is the negative control: its print must differ.

const ordinaryOnlyUntyped = `rule main = stmt* #stream $$
rule stmt = (e ";") #recover(skip=[^;]+ ";")
rule e = e "+" t / t
rule t = [0-9]+ #atomic / "(" e ")" / "é"`

const ordinaryOnlyTyped = `type A = { Text string }
type S = { L E; R A }
type E ~ S | A
rule main E = v:x $$ => $v
rule x E = sum / a
rule sum S = l:y "+" r:a => S{L: $l, R: $r}
rule y E = x
rule a A = t:("a" / "é") #atomic => A{Text: text($t)}`

var ordinaryOnlyUntypedInputs = []string{
	"1;", "1+2;3;", "(1+2)+3;", "é+1;", "1+;2;", "x;1;", "1+2", "", ";", "((((((1))))));",
	"\xff;", "1;\xc3", "é\xff+1;", "1+2;\n3+", "(1+(2+(3+(4+5))));",
}

var ordinaryOnlyTypedInputs = []string{
	"a", "a+a", "a+a+a", "a+", "+a", "", "é", "é+a+é", "a+a\xff", "\xffa", "a+ a", "aa",
}

const ordinaryOnlyShow = `
func show(n *Node, err error) string {
	s := "<nil>"
	if n != nil {
		s = n.String()
	}
	return s + " | " + errText(err)
}

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	e := err.Error()
	var se *SyntaxError
	if errors.As(err, &se) {
		e += fmt.Sprintf(" @%d:%d:%d", se.Line, se.Col, se.Pos)
	}
	return e
}

func writeTranscript(t *testing.T, b *strings.Builder) {
	if err := os.WriteFile(filepath.Join(os.Getenv("TRANSCRIPT_DIR"), "PKG.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`

// ordinaryOnlyUntypedProgram prints the Node entry points, the stream entry points and the Document.
const ordinaryOnlyUntypedProgram = `package PKG

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)
` + ordinaryOnlyShow + `
func TestTranscript(t *testing.T) {
	var b strings.Builder
	for _, in := range INPUTS {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			n, err := Parse(in, WithUnit(unit))
			fmt.Fprintf(&b, "parse %q %v: %s\n", in, unit, show(n, err))
			fmt.Fprintf(&b, "recognize %q %v: %s\n", in, unit, errText(Recognize(in, WithUnit(unit))))
			n, err = ParseRule("e", in, WithUnit(unit))
			fmt.Fprintf(&b, "rule e %q %v: %s\n", in, unit, show(n, err))
		}
		n, err := Parse(in, WithMaxDepth(4))
		fmt.Fprintf(&b, "depth %q: %s\n", in, show(n, err))
		var got []string
		err = ParseStream(strings.NewReader(in), func(n *Node) error { got = append(got, fmt.Sprint(n)); return nil })
		fmt.Fprintf(&b, "stream %q: %q %s\n", in, got, errText(err))
		got = nil
		it, err := Iterate(strings.NewReader(in))
		if err != nil {
			t.Fatal(err)
		}
		for {
			n, ok, err := it.Next()
			if err != nil || !ok {
				fmt.Fprintf(&b, "iterate %q: %q %s\n", in, got, errText(err))
				break
			}
			got = append(got, fmt.Sprint(n))
		}
		it.Close()
		d, err := NewDocument(in)
		if err != nil {
			t.Fatal(err)
		}
		n, err = d.Parse()
		fmt.Fprintf(&b, "document %q: %s\n", in, show(n, err))
		fmt.Fprintf(&b, "edit: %s\n", errText(d.Edit(0, 0, "1;")))
		n, err = d.Parse()
		fmt.Fprintf(&b, "document edited %q: %s\n", d.Text(), show(n, err))
	}
	writeTranscript(t, &b)
}
`

// ordinaryOnlyTypedProgram prints the typed entry points, the Node entry points and the typed Document.
const ordinaryOnlyTypedProgram = `package PKG

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ordtest/PKG/types"
)

var _ types.E
` + ordinaryOnlyShow + `
func showAST(v any, err error) string {
	j, jerr := json.Marshal(v)
	if jerr != nil {
		return "marshal: " + jerr.Error()
	}
	return string(j) + " | " + errText(err)
}

func TestTranscript(t *testing.T) {
	var b strings.Builder
	for _, in := range INPUTS {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			v, err := ParseAST(in, WithUnit(unit))
			fmt.Fprintf(&b, "ast %q %v: %s\n", in, unit, showAST(v, err))
			w, err := ParseRuleAST("sum", in, WithUnit(unit))
			fmt.Fprintf(&b, "rule sum %q %v: %s\n", in, unit, showAST(w, err))
		}
		v, err := ParseAST(in, WithMaxDepth(5))
		fmt.Fprintf(&b, "depth %q: %s\n", in, showAST(v, err))
		_, err = ParseRuleAST("nope", in)
		fmt.Fprintf(&b, "unknown rule: %s\n", errText(err))
NODE	}
	writeTranscript(t, &b)
}
`

const ordinaryOnlyTypedNode = `
		n, nerr := Parse(in)
		fmt.Fprintf(&b, "parse %q: %s\n", in, show(n, nerr))
		fmt.Fprintf(&b, "recognize %q: %s\n", in, errText(Recognize(in)))
		d, err := NewDocument(in)
		if err != nil {
			t.Fatal(err)
		}
		v, err = d.ParseAST()
		fmt.Fprintf(&b, "document %q: %s\n", in, showAST(v, err))
		fmt.Fprintf(&b, "edit: %s\n", errText(d.Edit(0, 0, "a+")))
		v, err = d.ParseAST()
		fmt.Fprintf(&b, "document edited %q: %s\n", d.Text(), showAST(v, err))
`

// ordinaryOnlyPackage generates the grammar src as the package pkg of the module dir, with opts.
func ordinaryOnlyPackage(t *testing.T, dir, pkg, src string, opts gen.GenOptions, program string, inputs []string) {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	opts.Runtime = gen.RuntimeImport
	opts.Package, opts.Module, opts.Start = pkg, "ordtest/"+pkg, "main"
	files, err := gentest.Generate(t, g, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		path := filepath.Join(dir, pkg, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := "[]string{"
	for _, in := range inputs {
		list += goQuote(in) + ", "
	}
	list += "}"
	program = strings.ReplaceAll(strings.ReplaceAll(program, "INPUTS", list), "PKG", pkg)
	if err := os.WriteFile(filepath.Join(dir, pkg, "transcript_test.go"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			b.WriteString("\\x" + string("0123456789abcdef"[c>>4]) + string("0123456789abcdef"[c&15]))
		}
	}
	b.WriteByte('"')
	return b.String()
}

// runTranscripts runs the transcript program of every package of dir and returns what each printed.
func runTranscripts(t *testing.T, dir string, pkgs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestTranscript", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TRANSCRIPT_DIR="+dir)
	if res, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, res)
	}
	for _, pkg := range pkgs {
		data, err := os.ReadFile(filepath.Join(dir, pkg+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		out[pkg] = string(data)
	}
	return out
}

func TestGeneratedOrdinaryOnlyMatchesControlled(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	cases := []struct {
		name, src, mutated, program string
		opts                        gen.GenOptions
		inputs                      []string
		want                        []string // lines the transcript must contain, so that it is not trivially equal
	}{
		{"untyped", ordinaryOnlyUntyped, strings.Replace(ordinaryOnlyUntyped, `"+"`, `"-"`, 1), ordinaryOnlyUntypedProgram,
			gen.GenOptions{Recognize: true, Document: true, Stream: true}, ordinaryOnlyUntypedInputs,
			[]string{"parse \"1+2;3;\" ", "syntax error", "stream \"1+2;3;\":", "iterate \"1;\":", "document edited", "depth"}},
		{"typed", ordinaryOnlyTyped, strings.Replace(ordinaryOnlyTyped, `"+"`, `"-"`, 1), strings.Replace(ordinaryOnlyTypedProgram, "NODE", ordinaryOnlyTypedNode, 1),
			gen.GenOptions{Types: true, Recognize: true, Document: true}, ordinaryOnlyTypedInputs,
			[]string{"ast \"a+a\"", "syntax error", "document edited", "rule sum", "unknown rule"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("ordtest")), 0o644); err != nil {
				t.Fatal(err)
			}
			controlled, ordinary := tc.opts, tc.opts
			controlled.WorkControls = true
			ordinaryOnlyPackage(t, dir, "controlled", tc.src, controlled, tc.program, tc.inputs)
			ordinaryOnlyPackage(t, dir, "ordinary", tc.src, ordinary, tc.program, tc.inputs)
			ordinaryOnlyPackage(t, dir, "mutated", tc.mutated, ordinary, tc.program, tc.inputs)
			out := runTranscripts(t, dir, "controlled", "ordinary", "mutated")
			if out["controlled"] != out["ordinary"] {
				t.Errorf("the ordinary-only parser differs from the controlled one:\n%s", firstTranscriptDifference(out["controlled"], out["ordinary"]))
			}
			for _, want := range tc.want {
				if !strings.Contains(out["ordinary"], want) {
					t.Errorf("the transcript has no %q", want)
				}
			}
			// The negative control: a parser of another grammar must be told apart.
			if out["mutated"] == out["ordinary"] {
				t.Error("the transcripts of a mutated grammar are the same: the comparison cannot fail")
			}
		})
	}
}

func firstTranscriptDifference(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return la[i] + "\n" + lb[i]
		}
	}
	return "lengths differ"
}
