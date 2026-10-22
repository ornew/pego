package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

var compiledRecognitionLoadSink *Program
var compiledRecognitionSaveSink []byte
var compiledRecognitionParseNodeSink *Node
var compiledRecognitionParseErrorSink error

// mustMarshal returns the bytes of a save that cannot fail in a benchmark.
func mustMarshal(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
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
	g, err := parseTest(src.String())
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
			noBundle := marshalWithoutRecognition(p, "main")
			bundle, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
			if err != nil {
				b.Fatal(err)
			}
			// Warm the lazy recognition program so SerializeCached measures encoding,
			// while CompileAndFirstSave includes its first-save construction cost.
			if _, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true}); err != nil {
				b.Fatal(err)
			}

			for _, tc := range []struct {
				name    string
				data    []byte
				bundled bool
			}{{"no-bundle", noBundle, false}, {"bundle", bundle, true}} {
				b.Run("SerializeCached/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						var data []byte
						var err error
						if tc.bundled {
							data, err = p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
						} else {
							data = marshalWithoutRecognition(p, "main")
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
						if tc.bundled {
							data, err = fresh.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
						} else {
							data = marshalWithoutRecognition(fresh, "main")
						}
						if err != nil {
							b.Fatal(err)
						}
						compiledRecognitionSaveSink = data
					}
					b.ReportMetric(float64(len(tc.data)), "B/file")
				})
			}

			b.Run("Load/no-bundle", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(noBundle, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(noBundle)), "B/file")
			})
			b.Run("Load/bundle", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(bundle, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(bundle)), "B/file")
			})
		})
	}
}

func BenchmarkCompiledRecognitionASTBearing(b *testing.B) {
	for _, ruleCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Rules%d", ruleCount), func(b *testing.B) {
			g, source := compiledRecognitionBenchmarkGrammar(b, ruleCount)
			// Use a separately compiled Program so that preparing the cached save
			// cannot affect the first-save measurement.
			saved, err := Compile(g, Options{})
			if err != nil {
				b.Fatal(err)
			}
			data := mustMarshal(saved.MarshalBinary("main"))
			if data[len(compiledMagic)] != compiledVersion {
				b.Fatalf("unexpected format version: %d", data[len(compiledMagic)])
			}
			loaded, _, err := LoadProgram(data, Options{})
			if err != nil {
				b.Fatalf("load fixture: %v", err)
			}
			inputs := []struct {
				name  string
				input string
			}{{"last-rule-match", fmt.Sprintf("token-%03d", ruleCount-1)}, {"no-match", "not-a-token"}}
			for _, backend := range []Backend{Default, Bytecode} {
				for _, tc := range inputs {
					want, wantErr := source.ParseWith("main", tc.input, ParseOptions{Backend: backend})
					got, gotErr := loaded.ParseWith("main", tc.input, ParseOptions{Backend: backend})
					if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
						b.Fatalf("loaded fixture differs for %v/%s: got %s want %s", backend, tc.name, resultJSON(got, gotErr), resultJSON(want, wantErr))
					}
				}
			}

			b.Run("SerializeCached", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					compiledRecognitionSaveSink = mustMarshal(saved.MarshalBinary("main"))
				}
				b.ReportMetric(float64(len(data)), "B/file")
			})
			b.Run("CompileAndFirstSave", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					fresh, err := Compile(g, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionSaveSink = mustMarshal(fresh.MarshalBinary("main"))
				}
				b.ReportMetric(float64(len(data)), "B/file")
			})
			b.Run("Load", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					loaded, _, err := LoadProgram(data, Options{})
					if err != nil {
						b.Fatal(err)
					}
					compiledRecognitionLoadSink = loaded
				}
				b.ReportMetric(float64(len(data)), "B/file")
			})

			for _, backend := range []struct {
				name string
				kind Backend
			}{{"Default", Default}, {"Bytecode", Bytecode}} {
				for _, input := range inputs {
					// Run one identical parse before timing to warm the loaded program's
					// backend and scratch pools outside the measured loop.
					if _, err := loaded.ParseWith("main", input.input, ParseOptions{Backend: backend.kind}); err != nil && input.name == "last-rule-match" {
						b.Fatalf("warm fixture: %v", err)
					}
					b.Run(fmt.Sprintf("Parse/%s/%s", backend.name, input.name), func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							compiledRecognitionParseNodeSink, compiledRecognitionParseErrorSink = loaded.ParseWith("main", input.input, ParseOptions{Backend: backend.kind})
						}
					})
				}
			}
		})
	}
}
