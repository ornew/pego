package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestInitRunsForEverySelectedStart(t *testing.T) {
	prog := compile(t, "init { n := 1; next := n + 1 }\nrule first = {next == 2} \"a\"\nrule second = {n := n + 1} {n == 2} \"b\"")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for i := 0; i < 2; i++ {
			if _, err := prog.ParseWith("first", "a", ParseOptions{Backend: backend}); err != nil {
				t.Fatalf("backend %v first parse %d: %v", backend, i, err)
			}
			if _, err := prog.ParseWith("second", "b", ParseOptions{Backend: backend}); err != nil {
				t.Fatalf("backend %v second parse %d: %v", backend, i, err)
			}
		}
		if _, err := prog.ParseWith("first", "a", ParseOptions{Backend: backend, Recognize: true}); err != nil {
			t.Fatalf("backend %v recognition: %v", backend, err)
		}
		doc, err := prog.NewDocumentWith("first", "a", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := doc.Parse(); err != nil {
				t.Fatalf("backend %v document parse %d: %v", backend, i, err)
			}
		}
	}
}

func TestInitRunsBeforeStream(t *testing.T) {
	prog := compile(t, "init { n := 1 }\nrule main = (\"a\" {n == 1})* #stream $$")
	for i := 0; i < 2; i++ {
		count := 0
		err := prog.ParseStream("main", strings.NewReader("aa"), func(*Node) error { count++; return nil })
		if err != nil || count != 2 {
			t.Fatalf("stream parse %d: count %d, error %v", i, count, err)
		}
	}
}

func TestInitFailureLeavesDocumentReusable(t *testing.T) {
	prog := compile(t, "init { n := 1 / 0 }\nrule main = \"a\"")
	doc, err := prog.NewDocument("main", "a")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := doc.Parse(); err == nil || !strings.Contains(err.Error(), "action in init") {
			t.Fatalf("document parse %d: %v", i, err)
		}
	}
}

func TestInitSavedGrammar(t *testing.T) {
	prog := compile(t, "init { n := 1; next := n + 1 }\nrule main = {next == 2} \"a\"")
	for _, omitAST := range []bool{false, true} {
		data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: omitAST})
		if err != nil {
			t.Fatal(err)
		}
		if got := data[len(compiledMagic)]; got != 4 {
			t.Fatalf("initializer file version = %d, want 4", got)
		}
		loaded, start, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for i := 0; i < 2; i++ {
				if _, err := loaded.ParseWith(start, "a", ParseOptions{Backend: backend}); err != nil {
					t.Fatalf("omit AST %v, backend %v, parse %d: %v", omitAST, backend, i, err)
				}
			}
		}
	}
}

func TestInitGeneratedSources(t *testing.T) {
	g, err := syntax.Parse("init { n := 1; next := n + 1 }\nrule main = {next == 2} \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	goSource, err := Generate(g, GenOptions{Start: "main", Package: "initparser", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goSource), "initActions =") {
		t.Fatal("generated Go initializer missing")
	}
	tsSource, err := GenerateTS(g, GenOptions{Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tsSource), "initActions =") {
		t.Fatal("generated TypeScript initializer missing")
	}
}

func TestInitAndWholeOperatorsActionGeneratedParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	cases := []genCase{
		{"initializer", "init { n := 1; next := n + 1 }\nrule main = {next == 2} \"a\"", []string{"a", "b"}},
		{"whole Operators Rule action", `type Wrap struct { Value node }
rule main Wrap = operators {
    operand "a"
    level { infix left "+" }
} => Wrap{Value: $1}`, []string{"a", "a+a", "a+"}},
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, typed := range []bool{false, true} {
		name := "untyped"
		if typed {
			name = "typed"
		}
		if _, err := os.Stat(goBin); err == nil {
			t.Run("Go/"+name, func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{Types: typed}) })
		}
		if node, err := exec.LookPath("node"); err == nil {
			t.Run("TypeScript/"+name, func(t *testing.T) { testGeneratedTSParsersCorpus(t, node, cases, GenOptions{Types: typed}) })
		}
	}
}

func TestInitValidation(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"init { n := 1 }\ninit { n := 2 }\nrule a = \"a\"", "only one init declaration"},
		{"init { 1 }\nrule a = \"a\"", "init statements must be assignments"},
		{"init { n := later; later := 1 }\nrule a = \"a\"", "not initialized before use"},
		{"init { n := $1 }\nrule a = \"a\"", "cannot be used in init assignments"},
	} {
		if err := compileError(t, tc.src); !strings.Contains(err, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.src, err, tc.want)
		}
	}
}
