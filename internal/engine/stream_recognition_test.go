package engine

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
	"github.com/ornew/pego/internal/value"
)

func TestStreamCaptureRecognition(t *testing.T) {
	for _, tc := range []struct {
		name, body, bound, input string
		count                    int
		ok                       bool
	}{
		{"empty zero", `x:_`, "#repeat(0)", "", 0, true},
		{"empty one", `x:_`, "#repeat(1)", "", 1, true},
		{"empty unbounded", `x:_`, "*", "", 1, true},
		{"optional absent", `x:"é"`, "#repeat(0, 1)", "", 0, true},
		{"optional present", `x:"é"`, "#repeat(0, 1)", "é", 1, true},
		{"exact", `x:"é"`, "#repeat(2)", "éé", 2, true},
		{"exact short", `x:"é"`, "#repeat(2)", "é", 1, false},
		{"exact excess", `x:"é"`, "#repeat(2)", "ééé", 2, false},
		{"unbounded", `x:"é"`, "+", "ééé", 3, true},
		{"predicate captures", `x:"é" #atomic {text($x) == "é"}`, "+", "éé", 2, true},
		{"predicate rejection", `x:. #atomic {text($x) == "é"}`, "+", "a", 0, false},
		{"nil elements", `x:r`, "*", "éé", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "rule main = (" + tc.body + ")" + tc.bound + " #stream $$\nrule r = \"é\" => nil"
			plain := compile(t, strings.ReplaceAll(source, " #stream", ""))
			for _, disableMemo := range []bool{false, true} {
				p := compile(t, source, Options{DisableMemo: disableMemo})
				programs := []*Program{p}
				for _, mode := range allModes {
					programs = append(programs, saveLoad(t, p, "main", mode))
				}
				for _, program := range programs {
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						if _, loaded := program.Mode(); loaded && backend == Closure {
							_, err := program.ParseWith("main", tc.input, servedBy(program.mode, ParseOptions{Backend: backend}))
							wantClosureRefused(t, err)
							continue
						}
						for _, unit := range []PositionUnit{CodePoints, Bytes} {
							opts := ParseOptions{Backend: backend, Unit: unit}
							for _, recognize := range []bool{false, true} {
								opts.Recognize = recognize
								if program.checkMode(opts, false) != nil {
									// A compiled grammar runs the parses of its mode only.
									continue
								}
								n, err := program.ParseWith("main", tc.input, opts)
								want, werr := plain.ParseWith("main", tc.input, opts)
								if (err == nil) != tc.ok || resultJSON(n, err) != resultJSON(want, werr) {
									t.Errorf("%s/%s recognition=%v: %s; want %s", backend, unit, recognize, resultJSON(n, err), resultJSON(want, werr))
								}
							}
							opts.Recognize = false
							if program.checkMode(opts, false) != nil {
								continue
							}
							count := 0
							err := program.ParseStreamWith("main", strings.NewReader(tc.input), func(*value.Node) error { count++; return nil }, opts)
							if count != tc.count || (err == nil) != tc.ok {
								t.Errorf("stream %s/%s: %d, %v; want %d success=%v", backend, unit, count, err, tc.count, tc.ok)
							}
						}
					}
				}
			}
		})
	}
}

func BenchmarkStreamCaptureRecognition(b *testing.B) {
	for _, captures := range []bool{false, true} {
		body := `"é"`
		if captures {
			body = `x:"é" #atomic {text($x) == "é"}`
		}
		source := "rule main = (" + body + ")* #stream $$"
		// The old capture-bearing #stream recognizer is invalid. Its ordinary
		// repetition is the equivalent successful baseline, since #stream has
		// no effect on recognition. Keep grammar setup outside the timed loop.
		if os.Getenv("PEGO_BENCH_RECOGNITION_PLAIN") != "" {
			source = strings.ReplaceAll(source, " #stream", "")
		}
		g, err := parseTest(source)
		if err != nil {
			b.Fatal(err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			b.Fatal(err)
		}
		input := strings.Repeat("é", 1000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			b.Run(fmt.Sprintf("captures=%v/%s", captures, backend), func(b *testing.B) {
				opts := ParseOptions{Backend: backend, Recognize: true}
				if _, err := p.ParseWith("main", input, opts); err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(input)))
				b.ReportAllocs()
				for b.Loop() {
					if _, err := p.ParseWith("main", input, opts); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// TestStreamRecognitionCompiled checks that a program compiled with recognition streams the
// elements of a #stream repetition as values on every backend, whether it is compiled in memory
// or loaded from a compiled grammar, and that the recognizer built from an ordinary program
// builds none.
func TestStreamRecognitionCompiled(t *testing.T) {
	g, err := syntax.Parse(startRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, Options{recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := prog.MarshalBinary("")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]*Program{"compiled": prog, "loaded": loaded} {
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if name == "loaded" && b == Closure {
				continue
			}
			var got []string
			err := p.ParseStreamWith("main", strings.NewReader("aa"), func(n *value.Node) error {
				got = append(got, n.Text())
				return nil
			}, ParseOptions{Backend: b})
			if err != nil || !slices.Equal(got, []string{"a", "a"}) {
				t.Errorf("%s %s: elements %q, error %v; want a, a", name, b, got, err)
			}
		}
	}
	// The recognizer of an ordinary program only recognizes.
	g, err = syntax.Parse(startRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := ordinary.recognizer()
	if err != nil {
		t.Fatal(err)
	}
	if r := rec.byName["main"]; !r.novalue {
		t.Error("the recognizer builds values for the rule with a #stream repetition")
	}
}
