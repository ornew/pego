package engine

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

func negativeMaxima() []int {
	values := []int{-1, -2, -1 << 31}
	if strconv.IntSize == 64 {
		large := int64(-1) << 32
		values = append(values, int(large), -int(^uint(0)>>1)-1)
	}
	return values
}

func TestNegativeRepetitionMaxima(t *testing.T) {
	t.Parallel()
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
				g := gentest.GrammarWithNegativeMaximum(t, c.src, maximum)
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
											if gentest.ResultJSON(got, ge) != gentest.ResultJSON(want, we) {
												t.Errorf("%s/%s recognize=%v input=%q: got %s, want %s", b, unit, recognize, input, gentest.ResultJSON(got, ge), gentest.ResultJSON(want, we))
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
	t.Parallel()
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
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("wide minimum needs 64-bit implementation")
	}
	for _, body := range []string{`"é" #repeat(2147483648, ) $$`, `[é] #repeat(2147483648, ) #atomic $$`} {
		for _, maximum := range negativeMaxima() {
			g := gentest.GrammarWithNegativeMaximum(t, "rule main = "+body, maximum)
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
						if gentest.ResultJSON(got, ge) != gentest.ResultJSON(want, we) {
							t.Fatalf("wide %s/%s got %s, want %s", b, unit, gentest.ResultJSON(got, ge), gentest.ResultJSON(want, we))
						}
					}
				}
			}
		}
	}
}
