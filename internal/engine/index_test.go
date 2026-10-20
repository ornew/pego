package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func indexCases() []genCase {
	return []genCase{
		{"first and last", `rule main = xs:item+ $$ => list($xs[0], $xs[len($xs) - 1])
rule item = [a-z]`, []string{"abc", "a", "", "1"}},
		{"out of range", `rule main = xs:item* $$ => $xs[2]
rule item = [a-z]`, []string{"abc", "ab", ""}},
		{"negative", `rule main = xs:item* $$ => $xs[0 - 1]
rule item = [a-z]`, []string{"a", ""}},
		{"nil list", `rule main = xs:(item+)? $$ => list($xs)[0]
rule item = [a-z]`, []string{"a", ""}},
		{"nested", `rule main = xs:item+ $$ => list($xs, list($xs[0]))[1][0]
rule item = [a-z]`, []string{"ab", ""}},
		{"lambda parameter", `rule main = xs:item+ $$ => map(list($xs), (l) => l[0])
rule item = [a-z]`, []string{"ab", ""}},
		{"predicate", `rule main = xs:item+ ({text($xs[0]) == "a"} "!" / "?") $$
rule item = [a-z]`, []string{"ab!", "ab?", "ba!", "ba?", ""}},
		{"predicate error rollback", `rule main = xs:item+ ({$xs[5] == nil} "!" / "?") $$
rule item = [a-z]`, []string{"ab!", "ab?", ""}},
		{"dynamic index", `var i int
rule main = {i := 1} xs:item+ $$ => $xs[i]
rule item = [a-z]`, []string{"ab", "a", ""}},
	}
}

func TestIndexAcrossBackends(t *testing.T) {
	for _, fixture := range indexCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := compile(t, fixture.src)
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					for _, input := range fixture.inputs {
						baseline := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
						for _, backend := range []Backend{Bytecode, BytecodeIterative} {
							actual := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}))
							if actual != baseline {
								t.Fatalf("%s/%s recognize=%t input=%q: %s / %s", backend, unit, recognize, input, actual, baseline)
							}
						}
					}
				}
			}
		})
	}
}

func TestIndexValuesAndErrors(t *testing.T) {
	program := compile(t, `rule main = xs:item+ $$ => $xs[1]
rule item = [a-z]`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		node, err := program.ParseWith("main", "abc", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatalf("%s: %v", backend, err)
		}
		if node.text != "b" {
			t.Fatalf("%s: indexed %q", backend, node.text)
		}
		_, err = program.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err == nil || !strings.Contains(err.Error(), "index 1 out of range for 1 elements") {
			t.Fatalf("%s: %v", backend, err)
		}
	}
	unchecked := compile(t, `var i string
rule main = {i := "0"} xs:item+ $$ => $xs[i]
rule item = [a-z]`, Options{NoTypeCheck: true})
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		_, err := unchecked.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err == nil || !strings.Contains(err.Error(), "index must be int, got string") {
			t.Fatalf("%s: %v", backend, err)
		}
	}
}

func TestIndexTypeErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`rule main = xs:"a"+ => $xs["0"]`, "index must be int, got string"},
		{`rule main = x:"a" => $x[0]`, "cannot index node[string]"},
		{`rule main = "a" => (1)[0]`, "cannot index int"},
	} {
		g, err := syntax.Parse(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// A node indexes its children.
	for _, src := range []string{
		"rule main = x:item => $x[0]\nrule item = a:\"a\" b:\"b\" => list($a, $b)",
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if _, err := Compile(g, Options{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	// The element type of a list is the type of an indexed value.
	for _, tc := range []struct {
		field string
		ok    bool
	}{{"node[string]", true}, {"*node[string]", true}, {"int", false}} {
		g, err := syntax.Parse("type Out = { First " + tc.field + " }\nrule main = xs:\"a\"+ => Out{First: $xs[0]}")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); (err == nil) != tc.ok {
			t.Errorf("field %s: %v", tc.field, err)
		}
	}
}

// Elements and captures that hold values other than nodes read as those values, not as the
// carriers that hold them in the tree. Until the checker admits such action results, they are
// made without type checks.
func TestIndexValueElements(t *testing.T) {
	for _, tc := range []struct {
		src    string
		inputs map[string]int
	}{
		{"rule main = x:(n n) $$ => $x[0] + $x[1]\nrule n = [0-9] => 1", map[string]int{"12": 2}},
		{"rule main = xs:n+ $$ => foldl(0, $xs, (a, x) => a + x)\nrule n = [0-9] => 2", map[string]int{"123": 6}},
		{"rule main = x:p $$ => $x.$v + 1\nrule p = v:n \"!\"\nrule n = [0-9] => 1", map[string]int{"1!": 2}},
	} {
		program := compile(t, tc.src, Options{NoTypeCheck: true})
		for input, want := range tc.inputs {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				node, err := program.ParseWith("main", input, ParseOptions{Backend: backend})
				if err != nil {
					t.Fatalf("%s %q: %v\n%s", backend, input, err, tc.src)
				}
				if got := unboxNode(node); got != want {
					t.Fatalf("%s %q: got %v, want %d\n%s", backend, input, got, want, tc.src)
				}
			}
		}
	}
}

// Indexing is written directly after the indexed value; with a space before it, "[" begins a
// character class.
func TestIndexSyntax(t *testing.T) {
	g, err := syntax.Parse(`rule main = xs:item+ => list($xs, $xs)[1][0]
rule item = [a-z]`)
	if err != nil {
		t.Fatal(err)
	}
	r := g.Rules()[0]
	outer, ok := r.Action.(*grammar.Binary)
	if !ok || outer.Op != "[]" {
		t.Fatalf("action %T", r.Action)
	}
	if inner, ok := outer.L.(*grammar.Binary); !ok || inner.Op != "[]" {
		t.Fatalf("inner %T", outer.L)
	}
	if got := grammar.Format(g); !strings.Contains(got, "list($xs, $xs)[1][0]") {
		t.Fatalf("formatted:\n%s", got)
	}
	// Parentheses that the source does not keep are added where "[" would not index.
	for _, x := range []grammar.Term{&grammar.IntLit{Value: 1}, &grammar.StringLit{Value: "a"}, &grammar.New{Type: "T"}} {
		formatted := grammar.Format(&grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Any{}, Action: &grammar.Binary{Op: "[]", L: x, R: &grammar.IntLit{Value: 0}}}}}})
		if !strings.Contains(formatted, ")[0]") {
			t.Errorf("formatted %T index: %s", x, formatted)
		}
	}
	if _, err := syntax.Parse(`rule main = item[a-z]
rule item = "a"`); err == nil || !strings.Contains(err.Error(), "write a space before it") {
		t.Fatalf("adjacent character class: %v", err)
	}
	for _, src := range []string{
		"rule main = item [a-z]\nrule item = \"a\"",
		"rule main = (item) [a-z]\nrule item = \"a\"",
		"rule main = [a-z][0-9]",
		"rule main = \"a\"[0-9]",
	} {
		if _, err := syntax.Parse(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestIndexCompiledInstructionSet(t *testing.T) {
	src := `rule main = xs:item+ $$ => $xs[0]
rule item = [a-z]`
	program := compile(t, src)
	for _, omit := range []bool{false, true} {
		data, err := program.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
		if err != nil || data[len(compiledMagic)+1] != isaVersion {
			t.Fatalf("index ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			options := ParseOptions{Backend: backend}
			if got, want := resultJSON(loaded.ParseWith("main", "ab", options)), resultJSON(program.ParseWith("main", "ab", options)); got != want {
				t.Fatalf("loaded index: %s / %s", got, want)
			}
		}
		data[len(compiledMagic)+1] = isaVersion - 1
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("under-declared index ISA accepted: %v", err)
		}
	}
}

func TestIndexGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, indexCases(), GenOptions{})
	testGeneratedTypesCorpus(t, goBin, indexCases(), GenOptions{})
}

func TestIndexGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, indexCases(), GenOptions{})
}

// Generated parsers index the same with and without work limits, which select the
// work-accounting runtime.
func TestIndexGeneratedGoWork(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	g, err := syntax.Parse(`type Out = { First node[string] }
rule main = xs:item+ $$ => Out{First: $xs[1]}
rule item = [a-z]`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("indexwork")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, types := range []bool{false, true} {
		pkg := fmt.Sprintf("index_%v", types)
		code, err := generateCode(g, GenOptions{Package: pkg, Start: "main", Types: types})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pkg)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"parser.go": code, "index_test.go": []byte(strings.ReplaceAll(generatedGoIndexConsumer, "PACKAGE", pkg))} {
			if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated indexing: %v\n%s", err, out)
	}
}

const generatedGoIndexConsumer = `package PACKAGE
import("fmt";"strings";"testing")
func TestIndex(t *testing.T){
 for _,opts:=range [][]ParseOption{nil,{WithWorkLimits(WorkLimits{MaxSteps:1<<50})}}{
  n,err:=Parse("abc",opts...);if err!=nil || !strings.Contains(fmt.Sprintf("%+v",n),"b"){t.Fatal(n,err)}
  _,err=Parse("a",opts...);if err==nil || !strings.Contains(err.Error(),"index 1 out of range for 1 elements"){t.Fatal(err)}
 }
}
`
