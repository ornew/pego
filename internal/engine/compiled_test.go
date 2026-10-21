package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math/rand"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// TestCompiledRoundTrip checks that a program loaded from a compiled grammar returns the same
// results as the original program.
func TestCompiledRoundTrip(t *testing.T) {
	for _, c := range genCorpus(t) {
		t.Run(c.name, func(t *testing.T) {
			checkCompiledRoundTrip(t, compile(t, c.src), c.inputs)
		})
	}
}

// The retired terminal constructors are read with syntax.ParseLegacy as the text struct
// constructors they mean.
func TestCompiledRoundTripTerminalConstructors(t *testing.T) {
	for name, src := range map[string]string{
		"value error":     "type Int terminal\nvar value int\nrule main = {value := 1} \"x\" => Int{list(value, \"a\")[0].(string)}",
		"predicate error": "type Int terminal\nvar value int\nrule main = {value := 1} {Int{list(value, \"a\")[0].(string)} != nil} \"x\" / \"x\"",
	} {
		t.Run(name, func(t *testing.T) {
			checkCompiledRoundTrip(t, compileLegacy(t, src), []string{"x", ""})
		})
	}
}

func checkCompiledRoundTrip(t *testing.T, prog *Program, inputs []string) {
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, start, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if start != "main" {
		t.Errorf("start %q", start)
	}
	for _, in := range inputs {
		n1, err1 := prog.Parse("main", in)
		n2, err2 := loaded.Parse(start, in)
		if a, b := resultJSON(n1, err1), resultJSON(n2, err2); a != b {
			t.Errorf("input %q\n original %s\n loaded   %s", in, a, b)
		}
	}
	// Saving the loaded program again produces the same bytes.
	again, err := loaded.MarshalBinary("main")
	if err != nil || !bytes.Equal(data, again) {
		t.Errorf("re-marshaled data differs (%v)", err)
	}
	// An AST-less file carries a recognition bundle and returns the same
	// results on the bytecode backend. The bundle can make it larger.
	bare, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	if data[len(compiledMagic)] != 2 || bare[len(compiledMagic)] != 3 {
		t.Fatalf("compiled versions: with AST %d, without AST %d", data[len(compiledMagic)], bare[len(compiledMagic)])
	}
	bareProg, _, err := LoadProgram(bare, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if bareProg.Grammar != nil || strings.Join(bareProg.Rules(), ",") != strings.Join(prog.Rules(), ",") {
		t.Errorf("bare program: grammar %v, rules %v", bareProg.Grammar, bareProg.Rules())
	}
	if _, err := bareProg.ParseWith("main", "", ParseOptions{Backend: Closure}); err == nil || !strings.Contains(err.Error(), "closure backend") {
		t.Errorf("closure backend without the AST: %v", err)
	}
	if again, err := bareProg.MarshalBinary("main"); err != nil || !bytes.Equal(bare, again) {
		t.Errorf("re-marshaled bare data differs (%v)", err)
	}
	for _, in := range inputs {
		want := resultJSON(prog.Parse("main", in))
		for _, l := range []struct {
			name string
			prog *Program
			b    Backend
		}{{"bare", bareProg, Default}, {"bytecode", loaded, Bytecode}, {"iterative", bareProg, BytecodeIterative}} {
			if got := resultJSON(l.prog.ParseWith("main", in, ParseOptions{Backend: l.b})); got != want {
				t.Errorf("%s: input %q\n original %s\n loaded   %s", l.name, in, want, got)
			}
		}
	}
}

// TestCompiledStreamAndDocument checks that stream and incremental parsing work with a loaded
// program.
func TestCompiledStreamAndDocument(t *testing.T) {
	data, _ := compile(t, records).MarshalBinary("")
	prog, start, err := LoadProgram(data, Options{})
	if err != nil || start != "" {
		t.Fatalf("%v %q", err, start)
	}
	var got []string
	err = prog.ParseStream("main", strings.NewReader("#records\na=1\nb=2\n"), func(n *Node) error {
		got = append(got, n.Field("k").(*Node).text)
		return nil
	})
	if err != nil || strings.Join(got, ",") != "a,b" {
		t.Errorf("got %v, %v", got, err)
	}

	data, _ = compile(t, incrementalGrammar).MarshalBinary("main")
	prog, _, _ = LoadProgram(data, Options{})
	fresh := compile(t, incrementalGrammar)
	doc, _ := prog.NewDocument("main", "x = 1+2\nab,cd\n")
	doc.Parse()
	doc.Edit(4, 5, "(7)")
	n, err := doc.Parse()
	fn, ferr := fresh.Parse("main", doc.Text())
	if resultJSON(n, err) != resultJSON(fn, ferr) || doc.Stats().Reused == 0 {
		t.Errorf("document on loaded program: %v %v %+v", n, err, doc.Stats())
	}
}

func TestCompiledErrors(t *testing.T) {
	data, err := compile(t, arith).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	resum := func(b []byte) []byte {
		b = append([]byte(nil), b[:len(b)-4]...)
		return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	}
	version := append([]byte(nil), data...)
	version[len(compiledMagic)] = 99
	isa := append([]byte(nil), data...)
	isa[len(compiledMagic)+1] = isaVersion + 1
	isa = resum(isa)
	flipped := append([]byte(nil), data...)
	flipped[20] ^= 0xff
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"not compiled", []byte("rule main = x"), "not a compiled PEGO grammar"},
		{"too short", []byte(compiledMagic), "too short"},
		{"version", version, "unsupported compiled grammar version 99"},
		{"isa version", isa, fmt.Sprintf("unsupported instruction set version %d", isaVersion+1)},
		{"checksum", flipped, "checksum mismatch"},
		{"truncated", resum(data[:len(data)-10]), "invalid compiled grammar"},
		{"trailing", resum(append(append([]byte(nil), data[:len(data)-4]...), 0, 0, 0, 0, 0)), "trailing bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := LoadProgram(tc.data, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := compile(t, arith).MarshalBinary("nope"); err == nil {
		t.Error("expected an error for an undefined start rule")
	}
}

// TestCompiledEarlierInstructionSet checks that a file of an earlier instruction set, which built
// the previous syntax trees, does not load and asks for the grammar to be compiled again, that a
// file of a later version does not load either, and that one of the current version does.
func TestCompiledEarlierInstructionSet(t *testing.T) {
	src := `rule main = n:[0-9]+ #atomic => $n`
	data, err := compile(t, src).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	resum := func(b []byte) []byte {
		b = append([]byte(nil), b[:len(b)-4]...)
		return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	}
	for _, isa := range []byte{1, 8, 12, isaVersion, isaVersion + 1} {
		d := append([]byte(nil), data...)
		d[len(compiledMagic)+1] = isa
		prog, _, err := LoadProgram(resum(d), Options{})
		switch {
		case isa < isaVersion:
			if err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
				t.Errorf("isa %d: got %v", isa, err)
			}
			continue
		case isa > isaVersion:
			if err == nil || !strings.Contains(err.Error(), "unsupported instruction set version") {
				t.Errorf("isa %d: got %v", isa, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("isa %d: %v", isa, err)
		}
		if got := resultWith(prog, "12", ParseOptions{Backend: Bytecode}); got != `"12"` {
			t.Errorf("isa %d: got %s", isa, got)
		}
	}
}

// TestCompiledCorruptionDoesNotPanic checks that loading corrupted data (with a valid checksum)
// does not panic.
func TestCompiledCorruptionDoesNotPanic(t *testing.T) {
	var seeds [][]byte
	for _, c := range genCorpus(t) {
		prog := compile(t, c.src)
		data, err := prog.MarshalBinary("main")
		if err != nil {
			t.Fatal(err)
		}
		bare, _ := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
		seeds = append(seeds, data, bare)
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 3000; i++ {
		seed := seeds[rng.Intn(len(seeds))]
		b := append([]byte(nil), seed[:len(seed)-4]...)
		for k := rng.Intn(4) + 1; k > 0; k-- {
			p := len(compiledMagic) + 1 + rng.Intn(len(b)-len(compiledMagic)-1)
			b[p] = byte(rng.Intn(256))
		}
		if rng.Intn(4) == 0 {
			b = b[:len(compiledMagic)+1+rng.Intn(len(b)-len(compiledMagic)-1)]
		}
		b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
		func() {
			defer func() {
				if x := recover(); x != nil {
					t.Fatalf("panic on corrupted data: %v\n%s", x, debug.Stack())
				}
			}()
			prog, start, err := LoadProgram(b, Options{})
			if err == nil && start != "" {
				// Non-terminating recursion, for example from corrupted left-recursion marks, fails at the
				// nesting depth limit.
				for _, b := range []Backend{Default, Bytecode, BytecodeIterative} {
					prog.ParseWith(start, "1+2", ParseOptions{Backend: b, MaxDepth: 1000})
				}
			}
		}()
	}
}

func FuzzLoadProgram(f *testing.F) {
	for _, src := range []string{arith, ast, incrementalGrammar, records} {
		g := mustParse(src)
		prog, err := Compile(g, Options{})
		if err != nil {
			f.Fatal(err)
		}
		data, _ := prog.MarshalBinary("main")
		f.Add(data)
		bare, _ := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
		f.Add(bare)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		prog, start, err := LoadProgram(data, Options{})
		if err == nil && start != "" {
			prog.ParseWith(start, "a=1\n", ParseOptions{MaxDepth: 1000})
			prog.ParseWith(start, "a=1\n", ParseOptions{Backend: Bytecode, MaxDepth: 1000})
		}
	})
}

// BenchmarkLoad compares compiling from source with loading a compiled grammar.
func BenchmarkLoad(b *testing.B) {
	src, err := os.ReadFile("../../examples/minilang/minilang.pego")
	if err != nil {
		b.Fatal(err)
	}
	prog, _ := Compile(mustParse(string(src)), Options{})
	data, _ := prog.MarshalBinary("main")
	b.Run("source", func(b *testing.B) {
		for b.Loop() {
			g, _ := syntax.Parse(string(src))
			Compile(g, Options{})
		}
	})
	b.Run("compiled", func(b *testing.B) {
		for b.Loop() {
			LoadProgram(data, Options{})
		}
	})
}

// TestCompiledTypeExpressions checks that every form of type expression survives a compiled
// grammar: a load that read only some of them would misread the rest of the file.
func TestCompiledTypeExpressions(t *testing.T) {
	src := `type P = { A node[string]; B *(int); C []node[string] }
type Q ~ P | node[string] & node[_]
rule main P = a:[a-z] => P{A: $a, B: 1}
rule pair node[(string)] = [a-z]`
	prog := compile(t, src)
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if again, err := loaded.MarshalBinary("main"); err != nil || !bytes.Equal(data, again) {
		t.Errorf("re-marshaled data differs (%v)", err)
	}
	for _, rule := range []string{"main", "pair"} {
		for _, in := range []string{"a", "ab", ""} {
			n1, err1 := prog.Parse(rule, in)
			n2, err2 := loaded.Parse(rule, in)
			if a, b := resultJSON(n1, err1), resultJSON(n2, err2); a != b {
				t.Errorf("%s %q\n original %s\n loaded   %s", rule, in, a, b)
			}
		}
	}
	if a, b := grammarTypes(prog), grammarTypes(loaded); a != b {
		t.Errorf("types differ\n original %s\n loaded   %s", a, b)
	}
}

// grammarTypes renders the type definitions and rule types of a program's grammar, ignoring
// layout, which compiled grammars do not keep.
func grammarTypes(prog *Program) string {
	var b strings.Builder
	for _, s := range prog.Grammar.File.Decls {
		switch s := s.(type) {
		case grammar.TypeDecl:
			fmt.Fprintf(&b, "type %s", s.DeclName())
			switch spec := s.(type) {
			case *grammar.TypeDef:
				for _, f := range spec.Struct().Fields {
					fmt.Fprintf(&b, " %s %s;", f.Name, grammar.FormatType(f.Type))
				}
			case *grammar.TypeAlias:
				b.WriteString(" ~ " + grammar.FormatType(spec.Type))
			}
			b.WriteString("\n")
		case *grammar.RuleDef:
			if s.Type != nil {
				fmt.Fprintf(&b, "rule %s %s\n", s.Name, grammar.FormatType(s.Type))
			}
		}
	}
	return b.String()
}
