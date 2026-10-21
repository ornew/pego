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

	"github.com/ornew/pego/grammar"
)

// marshalV2NoRecognition creates the version 2 equivalent of an AST-less save.
// It is a benchmark/test control for the v3 writer's optional recognition bundle.
func marshalV2NoRecognition(prog *Program, start string) []byte {
	m := prog.Module()
	w := &encoder{strs: map[string]int{}}
	for i, s := range m.Strings {
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.str(start)
	pkg := prog.pkg
	if prog.Grammar != nil {
		pkg = prog.Grammar.File.Pkg.Name
	}
	w.str(pkg)
	w.module(m)
	w.bool(false) // AST omitted
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(2)
	out.Write(binary.AppendUvarint(nil, uint64(m.instructionSetVersion())))
	w.finish(&out)
	return out.Bytes()
}

// marshalV3WithAST reproduces files written before AST-bearing saves returned
// to version 2. The loader must continue to accept their optional bundle.
func marshalV3WithAST(prog *Program, start string, recognition *Program) []byte {
	m := prog.Module()
	w := &encoder{strs: map[string]int{}}
	for i, s := range m.Strings {
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.str(start)
	w.str(prog.Grammar.File.Pkg.Name)
	w.module(m)
	w.bool(true)
	bundle := encodeRecognition(recognition)
	w.uint(len(bundle))
	w.body.Write(bundle)
	w.bool(true)
	w.statements(prog)
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.Write(binary.AppendUvarint(nil, uint64(m.instructionSetVersion())))
	w.finish(&out)
	return out.Bytes()
}

func TestCompiledRecognitionV3WithASTResavesAsV2(t *testing.T) {
	p := compile(t, `rule main = "a" $$`)
	rec, err := p.recognizer()
	if err != nil {
		t.Fatal(err)
	}
	legacy := marshalV3WithAST(p, "main", rec)
	loaded, start, err := LoadProgram(legacy, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if start != "main" || loaded.Grammar == nil || loaded.recognitionModule == nil {
		t.Fatal("version 3 AST and recognition bundle were not loaded")
	}
	got, err := loaded.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	want, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if got[len(compiledMagic)] != 2 || !bytes.Equal(got, want) {
		t.Fatal("AST-bearing version 3 file did not normalize to version 2")
	}
	withoutAST, err := loaded.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil || withoutAST[len(compiledMagic)] != 3 {
		t.Fatalf("AST-less resave lost the recognition bundle: %v", err)
	}
}

func TestCompiledRecognitionRoundTrip(t *testing.T) {
	for _, c := range genCorpus(t) {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, c.src)
			for _, omit := range []bool{false, true} {
				data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
				if err != nil {
					t.Fatal(err)
				}
				if version := int(data[len(compiledMagic)]); version != map[bool]int{false: 2, true: 3}[omit] {
					t.Fatalf("omitAST=%t compiled version=%d", omit, version)
				}
				loaded, _, err := LoadProgram(data, Options{})
				if err != nil {
					t.Fatal(err)
				}
				again, err := loaded.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
				if err != nil || !bytes.Equal(data, again) {
					t.Fatalf("resave differs: %v", err)
				}
				for _, entry := range moduleEntries(p.Module()) {
					for _, input := range append(append([]string{}, c.inputs...), "\xff", "", "trailing") {
						for _, unit := range []PositionUnit{CodePoints, Bytes} {
							want, werr := p.ParseWith(entry.name, input, ParseOptions{Recognize: true, Unit: unit})
							for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
								got, gerr := loaded.ParseWith(entry.name, input, ParseOptions{Recognize: true, Unit: unit, Backend: backend})
								if got != nil || want != nil || resultJSON(got, gerr) != resultJSON(want, werr) {
									t.Fatalf("omit=%t start=%s unit=%v backend=%v input=%q: got %s want %s", omit, entry.name, unit, backend, input, resultJSON(got, gerr), resultJSON(want, werr))
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestCompiledRecognitionLegacyCapability(t *testing.T) {
	p := moduleProgram(compile(t, `rule main = "a"`).Module())
	data, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[len(compiledMagic)] != 2 {
		t.Fatal("legacy AST-less save gained a capability")
	}
	loaded, _, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
		n, err := loaded.ParseWith("main", "a", ParseOptions{Backend: backend, Recognize: true})
		if n != nil || err == nil || !strings.Contains(err.Error(), "recompile") {
			t.Fatalf("node=%v error=%v", n, err)
		}
	}
	again, err := loaded.MarshalBinary("main")
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("legacy resave differs: %v", err)
	}
}

func TestCompiledRecognitionV2NoBundleBaseline(t *testing.T) {
	p := compile(t, `rule main = first / second $$
rule first = "a"
rule second = "é"`)
	withAST, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if withAST[len(compiledMagic)] != 2 || !bytes.Equal(withAST, marshalRecognitionV2(p, "main", false)) {
		t.Fatal("AST-bearing save differs from version 2")
	}
	if p.rec != nil {
		t.Fatal("AST-bearing save built an unused recognizer")
	}
	v2Data := marshalV2NoRecognition(p, "main")
	if v2Data[len(compiledMagic)] != 2 {
		t.Fatal("baseline is not compiled format version 2")
	}
	v3Data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	if v3Data[len(compiledMagic)] != 3 {
		t.Fatal("recognition save is not compiled format version 3")
	}
	v2, _, err := LoadProgram(v2Data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	v3, _, err := LoadProgram(v3Data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		match bool
	}{{"a", true}, {"é", true}, {"", false}, {"a?", false}} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				got, gotErr := v3.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit, Recognize: true})
				if got != nil || (gotErr == nil) != tc.match {
					t.Fatalf("v3 recognition %v/%v %q: node=%v err=%v", backend, unit, tc.input, got, gotErr)
				}
				legacy, legacyErr := v2.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit, Recognize: true})
				if legacy != nil || legacyErr == nil || !strings.Contains(legacyErr.Error(), "recognition needs") {
					t.Fatalf("v2 recognition %v/%v %q: node=%v err=%v", backend, unit, tc.input, legacy, legacyErr)
				}
				want, wantErr := p.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
				for _, loaded := range []*Program{v2, v3} {
					got, gotErr := loaded.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
					if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
						t.Fatalf("normal parse %v/%v %q: got %s want %s", backend, unit, tc.input, resultJSON(got, gotErr), resultJSON(want, wantErr))
					}
				}
			}
		}
	}
}

func TestCompiledRecognitionASTSaveAfterClassMutation(t *testing.T) {
	p := compile(t, `rule main = [a-z]`)
	p.Module() // Keep the compiled bytecode as it was before the AST change.
	class := p.Grammar.Rules()[0].Expr.(*grammar.CharClass)
	class.Ranges[0] = grammar.CharRange{Lo: 'z', Hi: 'a'}
	data, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[len(compiledMagic)] != 2 || p.rec != nil {
		t.Fatal("AST-bearing save gained an unused recognition bundle")
	}
}

func TestCompiledRecognitionControlsAndConcurrency(t *testing.T) {
	p := compile(t, `rule main = [a-z]* $$
rule other = "é" $$`)
	data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err = LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		n, err := p.ParseWith("main", "abc", ParseOptions{Backend: backend, Recognize: true, Context: ctx})
		if n != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel node=%v error=%v", n, err)
		}
		n, err = p.ParseWith("main", "abc", ParseOptions{Backend: backend, Recognize: true, WorkLimits: WorkLimits{MaxInputWork: 1}})
		if n != nil || err == nil {
			t.Fatalf("budget node=%v error=%v", n, err)
		}
		n, err = p.ParseWith("main", "abc", ParseOptions{Backend: backend, Recognize: true})
		if n != nil || err != nil {
			t.Fatalf("retry node=%v error=%v", n, err)
		}
	}
	if _, err := p.ParseWith("main", "abc", ParseOptions{Backend: Closure, Recognize: true}); err == nil {
		t.Fatal("AST-less closure succeeded")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				for _, recognize := range []bool{false, true} {
					n, err := p.ParseWith("other", "é", ParseOptions{Backend: backend, Recognize: recognize})
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
	const source = `rule main = "a" $$
rule other = "b" $$`
	want, err := compile(t, source).MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	p := compile(t, source)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				got, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("concurrent save differs: %v", err)
					return
				}
				got[0] ^= 0xff // The returned file must not own the cached bundle.
			}
		}()
	}
	wg.Wait()
	if len(p.recognitionBytes) == 0 {
		t.Fatal("source recognizer bundle was not cached")
	}
	again, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil || !bytes.Equal(again, want) {
		t.Fatalf("save after caller mutation differs: %v", err)
	}
}

func TestCompiledRecognitionLoadedModuleMutation(t *testing.T) {
	data, err := compile(t, `rule main = "a" $$`).MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p.recognitionModule.Strings = append(p.recognitionModule.Strings, "unused")
	changed, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil || bytes.Equal(changed, data) {
		t.Fatalf("loaded bundle mutation was not serialized: %v", err)
	}
	if _, _, err := LoadProgram(changed, Options{}); err != nil {
		t.Fatalf("mutated bundle is not loadable: %v", err)
	}
}

func TestCompiledRecognitionCorruptMainAndBundle(t *testing.T) {
	p := compile(t, `rule main = "a" $$`)
	data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(data, Options{}); err != nil {
		t.Fatal(err)
	}
	rec, err := p.recognizer()
	if err != nil {
		t.Fatal(err)
	}
	bundle := encodeRecognition(rec)
	pos := bytes.Index(data, bundle)
	if pos < 0 {
		t.Fatal("bundle not found in saved file")
	}
	corruptBundle := append([]byte(nil), data...)
	corruptBundle[pos] = 99 // unsupported bundle instruction set
	binary.LittleEndian.PutUint32(corruptBundle[len(corruptBundle)-4:], crc32.ChecksumIEEE(corruptBundle[:len(corruptBundle)-4]))
	if _, _, err := LoadProgram(corruptBundle, Options{}); err == nil || !strings.Contains(err.Error(), "recognition instruction set") {
		t.Fatalf("corrupt bundle: %v", err)
	}
	reservedBundle := append([]byte(nil), data...)
	reservedBundle[pos] = 7 // reserved instruction set is not a supported version
	binary.LittleEndian.PutUint32(reservedBundle[len(reservedBundle)-4:], crc32.ChecksumIEEE(reservedBundle[:len(reservedBundle)-4]))
	if _, _, err := LoadProgram(reservedBundle, Options{}); err == nil || !strings.Contains(err.Error(), "recognition instruction set") {
		t.Fatalf("reserved bundle instruction set: %v", err)
	}

	// Invalid main rule names must be rejected before bundle entry comparison.
	p.Module().Rules[0].Name = len(p.Module().Strings)
	corruptMain, err := p.MarshalBinaryWith("", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(corruptMain, Options{}); err == nil || !strings.Contains(err.Error(), "invalid compiled grammar") {
		t.Fatalf("corrupt main module: %v", err)
	}
}

func TestCompiledRecognitionBundleValidation(t *testing.T) {
	p := compile(t, `rule main = "a"
rule other = "b"`)
	rec, err := p.recognizer()
	if err != nil {
		t.Fatal(err)
	}
	valid := encodeRecognition(rec)
	if _, _, err := decodeRecognition(valid, p.Module()); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]recognitionEntry{
		{},
		{{"main", 0}},
		{{"main", 0}, {"main", 1}},
		{{"main", 1}, {"other", 0}},
		{{"other", 1}, {"main", 0}},
		{{"main", 0}, {"other", 999999}},
	} {
		rec.recognitionStarts = entries
		if _, _, err := decodeRecognition(encodeRecognition(rec), p.Module()); err == nil {
			t.Fatalf("invalid entries accepted: %+v", entries)
		}
	}
	rec.recognitionStarts = nil
	for _, data := range [][]byte{valid[:len(valid)-1], append(append([]byte{}, valid...), 0), append([]byte{99}, valid[1:]...)} {
		if _, _, err := decodeRecognition(data, p.Module()); err == nil {
			t.Fatal("invalid bundle accepted")
		}
	}
}
