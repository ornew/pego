package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// ignoreCaseGenGrammar has case-insensitive literals in a choice, whose alternatives the generated
// parsers dispatch on their first character, and in a sequence.
const ignoreCaseGenGrammar = `rule main = ("select"i / "from"i / "Sel"i "ect"i) " " "xy"i`

// ignoreCaseGenInputs are parsed by every backend. They include a non-ASCII letter and the Kelvin
// sign, which the folding must not match.
var ignoreCaseGenInputs = []string{
	"SELECT xY", "from XY", "selEct xy", "SEL ect xy", "select", "fRoM xy", "SELECT xz",
	"SELECX xy", "selecté xy", "SeLeCt xy", "KEL", "", "Sel ect XY", "FROM xy",
}

// ignoreCaseLines is the expected output of a program that parses each input and prints "ok" with
// the consumed input, or "error".
func ignoreCaseLines(t *testing.T, src string, inputs []string) string {
	t.Helper()
	prog := compile(t, src)
	var b strings.Builder
	for _, in := range inputs {
		n, err := prog.ParseWith("main", in, ParseOptions{Backend: Closure})
		if err != nil {
			b.WriteString("error\n")
			continue
		}
		r := []rune(in)
		fmt.Fprintf(&b, "ok %s\n", string(r[n.Start():n.End()]))
	}
	return b.String()
}

// runGeneratedUntypedProgram generates untyped Go for src as package p, runs the program body with
// it, and compares its output with want. It is the untyped form of runGeneratedNamedNodeProgram.
func runGeneratedUntypedProgram(t *testing.T, src, body, want string) {
	t.Helper()
	runGeneratedProgramWith(t, src, GenOptions{Package: "p", Start: "main"}, body, want)
}

// runGeneratedProgramWith is runGeneratedUntypedProgram with the generation options opts, which
// must name package p and start rule main.
func runGeneratedProgramWith(t *testing.T, src string, opts GenOptions, body, want string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":      testGoMod("gentest"),
		"p/parser.go": string(code),
		"main.go":     "package main\n\nimport (\n\t\"fmt\"\n\n\t\"gentest/p\"\n)\n\n" + body,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

// ignoreCaseGoBody parses the inputs with the generated untyped parser and prints the result as
// ignoreCaseLines does.
func ignoreCaseGoBody(inputs []string) string {
	var list strings.Builder
	for _, in := range inputs {
		fmt.Fprintf(&list, "\t\t%s,\n", strconv.Quote(in))
	}
	return "func main() {\n\tfor _, s := range []string{\n" + list.String() + "\t} {\n" +
		"\t\tn, err := p.Parse(s)\n\t\tif err != nil {\n\t\t\tfmt.Println(\"error\")\n\t\t\tcontinue\n\t\t}\n" +
		"\t\tr := []rune(s)\n\t\tfmt.Printf(\"ok %s\\n\", string(r[n.Start():n.End()]))\n\t}\n}\n"
}

// TestIgnoreCaseGeneratedGo checks that the generated Go parser agrees with the closure backend on
// case-insensitive literals.
func TestIgnoreCaseGeneratedGo(t *testing.T) {
	want := ignoreCaseLines(t, ignoreCaseGenGrammar, ignoreCaseGenInputs)
	runGeneratedUntypedProgram(t, ignoreCaseGenGrammar, ignoreCaseGoBody(ignoreCaseGenInputs), want)
}

// TestIgnoreCaseGeneratedTS checks that the generated TypeScript parser agrees with the closure
// backend on case-insensitive literals.
func TestIgnoreCaseGeneratedTS(t *testing.T) {
	want := ignoreCaseLines(t, ignoreCaseGenGrammar, ignoreCaseGenInputs)
	var list strings.Builder
	for _, in := range ignoreCaseGenInputs {
		fmt.Fprintf(&list, "  %s,\n", strconv.Quote(in))
	}
	script := "import { parse } from \"./parser.ts\";\nfor (const s of [\n" + list.String() + "]) {\n" +
		"  const r = parse(s);\n  if (r.error !== null) {\n    console.log(\"error\");\n    continue;\n  }\n" +
		"  console.log(\"ok \" + Array.from(s).slice(r.node.start, r.node.end).join(\"\"));\n}\n"
	if got := runTSScript(t, ignoreCaseGenGrammar, script); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestIgnoreCaseGeneratedTypedGo checks that the typed Go parser accepts exactly the inputs that the
// closure backend accepts, for case-insensitive literals in a choice and in a sequence.
func TestIgnoreCaseGeneratedTypedGo(t *testing.T) {
	prog := compile(t, ignoreCaseGenGrammar)
	var want strings.Builder
	for _, in := range ignoreCaseGenInputs {
		if _, err := prog.ParseWith("main", in, ParseOptions{Backend: Closure}); err != nil {
			want.WriteString("error\n")
		} else {
			want.WriteString("ok\n")
		}
	}
	var list strings.Builder
	for _, in := range ignoreCaseGenInputs {
		fmt.Fprintf(&list, "\t\t%s,\n", strconv.Quote(in))
	}
	body := "func main() {\n\tfor _, s := range []string{\n" + list.String() + "\t} {\n" +
		"\t\t_, err := p.ParseAST(s)\n\t\tif err != nil {\n\t\t\tfmt.Println(\"error\")\n\t\t\tcontinue\n\t\t}\n" +
		"\t\tfmt.Println(\"ok\")\n\t}\n}\n"
	runGeneratedNamedNodeProgram(t, ignoreCaseGenGrammar, body, want.String())
}

// TestIgnoreCaseGeneratedExpectation checks that the generated Go parser reports the same error as
// the closure backend, whose expectation of a case-insensitive literal keeps its suffix.
func TestIgnoreCaseGeneratedExpectation(t *testing.T) {
	_, err := compile(t, ignoreCaseGenGrammar).ParseWith("main", "x", ParseOptions{Backend: Closure})
	if err == nil {
		t.Fatal("the closure backend parsed x")
	}
	body := "func main() {\n\t_, err := p.Parse(\"x\")\n\tfmt.Println(err)\n}\n"
	runGeneratedUntypedProgram(t, ignoreCaseGenGrammar, body, err.Error()+"\n")
}

// ignoreCaseDocEdit is one edit replacing runes [start, end) of the document text with text.
type ignoreCaseDocEdit struct {
	start, end int
	text       string
}

// ignoreCaseDocInput and ignoreCaseDocEdits drive the generated Document. The edits move the text
// between accepted and rejected folded spellings, and they reparse the document after each one.
const ignoreCaseDocInput = "SELECT xY"

var ignoreCaseDocEdits = []ignoreCaseDocEdit{
	{0, 6, "from"}, {4, 5, ""}, {4, 4, " "}, {5, 6, "z"}, {5, 6, "X"}, {0, 4, "Sel"}, {3, 3, "ect"},
}

// ignoreCaseDocWant replays the edits on the closure backend, parsing the whole text after the
// initial parse and after each edit.
func ignoreCaseDocWant(t *testing.T) string {
	t.Helper()
	prog := compile(t, ignoreCaseGenGrammar)
	var b strings.Builder
	show := func(text []rune) {
		n, err := prog.ParseWith("main", string(text), ParseOptions{Backend: Closure})
		if err != nil {
			b.WriteString("error\n")
			return
		}
		fmt.Fprintf(&b, "ok %s\n", string(text[n.Start():n.End()]))
	}
	text := []rune(ignoreCaseDocInput)
	show(text)
	for _, e := range ignoreCaseDocEdits {
		next := append([]rune{}, text[:e.start]...)
		next = append(next, []rune(e.text)...)
		text = append(next, text[e.end:]...)
		show(text)
	}
	return b.String()
}

// ignoreCaseDocBody runs the same initial parse and edits on the generated Document and prints
// the result of each parse as ignoreCaseDocWant does.
func ignoreCaseDocBody() string {
	var edits strings.Builder
	for _, e := range ignoreCaseDocEdits {
		fmt.Fprintf(&edits, "\t\t{%d, %d, %s},\n", e.start, e.end, strconv.Quote(e.text))
	}
	return "func main() {\n\td, err := p.NewDocument(" + strconv.Quote(ignoreCaseDocInput) + ")\n" +
		"\tif err != nil {\n\t\tpanic(err)\n\t}\n" +
		"\tshow := func() {\n\t\tn, err := d.Parse()\n\t\tif err != nil {\n\t\t\tfmt.Println(\"error\")\n\t\t\treturn\n\t\t}\n" +
		"\t\tr := []rune(d.Text())\n\t\tfmt.Printf(\"ok %s\\n\", string(r[n.Start():n.End()]))\n\t}\n" +
		"\tshow()\n\tfor _, e := range []struct {\n\t\tstart, end int\n\t\ttext       string\n\t}{\n" + edits.String() +
		"\t} {\n\t\tif err := d.Edit(e.start, e.end, e.text); err != nil {\n\t\t\tpanic(err)\n\t\t}\n\t\tshow()\n\t}\n}\n"
}

// TestIgnoreCaseGeneratedDocument checks that a generated Document, which reparses after each edit
// of folded literals, agrees with the closure backend after every edit.
func TestIgnoreCaseGeneratedDocument(t *testing.T) {
	needsWorkControls(t)
	want := ignoreCaseDocWant(t)
	runGeneratedProgramWith(t, ignoreCaseGenGrammar, GenOptions{Package: "p", Start: "main", Document: true}, ignoreCaseDocBody(), want)
}
