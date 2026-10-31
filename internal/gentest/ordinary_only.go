package gentest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compiletest"
	"github.com/ornew/zeroparse/internal/gen"
)

const OrdinaryOnlyTyped = `type A = { Text string }
type S = { L E; R A }
type E ~ S | A
rule main E = v:x $$ => $v
rule x E = sum / a
rule sum S = l:y "+" r:a => S{L: $l, R: $r}
rule y E = x
rule a A = t:("a" / "é") #atomic => A{Text: text($t)}`

var OrdinaryOnlyTypedInputs = []string{
	"a", "a+a", "a+a+a", "a+", "+a", "", "é", "é+a+é", "a+a\xff", "\xffa", "a+ a", "aa",
}

// OrdinaryOnlyPackage generates the grammar src as the package pkg of the module dir, with opts.
func OrdinaryOnlyPackage(t *testing.T, dir, pkg, src string, opts gen.GenOptions, program string, inputs []string) {
	t.Helper()
	g, err := compiletest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	opts.Runtime = gen.RuntimeImport
	opts.Package, opts.Module, opts.Start = pkg, "ordtest/"+pkg, "main"
	files, err := Generate(t, g, opts)
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

// RunTranscripts runs the transcript program of every package of dir and returns what each printed.
func RunTranscripts(t *testing.T, dir string, pkgs ...string) map[string]string {
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

func FirstTranscriptDifference(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return la[i] + "\n" + lb[i]
		}
	}
	return "lengths differ"
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

// OrdinaryOnlyTypedEntries prints only the typed entry points, which a typed-only parser has.
const OrdinaryOnlyTypedEntries = `package PKG

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			w, err = ParseRuleAST("a", in, WithUnit(unit))
			fmt.Fprintf(&b, "rule a %q %v: %s\n", in, unit, showAST(w, err))
		}
		v, err := ParseAST(in, WithMaxDepth(5))
		fmt.Fprintf(&b, "depth %q: %s\n", in, showAST(v, err))
		_, err = ParseRuleAST("nope", in)
		fmt.Fprintf(&b, "unknown rule: %s\n", errText(err))
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("TRANSCRIPT_DIR"), "PKG.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`
