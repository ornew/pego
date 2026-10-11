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

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func negativeMaxima() []int {
	values := []int{-1, -2, -1 << 31}
	if strconv.IntSize == 64 {
		large := int64(-1) << 32
		values = append(values, int(large), -int(^uint(0)>>1)-1)
	}
	return values
}

func grammarWithNegativeMaximum(t testing.TB, src string, maximum int) *grammar.Grammar {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rules() {
		walkExpr(r.Expr, func(e grammar.Expr) {
			if rep, ok := e.(*grammar.Repeat); ok {
				rep.Max = maximum
			}
		})
	}
	return g
}

func TestNegativeRepetitionMaxima(t *testing.T) {
	for _, c := range []struct {
		name, src string
		op        Op
	}{
		{"ordinary", `def main = "é"* $$`, OpRepeat},
		{"class scan", `def main = @(?é)* $$`, OpScan},
		{"any scan", `def main = @.* $$`, OpScan},
		{"projection", `def main = xs:(v:"é")* $$ -> map($xs, (x) => $x.v)`, OpRepeat},
	} {
		for _, maximum := range negativeMaxima() {
			t.Run(fmt.Sprintf("%s/%d", c.name, maximum), func(t *testing.T) {
				g := grammarWithNegativeMaximum(t, c.src, maximum)
				before, err := grammar.MarshalJSON(g)
				if err != nil {
					t.Fatal(err)
				}
				for _, opts := range []Options{{}, {DisableMemo: true}, {noProjections: true}} {
					p, err := Compile(g, opts)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, in := range p.Module().Code {
						if in.Op == c.op {
							found = true
							max := in.B
							if c.op == OpScan {
								max = in.C
							}
							if max != -1 {
								t.Errorf("uncanonical maximum %d in %s", max, c.op)
							}
						}
					}
					if !found {
						t.Fatalf("expected %s lowering", c.op)
					}
					if err := validateModule(p.Module()); err != nil {
						t.Errorf("module: %v", err)
					}
					for _, marshal := range []MarshalOptions{{}, {OmitAST: true}} {
						data, err := p.MarshalBinaryWith("main", marshal)
						if err != nil {
							t.Fatal(err)
						}
						if data[len(compiledMagic)+1] != 3 {
							t.Fatal("negative maxima alone must not require wide instructions")
						}
						loaded, _, err := LoadProgram(data, Options{})
						if err != nil {
							t.Fatalf("load: %v", err)
						}
						if !marshal.OmitAST {
							ast, err := grammar.MarshalJSON(loaded.Grammar)
							if err != nil || string(ast) != string(before) {
								t.Fatalf("saved AST maximum changed: %v", err)
							}
						}
						for _, prog := range []*Program{p, loaded} {
							for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
								if marshal.OmitAST && prog == loaded && b == Closure {
									continue
								}
								for _, unit := range []Unit{CodePoints, Bytes} {
									for _, recognize := range []bool{false, true} {
										if marshal.OmitAST && prog == loaded && recognize {
											continue // Recognition without the AST remains a separate capability.
										}
										for _, input := range []string{"", "é", "ééé", "x", "éx"} {
											ref := ParseOptions{Unit: unit, Recognize: recognize}
											want, we := p.ParseWith("main", input, ref)
											ref.Backend = b
											got, ge := prog.ParseWith("main", input, ref)
											if resultJSON(got, ge) != resultJSON(want, we) {
												t.Errorf("%s/%s recognize=%v input=%q: got %s, want %s", b, unit, recognize, input, resultJSON(got, ge), resultJSON(want, we))
											}
										}
									}
								}
							}
						}
					}
				}
				after, err := grammar.MarshalJSON(g)
				if err != nil || string(after) != string(before) {
					t.Fatalf("compiler mutated caller AST: %v", err)
				}
			})
		}
	}
}

func TestNegativeMaximumWithWideMinimum(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("wide minimum needs 64-bit implementation")
	}
	for _, body := range []string{`"é"{2147483648,} $$`, `@(?é){2147483648,} $$`} {
		for _, maximum := range negativeMaxima() {
			g := grammarWithNegativeMaximum(t, "def main="+body, maximum)
			p, err := Compile(g, Options{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, in := range p.Module().Code {
				index := in.B
				if in.Op == OpScanWide {
					index = in.C
				} else if in.Op != OpRepeatWide {
					continue
				}
				found = true
				if p.Module().Exprs[index].integer() != -1 {
					t.Fatal("wide maximum is not canonical")
				}
			}
			if !found {
				t.Fatal("wide instruction missing")
			}
			for _, marshal := range []MarshalOptions{{}, {OmitAST: true}} {
				data, err := p.MarshalBinaryWith("main", marshal)
				if err != nil {
					t.Fatal(err)
				}
				loaded, _, err := LoadProgram(data, Options{})
				if err != nil {
					t.Fatal(err)
				}
				for _, b := range []Backend{Bytecode, BytecodeIterative} {
					for _, unit := range []Unit{CodePoints, Bytes} {
						ref := ParseOptions{Unit: unit}
						want, we := p.ParseWith("main", "éé", ref)
						ref.Backend = b
						got, ge := loaded.ParseWith("main", "éé", ref)
						if resultJSON(got, ge) != resultJSON(want, we) {
							t.Fatalf("wide %s/%s got %s, want %s", b, unit, resultJSON(got, ge), resultJSON(want, we))
						}
					}
				}
			}
		}
	}
}

func TestNegativeMaximumDocumentsAndStreams(t *testing.T) {
	// Force recording of this short repetition so edits exercise resumption.
	defer func(n int) { minRecorded = n }(minRecorded)
	minRecorded = 1
	for _, maximum := range negativeMaxima() {
		g := grammarWithNegativeMaximum(t, `def main = "é"* #stream $$`, maximum)
		p, err := Compile(g, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, marshal := range []MarshalOptions{{}, {OmitAST: true}} {
			data, err := p.MarshalBinaryWith("main", marshal)
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, prog := range []*Program{p, loaded} {
				for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
					if marshal.OmitAST && prog == loaded && b == Closure {
						continue
					}
					for _, unit := range []Unit{CodePoints, Bytes} {
						opts := ParseOptions{Backend: b, Unit: unit}
						var got []string
						err := prog.ParseStreamWith("main", strings.NewReader("ééé"), func(n *Node) error {
							got = append(got, resultJSON(n, nil))
							return nil
						}, opts)
						var want []string
						we := p.ParseStreamWith("main", strings.NewReader("ééé"), func(n *Node) error {
							want = append(want, resultJSON(n, nil))
							return nil
						}, ParseOptions{Unit: unit})
						if len(got) != 3 || strings.Join(got, "\n") != strings.Join(want, "\n") || fmt.Sprint(err) != fmt.Sprint(we) {
							t.Fatalf("max=%d stream %s/%s got %v, %v", maximum, b, unit, got, err)
						}
						d, err := prog.NewDocumentWith("main", strings.Repeat("é", 100), opts)
						if err != nil {
							t.Fatal(err)
						}
						for round := 0; round < 3; round++ {
							n, err := d.Parse()
							ref, re := p.ParseWith("main", d.Text(), ParseOptions{Unit: unit})
							if resultJSON(n, err) != resultJSON(ref, re) {
								t.Fatalf("max=%d Document %s/%s round %d: got %s want %s", maximum, b, unit, round, resultJSON(n, err), resultJSON(ref, re))
							}
							if round > 0 && d.resumed == 0 {
								t.Fatal("edited Document did not resume repetition elements")
							}
							at := 50
							if unit == Bytes {
								at *= len("é")
							}
							if err := d.Edit(at, at, "é"); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		}
	}
}

// Generated parsers retain negative maxima directly in their implementation-int
// loops. Check their accepted hand-built AST contract beside bytecode lowering.
func TestGeneratedNegativeMaximum(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	large := int64(-1) << 32
	maximum := int(large)
	if strconv.IntSize < 64 {
		maximum = -2
	}
	g := grammarWithNegativeMaximum(t, `def main: []Match = "é"* $$ -> $1`, maximum)
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, input := range []string{"", "é", "ééé", "x", "éx"} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			n, err := p.ParseWith("main", input, ParseOptions{Unit: unit})
			expected = append(expected, resultJSON(n, err))
		}
	}
	want := strings.Join(expected, "\n") + "\n"
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("Go typed=%v", typed), func(t *testing.T) {
			code, err := Generate(g, GenOptions{Package: "main", Start: "main", Types: typed})
			if err != nil {
				t.Fatal(err)
			}
			astCheck := ""
			if typed {
				astCheck = `ast, ae := ParseAST(input, WithUnit(unit)); if fmt.Sprint(ae) != fmt.Sprint(err) || err == nil && len(ast) != len([]rune(input)) { panic("typed negative-maximum result differs") }`
			}
			harness := `package main
import ("encoding/json";"fmt")
func main() {
 for _, input := range []string{"","é","ééé","x","éx"} {
  for _, unit := range []Unit{CodePoints,Bytes} {
   n, err := Parse(input,WithUnit(unit))
   ` + astCheck + `
   out := map[string]any{"node":n}
   if err != nil {out["err"]=err.Error()}
   data,_ := json.Marshal(out);fmt.Println(string(data))
  }
 }
}`
			dir := t.TempDir()
			for name, content := range map[string]string{"go.mod": "module negative\n\ngo 1.24\n", "parser.go": string(code), "main.go": harness} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != want {
				t.Fatalf("generated Go: %v\ngot %s\nwant %s", err, out, want)
			}
		})
	}
	t.Run("TypeScript", func(t *testing.T) {
		got := runTSGrammarScript(t, g, `import { parse, CodePoints, Bytes } from "./parser.ts";
for (const input of ["","é","ééé","x","éx"]) {
 for (const unit of [CodePoints, Bytes]) {
  const result=parse(input,unit);
  const out: Record<string,unknown>={};
  if(result.error) out["err"]=result.error.message;
  out["node"]=result.node;
  console.log(JSON.stringify(out));
 }
}`)
		if got != want {
			t.Fatalf("generated TS:\ngot %s\nwant %s", got, want)
		}
	})
}

// BenchmarkBytecodeLowering isolates construction of the module from an
// already analyzed grammar; source parsing, AST checking and VM setup are out.
func BenchmarkBytecodeLowering(b *testing.B) {
	for _, c := range []struct{ name, path string }{
		{"JSON", "../../parsers/json/json.pego"},
		{"Minilang", "../../examples/minilang/minilang.pego"},
	} {
		b.Run(c.name, func(b *testing.B) {
			source, err := os.ReadFile(c.path)
			if err != nil {
				b.Fatal(err)
			}
			g, err := syntax.Parse(string(source))
			if err != nil {
				b.Fatal(err)
			}
			p, err := Compile(g, Options{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				compiler := &bcompiler{prog: p, m: &Module{}, strs: map[string]int{}}
				compiler.compile()
			}
		})
	}
}
