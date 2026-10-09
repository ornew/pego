package engine

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

const streamRecoveryError = "#recover cannot wrap a #stream repetition"

func TestStreamRecoveryRejected(t *testing.T) {
	for _, attrs := range []string{`#stream #recover(skip=.*)`, `#recover(skip=.*) #stream`} {
		src := `def main="ab"{2} ` + attrs + ` $$`
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []Options{{}, {NoTypeCheck: true}, {recognize: true}} {
			if _, err := Compile(g, opts); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
				t.Errorf("%s: %v", src, err)
			}
		}
		for name, generate := range map[string]func() error{
			"Go": func() error { _, err := Generate(g, GenOptions{Start: "main", Package: "test"}); return err },
			"typed Go": func() error {
				_, err := Generate(g, GenOptions{Start: "main", Package: "test", Types: true})
				return err
			},
			"TS": func() error { _, err := GenerateTS(g, GenOptions{Start: "main"}); return err },
		} {
			if err := generate(); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
				t.Errorf("%s generation: %v", name, err)
			}
		}
	}
	pos := grammar.Pos{Line: 3, Col: 10}
	g := &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{Name: "main", Expr: &grammar.Attributed{
		Expr:  &grammar.Repeat{Expr: &grammar.Literal{Value: "ab"}, Min: 2, Max: 2},
		Attrs: []*grammar.Attribute{{Name: "stream"}, {Name: "recover", Pos: pos, Args: []*grammar.AttrArg{{Name: "skip", Value: &grammar.Repeat{Expr: &grammar.Any{}, Min: 0, Max: -1}}}}},
	}}}}
	_, err := Compile(g, Options{})
	if errs, ok := err.(ErrorList); !ok || len(errs) != 1 || errs[0].Pos != pos || !strings.Contains(err.Error(), streamRecoveryError) {
		t.Errorf("public AST: %v; want positioned recovery error", err)
	}
}

// legacyStreamRecovery builds the bytecode emitted before recovery around a
// stream was rejected: compile recovery, then restore its stream marker in the
// AST and its NEXT flag in the module. Both file formats retain the old shape.
func legacyStreamRecovery(t *testing.T, streamFirst bool) *Program {
	t.Helper()
	p := compile(t, `def main="ab"{2} #recover(skip=.*) $$`)
	m := p.Module()
	next := 0
	for i := range m.Code {
		if m.Code[i].Op == OpNext {
			m.Code[i].B = 2
			next++
		}
	}
	if next != 1 {
		t.Fatalf("legacy fixture has %d NEXT instructions", next)
	}
	a := p.Grammar.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.Attributed)
	stream := &grammar.Attribute{Name: "stream"}
	if streamFirst {
		a.Attrs = append([]*grammar.Attribute{stream}, a.Attrs...)
	} else {
		a.Attrs = append(a.Attrs, stream)
	}
	return p
}

func TestCompiledStreamRecoveryRejected(t *testing.T) {
	for _, streamFirst := range []bool{false, true} {
		p := legacyStreamRecovery(t, streamFirst)
		for _, omit := range []bool{false, true} {
			data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "recover") {
				t.Errorf("legacy streamFirst=%v omit=%v: %v", streamFirst, omit, err)
			}
		}
		if _, _, err := LoadProgram(marshalV1(p, "main"), Options{}); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
			t.Errorf("v1 legacy: %v", err)
		}
	}
}

func TestStreamElementRecovery(t *testing.T) {
	for _, body := range []string{`("ab" #recover(skip=(?^a)+))`, `(("ab" #recover(skip=(?^a)+)) #recover(skip="!"))`, `item`} {
		p := compile(t, `def main=`+body+`+ #stream $$
def item="ab" #error(message="record") #recover(skip=(?^a)+)`)
		for _, omit := range []bool{false, true} {
			data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if omit && backend == Closure {
					continue
				}
				for _, unit := range []Unit{CodePoints, Bytes} {
					count := 0
					err := loaded.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*Node) error { count++; return nil }, ParseOptions{Backend: backend, Unit: unit})
					var recovered SyntaxErrors
					if !errors.As(err, &recovered) || len(recovered) != 1 || count != 3 {
						t.Errorf("%s omit=%v %s/%s: count=%d error=%v", body, omit, backend, unit, count, err)
					}
					again, err := loaded.MarshalBinary("main")
					if err != nil || !bytes.Equal(data, again) {
						t.Errorf("roundtrip: %v", err)
					}
				}
			}
		}
	}
}

func TestStreamRecoveryRegions(t *testing.T) {
	safe := []Instr{{Op: OpRecover, A: 3}, {Op: OpTop}, {Op: OpEndRecover, A: 5}, {Op: OpTop}, {Op: OpEndSkip}, {Op: OpNext, B: 2}, {Op: OpEnd}}
	for _, tc := range []struct {
		name string
		edit func([]Instr)
		want string
	}{
		{"element local", func([]Instr) {}, ""},
		{"body", func(c []Instr) { c[1] = Instr{Op: OpNext, B: 2} }, "encloses a stream commitment"},
		{"skip", func(c []Instr) { c[3] = Instr{Op: OpNext, B: 2} }, "encloses a stream commitment"},
		{"missing body boundary", func(c []Instr) { c[0].A = 1 }, "invalid recovery body"},
		{"missing skip boundary", func(c []Instr) { c[2].A = 4 }, "invalid recovery skip"},
	} {
		c := append([]Instr(nil), safe...)
		tc.edit(c)
		err := validateStreamRecovery(c)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	nested := []Instr{{Op: OpRecover, A: 8}, {Op: OpRecover, A: 4}, {Op: OpTop}, {Op: OpEndRecover, A: 6}, {Op: OpTop}, {Op: OpEndSkip}, {Op: OpTop}, {Op: OpEndRecover, A: 10}, {Op: OpTop}, {Op: OpEndSkip}, {Op: OpNext, B: 2}, {Op: OpEnd}}
	if err := validateStreamRecovery(nested); err != nil {
		t.Errorf("nested element-local recovery: %v", err)
	}
	nested[3].A = 10
	if err := validateStreamRecovery(nested); err == nil || !strings.Contains(err.Error(), "crossing recovery regions") {
		t.Errorf("crossing regions: %v", err)
	}
	bodyToSkip := []Instr{{Op: OpRecover, A: 4}, {Op: OpRecover, A: 6}, {Op: OpTop}, {Op: OpEndRecover, A: 9}, {Op: OpTop}, {Op: OpEndRecover, A: 8}, {Op: OpTop}, {Op: OpEndSkip}, {Op: OpEndSkip}, {Op: OpNext, B: 2}, {Op: OpEnd}}
	if err := validateStreamRecovery(bodyToSkip); err == nil || !strings.Contains(err.Error(), "crossing recovery regions") {
		t.Errorf("body-to-skip crossing leaves recovery active at NEXT: %v", err)
	}
	nestedSkip := []Instr{{Op: OpRecover, A: 3}, {Op: OpTop}, {Op: OpEndRecover, A: 9}, {Op: OpRecover, A: 6}, {Op: OpTop}, {Op: OpEndRecover, A: 8}, {Op: OpTop}, {Op: OpEndSkip}, {Op: OpEndSkip}, {Op: OpNext, B: 2}, {Op: OpEnd}}
	if err := validateStreamRecovery(nestedSkip); err != nil {
		t.Errorf("nested recovery inside skip: %v", err)
	}
}

func BenchmarkStreamRecoveryPreparation(b *testing.B) {
	for _, n := range []int{1, 100} {
		var src strings.Builder
		src.WriteString("def main=item+ #stream $$\n")
		for i := range n {
			name := fmt.Sprintf("r%d", i)
			if i == 0 {
				name = "item"
			}
			fmt.Fprintf(&src, "def %s=\"ab\" #recover(skip=(?^a)+)\n", name)
		}
		g, err := syntax.Parse(src.String())
		if err != nil {
			b.Fatal(err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			b.Fatal(err)
		}
		data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("Compile/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(g, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("LoadBare/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := LoadProgram(data, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
