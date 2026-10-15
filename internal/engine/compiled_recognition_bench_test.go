package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

var compiledRecognitionLoadSink *Program
var compiledRecognitionSaveSink []byte
var compiledRecognitionParseNodeSink *Node
var compiledRecognitionParseErrorSink error

// marshalRecognitionV2 is the AST-bearing or AST-less v2 control writer used
// to compare v3's recognition bundle without changing production semantics.
func marshalRecognitionV2(prog *Program, start string, omitAST bool) []byte {
	m := prog.Module()
	w := &encoder{strs: map[string]int{}}
	for i, s := range m.Strings {
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.str(start)
	pkg := prog.pkg
	if prog.Grammar != nil {
		pkg = prog.Grammar.Package
	}
	w.str(pkg)
	w.module(m)
	withAST := prog.Grammar != nil && !omitAST
	w.bool(withAST)
	if withAST {
		w.statements(prog, 2)
	}
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(2)
	out.Write(binary.AppendUvarint(nil, uint64(m.instructionSetVersion())))
	w.finish(&out)
	return out.Bytes()
}

func compiledRecognitionBenchmarkGrammar(b *testing.B, ruleCount int) (*grammar.Grammar, *Program) {
	b.Helper()
	var src strings.Builder
	src.WriteString("rule main = ")
	for i := 0; i < ruleCount; i++ {
		if i != 0 {
			src.WriteString(" / ")
		}
		fmt.Fprintf(&src, "r%03d", i)
	}
	src.WriteString(" $$\n")
	for i := 0; i < ruleCount; i++ {
		fmt.Fprintf(&src, "rule r%03d = %q\n", i, fmt.Sprintf("token-%03d", i))
	}
	g, err := syntax.Parse(src.String())
	if err != nil {
		b.Fatalf("parse benchmark grammar: %v", err)
	}
	p, err := Compile(g, Options{})
	if err != nil {
		b.Fatalf("compile benchmark grammar: %v", err)
	}
	return g, p
}

func BenchmarkCompiledRecognitionSerialization(b *testing.B) {
	for _, ruleCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Rules%d", ruleCount), func(b *testing.B) {
			g, p := compiledRecognitionBenchmarkGrammar(b, ruleCount)
			v2 := marshalRecognitionV2(p, "main", true)
			v3, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
			if err != nil {
				b.Fatal(err)
			}
			// Warm the lazy recognition program so SerializeCached measures encoding,
			// while CompileAndFirstSave includes its first-save construction cost.
			if _, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true}); err != nil {
				b.Fatal(err)
			}

			for _, tc := range []struct {
				name string
				data []byte
				v3   bool
			}{{"v2-no-bundle", v2, false}, {"v3-bundle", v3, true}} {
				b.Run("SerializeCached/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						var data []byte
						var err error
						if tc.v3 {
							data, err = p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
						} else {
							data = marshalRecognitionV2(p, "main", true)
						}
						if err != nil {
							b.Fatal(err)
						}
						compiledRecognitionSaveSink = data
					}
					b.ReportMetric(float64(len(tc.data)), "B/file")
				})
				b.Run("CompileAndFirstSave/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						fresh, err := Compile(g, Options{})
						if err != nil {
							b.Fatal(err)
						}
						var data []byte
						if tc.v3 {
							data, err = fresh.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
						} else {
							data = marshalRecognitionV2(fresh, "main", true)
						}
						if err != nil {
							b.Fatal(err)
						}
						compiledRecognitionSaveSink = data
					}
					b.ReportMetric(float64(len(tc.data)), "B/file")
				})
			}

			b.Run("Load/v2-no-bundle", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(v2, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(v2)), "B/file")
			})
			b.Run("Load/v3-bundle", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(v3, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(v3)), "B/file")
			})
		})
	}
}

func BenchmarkCompiledRecognitionASTBearing(b *testing.B) {
	for _, ruleCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Rules%d", ruleCount), func(b *testing.B) {
			g, source := compiledRecognitionBenchmarkGrammar(b, ruleCount)
			// Use separately compiled but identical Programs so preparing the v3
			// recognition cache cannot affect the v2 control.
			v2Program, err := Compile(g, Options{})
			if err != nil {
				b.Fatal(err)
			}
			v3Program, err := Compile(g, Options{})
			if err != nil {
				b.Fatal(err)
			}
			v2Data := marshalRecognitionV2(v2Program, "main", false)
			v3Data, err := v3Program.MarshalBinary("main")
			if err != nil {
				b.Fatal(err)
			}
			if v2Data[len(compiledMagic)] != 2 || v3Data[len(compiledMagic)] != 3 {
				b.Fatalf("unexpected format versions: v2=%d v3=%d", v2Data[len(compiledMagic)], v3Data[len(compiledMagic)])
			}
			v2Loaded, _, err := LoadProgram(v2Data, Options{})
			if err != nil {
				b.Fatalf("load v2 fixture: %v", err)
			}
			v3Loaded, _, err := LoadProgram(v3Data, Options{})
			if err != nil {
				b.Fatalf("load v3 fixture: %v", err)
			}
			inputs := []struct {
				name  string
				input string
			}{{"last-rule-match", fmt.Sprintf("token-%03d", ruleCount-1)}, {"no-match", "not-a-token"}}
			for _, backend := range []Backend{Default, Bytecode} {
				for _, tc := range inputs {
					want, wantErr := source.ParseWith("main", tc.input, ParseOptions{Backend: backend})
					for name, loaded := range map[string]*Program{"v2": v2Loaded, "v3": v3Loaded} {
						got, gotErr := loaded.ParseWith("main", tc.input, ParseOptions{Backend: backend})
						if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
							b.Fatalf("%s fixture differs for %v/%s: got %s want %s", name, backend, tc.name, resultJSON(got, gotErr), resultJSON(want, wantErr))
						}
					}
				}
			}

			for _, tc := range []struct {
				name string
				data []byte
				v3   bool
			}{{"v2-ast", v2Data, false}, {"v3-ast", v3Data, true}} {
				b.Run("SerializeCached/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						var data []byte
						var err error
						if tc.v3 {
							data, err = v3Program.MarshalBinary("main")
						} else {
							data = marshalRecognitionV2(v2Program, "main", false)
						}
						if err != nil {
							b.Fatal(err)
						}
						compiledRecognitionSaveSink = data
					}
					b.ReportMetric(float64(len(tc.data)), "B/file")
					cacheBytes := 0
					if tc.v3 {
						cacheBytes = cap(v3Program.recognitionBytes)
					}
					b.ReportMetric(float64(cacheBytes), "B/cache")
				})
				b.Run("CompileAndFirstSave/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						fresh, err := Compile(g, Options{})
						if err != nil {
							b.Fatal(err)
						}
						var data []byte
						if tc.v3 {
							data, err = fresh.MarshalBinary("main")
						} else {
							data = marshalRecognitionV2(fresh, "main", false)
						}
						if err != nil {
							b.Fatal(err)
						}
						compiledRecognitionSaveSink = data
					}
					b.ReportMetric(float64(len(tc.data)), "B/file")
				})
			}

			b.Run("Load/v2-ast", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(v2Data, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(v2Data)), "B/file")
			})
			b.Run("Load/v3-ast", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(v3Data, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(v3Data)), "B/file")
			})

			for _, backend := range []struct {
				name string
				kind Backend
			}{{"Default", Default}, {"Bytecode", Bytecode}} {
				for _, input := range inputs {
					for _, fixture := range []struct {
						name string
						prog *Program
					}{{"v2-ast", v2Loaded}, {"v3-ast", v3Loaded}} {
						// Run one identical parse before timing to warm each loaded program's
						// backend and scratch pools outside the measured loop.
						if _, err := fixture.prog.ParseWith("main", input.input, ParseOptions{Backend: backend.kind}); err != nil && input.name == "last-rule-match" {
							b.Fatalf("warm %s fixture: %v", fixture.name, err)
						}
						b.Run(fmt.Sprintf("Parse/%s/%s/%s", backend.name, input.name, fixture.name), func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								compiledRecognitionParseNodeSink, compiledRecognitionParseErrorSink = fixture.prog.ParseWith("main", input.input, ParseOptions{Backend: backend.kind})
							}
						})
					}
				}
			}
		})
	}
}
