package execgo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ir "github.com/ornew/zeroparse/internal/ir/exec"
	"github.com/ornew/zeroparse/internal/ir/exec/printtest"
)

// testTarget implements the operations of printtest on the type P of the test program.
var testTarget = &Target{
	Recv:   "p *P",
	Source: "printtest",
	Op: func(c *ir.Call, args []string) string {
		name := map[string]string{"log.note": "note", "log.flag": "flag", "box.new": "newBox", "box.get": "get"}[c.Callee]
		return "p." + name + "(" + strings.Join(args, ", ") + ")"
	},
	Reg:    func(name string) string { return "p." + strings.ToUpper(name[:1]) + name[1:] },
	Handle: func(string) string { return "*box" },
}

const testPrelude = `package main

import (
	"fmt"
	"os"
	"strings"
)

type box struct{ x int }

type P struct {
	Pos int
	out *strings.Builder
}

func (p *P) note(x int)         { fmt.Fprintf(p.out, "note %v\n", x) }
func (p *P) flag(b bool) bool   { fmt.Fprintf(p.out, "flag %v\n", b); return b }
func (p *P) newBox(x int) *box  { return &box{x} }
func (p *P) get(b *box) int     { return b.x }

`

// program returns the Go test program: the printed functions and a main that runs each entry and
// prints its results as printtest.Want does.
func program(t *testing.T, m *ir.Module) string {
	var b strings.Builder
	b.WriteString(testPrelude)
	for _, f := range m.Funcs {
		src, err := Func(m, f, testTarget)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(src + "\n")
	}
	b.WriteString("func main() {\n\tvar out strings.Builder\n")
	for _, f := range printtest.Entries(m) {
		rs := make([]string, len(f.Results))
		for i := range rs {
			rs[i] = fmt.Sprintf("r%d", i)
		}
		fmt.Fprintf(&b, "\t{\n\t\tp := &P{out: &out}\n\t\t%s := p.%s()\n\t\tout.WriteString(%q)\n", strings.Join(rs, ", "), f.Name, f.Name)
		for _, r := range rs {
			fmt.Fprintf(&b, "\t\tfmt.Fprintf(&out, \" %%v\", %s)\n", r)
		}
		b.WriteString("\t\tout.WriteString(\"\\n\")\n\t}\n")
	}
	b.WriteString("\tos.Stdout.WriteString(out.String())\n}\n")
	return b.String()
}

// TestPrintedFunctionsRun prints every function of the printer tests, builds them into a program,
// vets it and runs it, and compares its output with the interpreter's.
func TestPrintedFunctionsRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a Go program")
	}
	m := printtest.Build()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(program(t, m)), 0o644); err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-trimpath", "GO111MODULE=on")
	vet := exec.Command(goBin, "vet", "main.go")
	vet.Dir, vet.Env = dir, env
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("go vet: %v\n%s\n%s", err, out, numbered(program(t, m)))
	}
	// A printed loop that does not end as the interpreter's does must fail, not hang.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := exec.CommandContext(ctx, goBin, "run", "main.go")
	run.Dir, run.Env = dir, env
	out, err := run.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if want := printtest.Want(m); string(out) != want {
		t.Errorf("printed functions print\n%s\nthe interpreter\n%s", out, want)
	}
}

func numbered(src string) string {
	lines := strings.Split(src, "\n")
	for i := range lines {
		lines[i] = fmt.Sprintf("%4d %s", i+1, lines[i])
	}
	return strings.Join(lines, "\n")
}

// TestPrintedForms checks the forms of printed statements that the program does not show: labels
// after braced blocks, labeled loops only when a jump needs the label, increments, and the comment
// naming the source and the specialization.
func TestPrintedForms(t *testing.T) {
	m := printtest.Build()
	src := func(name string) string {
		s, err := Func(m, m.Func(name), testTarget)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	checks := map[string][]string{
		"exits":  {"goto in\n", "\nin:\n", "outer:\n\tfor {\n", "continue outer\n", "break outer\n", "twin_2:\n"},
		"scopes": {"\t{\n\t\ta := 10\n", "\t}\nblk:\n", "sum = sum + c\n\t\twritten := 6\n", "written := 6\n", "_ = written\n"},
		"clauses": {"for i = 0; i < 6; i++ {\n", "for s < 20 {\n", "p.Pos, _ = p.pair(4)\n", "if s = s + 100; 0 < s {\n",
			"} else if s == 0 {\n", "// a comment\n"},
		"special_x_extra": {"// Printed from Exec IR function special_x_extra of printtest, specialized for extra.\n"},
		"wrap":            {"i + int32(1)", "w&uint64(240) | uint64(15) ^ w&uint64(255)&uint64(60)"},
		"constants":       {"return int32(-2147483648), uint32(4294967295), uint64(18446744073709551614), -2147483648, uint32(4294967295), -1, false\n"},
	}
	for name, wants := range checks {
		s := src(name)
		for _, w := range wants {
			if !strings.Contains(s, w) {
				t.Errorf("%s lacks %q:\n%s", name, w, s)
			}
		}
	}
}

// TestOpStatements checks operations printed as statements of their own and as nothing, and an
// operation printed as an operator expression inside another expression.
func TestOpStatements(t *testing.T) {
	m := printtest.Build()
	tg := *testTarget
	tg.Op = func(c *ir.Call, args []string) string {
		switch c.Callee {
		case "log.note":
			return ""
		case "log.flag":
			return args[0] + " == true"
		}
		return testTarget.Op(c, args)
	}
	tg.Stmt = func(c *ir.Call, args []string, dst []string, define bool) (string, bool) {
		if c.Callee != "box.new" {
			return "", false
		}
		op := "="
		if define {
			op = ":="
		}
		return fmt.Sprintf("%s %s p.newBox(%s)\np.newBox(0)", strings.Join(dst, ", "), op, args[0]), true
	}
	for name, want := range map[string]string{
		"handles":      "\tx = p.newBox(42)\n\tp.newBox(0)\n\ty := p.newBox(42)\n\tp.newBox(0)\n",
		"exits":        "\t\tgoto in\n\t}\nin:\n\tfor {\n",
		"shortcircuit": "a := (false == true) && (true == true)\n",
	} {
		s, err := Func(m, m.Func(name), &tg)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s, want) {
			t.Errorf("%s lacks %q:\n%s", name, want, s)
		}
		if strings.Contains(s, "note") {
			t.Errorf("%s prints a note:\n%s", name, s)
		}
	}
}
