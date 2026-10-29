package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
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
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rules() {
		compile.WalkExpr(r.Expr, func(e grammar.Expr) {
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
		op        bytecode.Op
	}{
		{"ordinary", `rule main = "é"* $$`, bytecode.OpRepeat},
		{"class scan", `rule main = [é]* #atomic $$`, bytecode.OpScan},
		{"any scan", `rule main = .* #atomic $$`, bytecode.OpScan},
		{"projection", `rule main = xs:(v:"é")* $$ => map($xs, (x) => x.$v)`, bytecode.OpRepeat},
	} {
		for _, maximum := range negativeMaxima() {
			t.Run(fmt.Sprintf("%s/%d", c.name, maximum), func(t *testing.T) {
				g := grammarWithNegativeMaximum(t, c.src, maximum)
				before, err := grammar.MarshalJSON(g)
				if err != nil {
					t.Fatal(err)
				}
				for _, opts := range []compile.Options{{}, {DisableMemo: true}, {NoProjections: true}} {
					p, err := Compile(g, opts)
					if err != nil {
						t.Fatal(err)
					}
					// Saved ASTs use the compiled grammar, where source-only groups
					// are removed. The caller's AST is checked separately below.
					savedAST := executableASTJSON(t, p.Grammar)
					found := false
					for _, in := range p.Module().Code {
						if in.Op == c.op {
							found = true
							max := in.B
							if c.op == bytecode.OpScan {
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
					for _, in := range p.Module().Code {
						if in.Op == bytecode.OpRepeatWide || in.Op == bytecode.OpScanWide {
							t.Errorf("negative maxima alone must not lower to %s", in.Op)
						}
					}
					if err := compile.ValidateModule(p.Module()); err != nil {
						t.Errorf("module: %v", err)
					}
					if ast := executableASTJSON(t, p.Grammar); ast != savedAST {
						t.Fatal("compiled executable AST changed")
					}
					for _, mode := range allModes {
						data, err := p.Marshal("main", mode)
						if err != nil {
							t.Fatal(err)
						}
						if want := byte(bytecode.ISAVersion); data[isaOffset] != want {
							t.Fatalf("negative maxima alone must not require wide instructions: instruction set %d, want %d", data[isaOffset], want)
						}
						loaded, _, err := LoadProgram(data)
						if err != nil {
							t.Fatalf("load: %v", err)
						}
						for _, prog := range []*Program{p, loaded} {
							for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
								if prog == loaded && b == Closure {
									_, err := loaded.ParseWith("main", "", servedBy(mode, ParseOptions{Backend: b}))
									wantClosureRefused(t, err)
									continue
								}
								for _, unit := range []PositionUnit{CodePoints, Bytes} {
									for _, recognize := range []bool{false, true} {
										if prog == loaded && recognize != (mode == CompiledRecognize) {
											continue // A compiled grammar serves the parses of its mode only.
										}
										for _, input := range []string{"", "é", "ééé", "x", "éx"} {
											ref := ParseOptions{Unit: unit, Recognize: recognize}
											want, we := p.ParseWith("main", input, ref)
											ref.Backend = b
											if prog == loaded {
												ref = servedBy(mode, ref)
											}
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

// Compiled ASTs preserve executable structure, not grammar source metadata.
// Ignore only SourceInfo; all other serialized fields must still round-trip.
func executableASTJSON(t testing.TB, g *grammar.Grammar) string {
	t.Helper()
	copy := g.Clone()
	var strip func(reflect.Value)
	strip = func(v reflect.Value) {
		if v.Type() == reflect.TypeFor[grammar.SourceInfo]() {
			v.SetZero()
			return
		}
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				strip(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				strip(v.Field(i))
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				strip(v.Index(i))
			}
		case reflect.Map:
			for _, key := range v.MapKeys() {
				strip(v.MapIndex(key))
			}
		}
	}
	strip(reflect.ValueOf(copy))
	data, err := grammar.MarshalJSON(copy)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCompiledSourceMetadataOmitted(t *testing.T) {
	g, err := parseTest("// documentation\npackage p\n// rule documentation\nrule main = \"x\" #atomic")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range allModes {
		data, err := p.Marshal("main", mode)
		if err != nil {
			t.Fatal(err)
		}
		// A compiled grammar holds no grammar, so it archives no source metadata.
		if strings.Contains(string(data), "documentation") {
			t.Fatalf("%s mode file archives source metadata", mode)
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.SourceGrammar() != nil {
			t.Fatalf("%s mode file holds a grammar", mode)
		}
	}
}

func TestNegativeMaximumWithWideMinimum(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("wide minimum needs 64-bit implementation")
	}
	for _, body := range []string{`"é" #repeat(2147483648, ) $$`, `[é] #repeat(2147483648, ) #atomic $$`} {
		for _, maximum := range negativeMaxima() {
			g := grammarWithNegativeMaximum(t, "rule main = "+body, maximum)
			p, err := Compile(g, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, in := range p.Module().Code {
				index := in.B
				if in.Op == bytecode.OpScanWide {
					index = in.C
				} else if in.Op != bytecode.OpRepeatWide {
					continue
				}
				found = true
				if p.Module().Exprs[index].Integer() != -1 {
					t.Fatal("wide maximum is not canonical")
				}
			}
			if !found {
				t.Fatal("wide instruction missing")
			}
			for _, mode := range valueModes {
				loaded := saveLoad(t, p, "main", mode)
				for _, b := range []Backend{Bytecode, BytecodeIterative} {
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						ref := ParseOptions{Unit: unit}
						want, we := p.ParseWith("main", "éé", ref)
						ref.Backend = b
						got, ge := loaded.ParseWith("main", "éé", servedBy(mode, ref))
						if resultJSON(got, ge) != resultJSON(want, we) {
							t.Fatalf("wide %s/%s got %s, want %s", b, unit, resultJSON(got, ge), resultJSON(want, we))
						}
					}
				}
			}
		}
	}
}

// Generated parsers retain negative maxima directly in their implementation-int
// loops. Check their accepted hand-built AST contract beside bytecode lowering.
func TestGeneratedNegativeMaximum(t *testing.T) { eachMode(t, testGeneratedNegativeMaximum) }

func testGeneratedNegativeMaximum(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	large := int64(-1) << 32
	maximum := int(large)
	if strconv.IntSize < 64 {
		maximum = -2
	}
	g := grammarWithNegativeMaximum(t, `rule main []node[string] = v1:"é"* $$ => $v1.Value`, maximum)
	p, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, input := range []string{"", "é", "ééé", "x", "éx"} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			n, err := p.ParseWith("main", input, ParseOptions{Unit: unit})
			expected = append(expected, resultJSON(n, err))
		}
	}
	want := strings.Join(expected, "\n") + "\n"
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("Go typed=%v", typed), func(t *testing.T) {
			code, err := generateCode(g, gen.GenOptions{Package: "main", Start: "main", Types: typed})
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
  for _, unit := range []PositionUnit{CodePoints,Bytes} {
   n, err := Parse(input,WithUnit(unit))
   ` + astCheck + `
   out := map[string]any{"node":n}
   if err != nil {out["err"]=err.Error()}
   data,_ := json.Marshal(out);fmt.Println(string(data))
  }
 }
}`
			dir := t.TempDir()
			for name, content := range map[string]string{"go.mod": testGoMod("negative"), "parser.go": string(code), "main.go": harness} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
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
			g, err := parseTest(string(source))
			if err != nil {
				b.Fatal(err)
			}
			p, err := Compile(g, compile.Options{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				compiler := &compile.Bcompiler{Prog: p.Static(), M: &bytecode.Module{}, Strs: map[string]int{}}
				compiler.Compile()
			}
		})
	}
}
