package exects

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ir "github.com/ornew/zeroparse/internal/ir/exec"
	"github.com/ornew/zeroparse/internal/ir/exec/printtest"
)

var testTarget = &Target{
	Source: "printtest",
	Op: func(c *ir.Call, args []string) string {
		name := map[string]string{"log.note": "note", "log.flag": "flag", "box.new": "newBox", "box.get": "get"}[c.Callee]
		return "this." + name + "(" + strings.Join(args, ", ") + ")"
	},
	Reg:    func(name string) string { return "this." + name },
	Handle: func(string) string { return "Box | null | undefined" },
	Packed: "Box",
}

// program returns the TypeScript test program: a class with the operations and the printed
// methods, and code that runs each entry and prints its results as printtest.Want does.
func program(t *testing.T, m *ir.Module) string {
	var b strings.Builder
	b.WriteString(`declare const process: { stdout: { write(s: string): void } };

class Box {
  x: number;
  constructor(x: number) {
    this.x = x;
  }
}

const out: string[] = [];

class P {
  pos = 0;
  note(x: number): void {
    out.push("note " + String(x) + "\n");
  }
  flag(b: boolean): boolean {
    out.push("flag " + String(b) + "\n");
    return b;
  }
  newBox(x: number): Box {
    return new Box(x);
  }
  get(b: Box | null | undefined): number {
    return b!.x;
  }

`)
	for _, f := range m.Funcs {
		src, err := Method(m, f, testTarget)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(src + "\n")
	}
	b.WriteString("}\n\n")
	for _, f := range printtest.Entries(m) {
		fmt.Fprintf(&b, "{\n  const r = new P().%s();\n  out.push(%q + ", f.Name, f.Name)
		if len(f.Results) == 1 {
			b.WriteString(`" " + String(r)`)
		} else {
			b.WriteString(`r.map((x) => " " + String(x)).join("")`)
		}
		b.WriteString(" + \"\\n\");\n}\n")
	}
	b.WriteString("process.stdout.write(out.join(\"\"));\n")
	return b.String()
}

// TestPrintedFunctionsRun prints every function of the printer tests into a TypeScript program,
// type-checks it strictly where tsc is installed and runs it with Node, and compares its output
// with the interpreter's.
func TestPrintedFunctionsRun(t *testing.T) {
	if testing.Short() {
		t.Skip("runs Node")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for the TypeScript printer tests")
	}
	m := printtest.Build()
	src := program(t, m)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if tsc, err := exec.LookPath("tsc"); err == nil {
		cmd := exec.Command(tsc, "--noEmit", "--target", "es2020", "--lib", "es2020", "--strict",
			"--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--allowUnreachableCode", "false",
			"--allowUnusedLabels", "false", "--erasableSyntaxOnly", "main.ts")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("tsc: %v\n%s\n%s", err, out, numbered(src))
		}
	} else {
		t.Log("tsc not found: not type-checking the printed code")
	}
	// A printed loop that does not end as the interpreter's does must fail, not hang.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "main.ts")
	cmd.Dir = dir
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, numbered(src))
	}
	if want := printtest.Want(m); string(got) != want {
		t.Errorf("printed functions print\n%s\nthe interpreter\n%s\n%s", got, want, numbered(src))
	}
}

func numbered(src string) string {
	lines := strings.Split(src, "\n")
	for i := range lines {
		lines[i] = fmt.Sprintf("%4d %s", i+1, lines[i])
	}
	return strings.Join(lines, "\n")
}

// TestPrintedForms checks forms of printed statements that the program does not show.
func TestPrintedForms(t *testing.T) {
	m := printtest.Build()
	checks := map[string][]string{
		"exits":           {"  out: {\n", "      in_: {\n", "break in_;\n", "    outer:\n    for (;;) {\n", "continue outer;\n", "break outer;\n", "twin_2: {\n"},
		"find":            {"find(x: number): Box | null | undefined {\n", "return undefined;\n", "return this.newBox(x);\n"},
		"relay":           {"const v = this.find(x);\n", "      return v;\n", "return v !== null ? v : undefined;\n"},
		"pairs":           {"let v: Box | null | undefined = null;\n", "v = this.find(-1);\n", "if (v === undefined) {\n", "if (v !== undefined) {\n", "const w = this.find(2);\n"},
		"scopes":          {"    blk: {\n      const a = 10;\n", "sum = sum + c;\n      let written = 6;\n", "      void written;\n", "break blk;\n"},
		"clauses":         {"for (i = 0; i < 6; i++) {\n", "while (s < 20) {\n", "[this.pos, ] = this.pair(4);\n", "s = s + 100;\n    if (0 < s) {\n", "} else if (s === 0) {\n"},
		"special_x_extra": {"  // Printed from Exec IR function special_x_extra of printtest, specialized for extra.\n"},
		"hash":            {"Math.imul(", "n = n + 1 | 0;\n"},
	}
	for name, wants := range checks {
		s, err := Method(m, m.Func(name), testTarget)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wants {
			if !strings.Contains(s, w) {
				t.Errorf("%s lacks %q:\n%s", name, w, s)
			}
		}
	}
}

// TestOpStatements checks operations printed as statements of their own and as nothing.
func TestOpStatements(t *testing.T) {
	m := printtest.Build()
	tg := *testTarget
	tg.Op = func(c *ir.Call, args []string) string {
		if c.Callee == "log.note" {
			return ""
		}
		return testTarget.Op(c, args)
	}
	tg.Stmt = func(c *ir.Call, args []string, dst []string, decl string) (string, bool) {
		if c.Callee != "box.new" {
			return "", false
		}
		return fmt.Sprintf("%s %s = this.newBox(%s);\nthis.newBox(0);", decl, strings.Join(dst, ", "), args[0]), true
	}
	for name, want := range map[string]string{
		"handles": "    x = this.newBox(42);\n    this.newBox(0);\n    const y = this.newBox(42);\n    this.newBox(0);\n",
		"exits":   "      }\n      for (;;) {\n",
	} {
		s, err := Method(m, m.Func(name), &tg)
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
