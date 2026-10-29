package engine

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/value"
)

const streamRecoveryError = "#recover cannot wrap a #stream repetition; recover inside each element instead"

func TestStreamRecoveryRejected(t *testing.T) {
	t.Parallel()
	eachMode(t, testStreamRecoveryRejected)
}

func testStreamRecoveryRejected(t *testing.T) {
	for _, pragmas := range []string{`#stream #recover(skip=.*)`, `#recover(skip=.*) #stream`} {
		src := `rule main = "ab" #repeat(2) ` + pragmas + ` $$`
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []compile.Options{{}, {Recognize: true}} {
			if _, err := Compile(g, opts); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
				t.Errorf("%s: %v", src, err)
			}
		}
		for name, generate := range map[string]func() error{
			"Go": func() error { _, err := generateCode(t, g, gen.GenOptions{Start: "main", Package: "test"}); return err },
			"typed Go": func() error {
				_, err := generateCode(t, g, gen.GenOptions{Start: "main", Package: "test", Types: true})
				return err
			},
			"TS": func() error { _, err := generateTSBundle(g, gen.GenOptions{Start: "main"}); return err },
		} {
			if err := generate(); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
				t.Errorf("%s generation: %v", name, err)
			}
		}
	}
	pos := grammar.Pos{Line: 3, Col: 10}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Recovery{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: &grammar.Stream{Expr: &grammar.Repeat{
		Expr: &grammar.Literal{Value: "ab"}, Min: 2, Max: 2,
	}},
		Skip: &grammar.Repeat{Expr: &grammar.Any{}, Min: 0, Max: -1},
	}}}}}
	starttest.MarkAll(g)
	_, err := Compile(g, compile.Options{})
	if errs, ok := err.(compile.ErrorList); !ok || len(errs) != 1 || errs[0].Pos != pos || !strings.Contains(err.Error(), streamRecoveryError) {
		t.Errorf("public AST: %v; want positioned recovery error", err)
	}
}

// legacyStreamRecovery builds the bytecode emitted before recovery around a
// stream was rejected: compile recovery, then restore its stream marker in the
// AST and its NEXT flag in the module. Both file formats retain the old shape.
func legacyStreamRecovery(t *testing.T, streamFirst bool) *Program {
	t.Helper()
	p := mustCompile(t, `rule main = "ab" #repeat(2) #recover(skip=.*) $$`)
	m := p.Module()
	next := 0
	for i := range m.Code {
		if m.Code[i].Op == bytecode.OpNext {
			m.Code[i].B = 2
			next++
		}
	}
	if next != 1 {
		t.Fatalf("legacy fixture has %d NEXT instructions", next)
	}
	a := p.Grammar.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.Attributed)
	// Mutate the normalized executable AST, which is the .pegoc payload.
	stream := &grammar.Attribute{Name: "stream", Pragma: true}
	if streamFirst {
		a.Attrs = append([]*grammar.Attribute{stream}, a.Attrs...)
	} else {
		a.Attrs = append(a.Attrs, stream)
	}
	return p
}

func TestCompiledStreamRecoveryRejected(t *testing.T) {
	t.Parallel()
	for _, streamFirst := range []bool{false, true} {
		p := legacyStreamRecovery(t, streamFirst)
		for _, mode := range allModes {
			data, err := p.Marshal("main", mode)
			if err != nil {
				// Building the recognizer of recognize mode rejects the shape before saving.
				if !strings.Contains(err.Error(), "recover") {
					t.Errorf("legacy streamFirst=%v %s mode save: %v", streamFirst, mode, err)
				}
				continue
			}
			if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "recover") {
				t.Errorf("legacy streamFirst=%v %s mode: %v", streamFirst, mode, err)
			}
		}
	}
}

func TestStreamElementRecovery(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`("ab" #recover(skip=[^a]+))`, `(("ab" #recover(skip=[^a]+)) #recover(skip="!"))`, `item`} {
		p := mustCompile(t, `rule main = `+body+`+ #stream $$
rule item = "ab" @error(message="record") #recover(skip=[^a]+)`)
		for _, mode := range valueModes {
			data, err := p.Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if backend == Closure {
					err := loaded.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*value.Node) error { return nil }, servedBy(mode, ParseOptions{Backend: backend}))
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					count := 0
					err := loaded.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*value.Node) error { count++; return nil }, servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
					var recovered SyntaxErrors
					if !errors.As(err, &recovered) || len(recovered) != 1 || count != 3 {
						t.Errorf("%s %s mode %s/%s: count=%d error=%v", body, mode, backend, unit, count, err)
					}
					again, err := loaded.Marshal("main", mode)
					if err != nil || !bytes.Equal(data, again) {
						t.Errorf("roundtrip: %v", err)
					}
				}
			}
		}
	}
}

func TestStreamRecoveryRegions(t *testing.T) {
	t.Parallel()
	safe := []bytecode.Instr{{Op: bytecode.OpRecover, A: 3}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 5}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpNext, B: 2}, {Op: bytecode.OpEnd}}
	for _, tc := range []struct {
		name string
		edit func([]bytecode.Instr)
		want string
	}{
		{"element local", func([]bytecode.Instr) {}, ""},
		{"body", func(c []bytecode.Instr) { c[1] = bytecode.Instr{Op: bytecode.OpNext, B: 2} }, "encloses a stream commitment"},
		{"skip", func(c []bytecode.Instr) { c[3] = bytecode.Instr{Op: bytecode.OpNext, B: 2} }, "encloses a stream commitment"},
		{"missing body boundary", func(c []bytecode.Instr) { c[0].A = 1 }, "invalid recovery body"},
		{"missing skip boundary", func(c []bytecode.Instr) { c[2].A = 4 }, "invalid recovery skip"},
	} {
		c := append([]bytecode.Instr(nil), safe...)
		tc.edit(c)
		err := bytecode.ValidateStreamRecovery(c)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	nested := []bytecode.Instr{{Op: bytecode.OpRecover, A: 8}, {Op: bytecode.OpRecover, A: 4}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 6}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 10}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpNext, B: 2}, {Op: bytecode.OpEnd}}
	if err := bytecode.ValidateStreamRecovery(nested); err != nil {
		t.Errorf("nested element-local recovery: %v", err)
	}
	nested[3].A = 10
	if err := bytecode.ValidateStreamRecovery(nested); err == nil || !strings.Contains(err.Error(), "crossing recovery regions") {
		t.Errorf("crossing regions: %v", err)
	}
	bodyToSkip := []bytecode.Instr{{Op: bytecode.OpRecover, A: 4}, {Op: bytecode.OpRecover, A: 6}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 9}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 8}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpNext, B: 2}, {Op: bytecode.OpEnd}}
	if err := bytecode.ValidateStreamRecovery(bodyToSkip); err == nil || !strings.Contains(err.Error(), "crossing recovery regions") {
		t.Errorf("body-to-skip crossing leaves recovery active at NEXT: %v", err)
	}
	nestedSkip := []bytecode.Instr{{Op: bytecode.OpRecover, A: 3}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 9}, {Op: bytecode.OpRecover, A: 6}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndRecover, A: 8}, {Op: bytecode.OpTop}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpEndSkip}, {Op: bytecode.OpNext, B: 2}, {Op: bytecode.OpEnd}}
	if err := bytecode.ValidateStreamRecovery(nestedSkip); err != nil {
		t.Errorf("nested recovery inside skip: %v", err)
	}
}

func BenchmarkStreamRecoveryPreparation(b *testing.B) {
	for _, n := range []int{1, 100} {
		var src strings.Builder
		src.WriteString("rule main = item+ #stream $$\n")
		for i := range n {
			name := fmt.Sprintf("r%d", i)
			if i == 0 {
				name = "item"
			}
			fmt.Fprintf(&src, "rule %s = \"ab\" #recover(skip=[^a]+)\n", name)
		}
		g, err := parseTest(src.String())
		if err != nil {
			b.Fatal(err)
		}
		p, err := Compile(g, compile.Options{})
		if err != nil {
			b.Fatal(err)
		}
		data, err := p.MarshalBinary("main")
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("Compile/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(g, compile.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("LoadBare/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := LoadProgram(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
