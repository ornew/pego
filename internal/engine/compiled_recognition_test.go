package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/bytecode"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/limits"
)

// moduleRuleNames returns the names of the rules of m in order, each once: value-free twins follow
// the rules they belong to and have their names.
func moduleRuleNames(m *bytecode.Module) []string {
	var names []string
	seen := map[string]bool{}
	for _, r := range m.Rules {
		if name := m.Strings[r.Name]; !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// wantModeRefused fails unless err refuses an operation that needs mode need on a compiled grammar
// in mode have, naming both modes and how to get the right file.
func wantModeRefused(t *testing.T, err error, have, need CompiledMode) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "the compiled grammar is in "+have.String()+" mode") ||
		!strings.Contains(err.Error(), " "+need.String()+" mode: compile it") || !strings.Contains(err.Error(), "-mode "+need.String()) ||
		!strings.Contains(err.Error(), "compile the grammar from source") {
		t.Fatalf("operation for %s mode on a %s-mode file: %v", need, have, err)
	}
}

// TestCompiledRecognitionASTFileRejectsBundle checks that a file holds one module: a second module
// after the first, such as the recognizer's, is data the format does not have.
func TestCompiledRecognitionASTFileRejectsBundle(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = "a" $$`)
	rec, err := p.Recognizer()
	if err != nil {
		t.Fatal(err)
	}
	w := &bytecode.Encoder{Strs: map[string]int{}}
	w.WriteModule(rec.Module())
	stored := marshalWithTrailing(p, "main", w.Body.Bytes())
	if _, _, err := LoadProgram(stored); err == nil || !strings.Contains(err.Error(), "trailing bytes") {
		t.Fatalf("file with a second module: %v", err)
	}
}

func TestCompiledRecognitionRoundTrip(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.GenCorpus(t) {
		t.Run(c.Name, func(t *testing.T) {
			p := mustCompile(t, c.Src)
			for _, mode := range allModes {
				data, err := p.Marshal("main", mode)
				if err != nil {
					t.Fatal(err)
				}
				if version := int(data[len(compiledMagic)]); version != compiledVersion {
					t.Fatalf("%s mode compiled version=%d", mode, version)
				}
				loaded, _, err := LoadProgram(data)
				if err != nil {
					t.Fatal(err)
				}
				again, err := loaded.Marshal("main", mode)
				if err != nil || !bytes.Equal(data, again) {
					t.Fatalf("resave differs: %v", err)
				}
				for _, name := range moduleRuleNames(p.Module()) {
					for _, input := range append(append([]string{}, c.Inputs...), "\xff", "", "trailing") {
						for _, unit := range []PositionUnit{CodePoints, Bytes} {
							want, werr := p.ParseWith(name, input, ParseOptions{Recognize: true, Unit: unit})
							for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
								got, gerr := loaded.ParseWith(name, input, ParseOptions{Recognize: true, Unit: unit, Backend: backend})
								if mode != CompiledRecognize {
									wantModeRefused(t, gerr, mode, CompiledRecognize)
									continue
								}
								if got != nil || want != nil || gentest.ResultJSON(got, gerr) != gentest.ResultJSON(want, werr) {
									t.Fatalf("%s mode start=%s unit=%v backend=%v input=%q: got %s want %s", mode, name, unit, backend, input, gentest.ResultJSON(got, gerr), gentest.ResultJSON(want, werr))
								}
							}
						}
					}
				}
			}
		})
	}
}

// TestCompiledRecognitionWithoutBundle checks that a file in parse mode refuses recognition with the
// mode it needs, and is saved again unchanged.
func TestCompiledRecognitionWithoutBundle(t *testing.T) {
	t.Parallel()
	p := moduleProgram(mustCompile(t, `rule main = "a"`).Module())
	data, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
		n, err := loaded.ParseWith("main", "a", ParseOptions{Backend: backend, Recognize: true})
		if n != nil {
			t.Fatalf("node=%v", n)
		}
		wantModeRefused(t, err, CompiledParse, CompiledRecognize)
	}
	again, err := loaded.MarshalBinary("main")
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("resave differs: %v", err)
	}
}

// TestCompiledRecognitionBundleBaseline checks the two modes side by side: the recognize-mode file
// recognizes and refuses value parses, and the parse-mode file parses and refuses recognition.
func TestCompiledRecognitionBundleBaseline(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = first / second $$
rule first = "a"
rule second = "é"`)
	parseData, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if parseData[len(compiledMagic)] != compiledVersion {
		t.Fatalf("parse-mode save has version %d", parseData[len(compiledMagic)])
	}
	if p.rec != nil {
		t.Fatal("parse-mode save built an unused recognizer")
	}
	recognizeData, err := p.Marshal("main", CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	if recognizeData[len(compiledMagic)] != compiledVersion || CompiledMode(recognizeData[len(compiledMagic)+1]) != CompiledRecognize {
		t.Fatal("recognition save is not the current format")
	}
	parsing, _, err := LoadProgram(parseData)
	if err != nil {
		t.Fatal(err)
	}
	recognizing, _, err := LoadProgram(recognizeData)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		match bool
	}{{"a", true}, {"é", true}, {"", false}, {"a?", false}} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				got, gotErr := recognizing.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit, Recognize: true})
				if got != nil || (gotErr == nil) != tc.match {
					t.Fatalf("recognition %v/%v %q: node=%v err=%v", backend, unit, tc.input, got, gotErr)
				}
				noRecognition, noRecognitionErr := parsing.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit, Recognize: true})
				if noRecognition != nil {
					t.Fatalf("recognition in parse mode %v/%v %q: node=%v", backend, unit, tc.input, noRecognition)
				}
				wantModeRefused(t, noRecognitionErr, CompiledParse, CompiledRecognize)
				want, wantErr := p.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
				got, gotErr = parsing.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
				if gentest.ResultJSON(got, gotErr) != gentest.ResultJSON(want, wantErr) {
					t.Fatalf("normal parse %v/%v %q: got %s want %s", backend, unit, tc.input, gentest.ResultJSON(got, gotErr), gentest.ResultJSON(want, wantErr))
				}
				_, gotErr = recognizing.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
				wantModeRefused(t, gotErr, CompiledRecognize, CompiledParse)
			}
		}
	}
}

func TestCompiledRecognitionASTSaveAfterClassMutation(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = [a-z]`)
	p.Module() // Keep the compiled bytecode as it was before the AST change.
	class := p.Grammar.Rules()[0].Expr.(*grammar.CharClass)
	class.Ranges[0] = grammar.CharRange{Lo: 'z', Hi: 'a'}
	data, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if p.rec != nil || data[len(compiledMagic)] != compiledVersion {
		t.Fatal("parse-mode save built an unused recognizer")
	}
}

func TestCompiledRecognitionControlsAndConcurrency(t *testing.T) {
	t.Parallel()
	src := mustCompile(t, `rule main = [a-z]* $$
rule other = "é" $$`)
	p := saveLoad(t, src, "main", CompiledRecognize)
	parsing := saveLoad(t, src, "main", CompiledParse)
	for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
		// The reference build has no cancellation or limits.
		if core.BudgetControlsEnabled {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			n, err := p.ParseWith("main", "abc", ParseOptions{Backend: backend, Recognize: true, Context: ctx})
			if n != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel node=%v error=%v", n, err)
			}
			n, err = p.ParseWith("main", "abc", ParseOptions{Backend: backend, Recognize: true, Limits: limits.Limits{Budget: 1}})
			if n != nil || err == nil {
				t.Fatalf("budget node=%v error=%v", n, err)
			}
		}
		n, err := p.ParseWith("main", "abc", ParseOptions{Backend: backend, Recognize: true})
		if n != nil || err != nil {
			t.Fatalf("retry node=%v error=%v", n, err)
		}
	}
	_, err := p.ParseWith("main", "abc", ParseOptions{Backend: Closure, Recognize: true})
	wantClosureRefused(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				for _, recognize := range []bool{false, true} {
					prog := parsing
					if recognize {
						prog = p
					}
					n, err := prog.ParseWith("other", "é", ParseOptions{Backend: backend, Recognize: recognize})
					if err != nil || (n == nil) != recognize {
						t.Errorf("concurrent node=%v error=%v", n, err)
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestCompiledRecognitionRepeatedConcurrentSave(t *testing.T) {
	t.Parallel()
	const source = `rule main = "a" $$
rule other = "b" $$`
	want, err := mustCompile(t, source).Marshal("main", CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	p := mustCompile(t, source)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				got, err := p.Marshal("main", CompiledRecognize)
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("concurrent save differs: %v", err)
					return
				}
				got[0] ^= 0xff // The returned file must not share memory with the program.
			}
		}()
	}
	wg.Wait()
	if p.rec == nil {
		t.Fatal("the recognizer was not kept for later saves")
	}
	again, err := p.Marshal("main", CompiledRecognize)
	if err != nil || !bytes.Equal(again, want) {
		t.Fatalf("save after caller mutation differs: %v", err)
	}
}

func TestCompiledRecognitionLoadedModuleMutation(t *testing.T) {
	t.Parallel()
	data, err := mustCompile(t, `rule main = "a" $$`).Marshal("main", CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	p.Module().Strings = append(p.Module().Strings, "unused")
	changed, err := p.Marshal("main", CompiledRecognize)
	if err != nil || bytes.Equal(changed, data) {
		t.Fatalf("loaded module mutation was not serialized: %v", err)
	}
	if _, _, err := LoadProgram(changed); err != nil {
		t.Fatalf("mutated module is not loadable: %v", err)
	}
}

func TestCompiledRecognitionCorruptMainAndBundle(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = "a" $$`)
	data, err := p.Marshal("main", CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(data); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), data...)
	corrupt[isaOffset] = 99 // unsupported instruction set
	binary.LittleEndian.PutUint32(corrupt[len(corrupt)-4:], crc32.ChecksumIEEE(corrupt[:len(corrupt)-4]))
	if _, _, err := LoadProgram(corrupt); err == nil || !strings.Contains(err.Error(), "unsupported instruction set version 99") {
		t.Fatalf("corrupt instruction set: %v", err)
	}
	reserved := append([]byte(nil), data...)
	reserved[isaOffset] = 7 // reserved instruction set is not a supported version
	binary.LittleEndian.PutUint32(reserved[len(reserved)-4:], crc32.ChecksumIEEE(reserved[:len(reserved)-4]))
	if _, _, err := LoadProgram(reserved); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("reserved instruction set: %v", err)
	}
	mode := append([]byte(nil), data...)
	mode[len(compiledMagic)+1] = 3 // no such mode
	binary.LittleEndian.PutUint32(mode[len(mode)-4:], crc32.ChecksumIEEE(mode[:len(mode)-4]))
	if _, _, err := LoadProgram(mode); err == nil || !strings.Contains(err.Error(), "unknown mode 3") {
		t.Fatalf("unknown mode: %v", err)
	}

	// Invalid rule names are rejected.
	rec, err := p.Recognizer()
	if err != nil {
		t.Fatal(err)
	}
	rec.Module().Rules[0].Name = len(rec.Module().Strings)
	corruptMain, err := p.Marshal("", CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(corruptMain); err == nil || !strings.Contains(err.Error(), "invalid compiled grammar") {
		t.Fatalf("corrupt module: %v", err)
	}
}

// TestCompiledRecognitionBundleValidation checks that a recognize-mode file starts each rule name at
// the first rule of that name, the rule itself rather than its value-free twin, and that truncated
// or extended files are rejected.
func TestCompiledRecognitionBundleValidation(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = "a"
rule other = "b"`)
	valid, err := p.Marshal("main", CompiledRecognize)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(valid)
	if err != nil {
		t.Fatal(err)
	}
	m := loaded.Module()
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, name := range moduleRuleNames(m) {
			r, err := loaded.rule(backend, name)
			if err != nil {
				t.Fatal(err)
			}
			first := -1
			for i, mr := range m.Rules {
				if m.Strings[mr.Name] == name {
					first = i
					break
				}
			}
			if r.ID != first {
				t.Fatalf("%s: rule %s starts at rule %d, want %d", backend, name, r.ID, first)
			}
		}
	}
	resum := func(d []byte) []byte {
		d = append([]byte(nil), d...)
		binary.LittleEndian.PutUint32(d[len(d)-4:], crc32.ChecksumIEEE(d[:len(d)-4]))
		return d
	}
	truncated := resum(append(append([]byte(nil), valid[:len(valid)-5]...), 0, 0, 0, 0))
	extended := resum(append(append(append([]byte(nil), valid[:len(valid)-4]...), 0), 0, 0, 0, 0))
	for _, data := range [][]byte{truncated, extended} {
		if _, _, err := LoadProgram(data); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
}
