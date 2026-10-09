package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestStreamCaptureRecognition(t *testing.T) {
	for _, tc := range []struct {
		name, body, bound, input string
		count                    int
		ok                       bool
	}{
		{"empty zero", `x:_`, "{0}", "", 0, true},
		{"empty one", `x:_`, "{1}", "", 1, true},
		{"empty unbounded", `x:_`, "*", "", 1, true},
		{"optional absent", `x:"é"`, "{0,1}", "", 0, true},
		{"optional present", `x:"é"`, "{0,1}", "é", 1, true},
		{"exact", `x:"é"`, "{2}", "éé", 2, true},
		{"exact short", `x:"é"`, "{2}", "é", 1, false},
		{"exact excess", `x:"é"`, "{2}", "ééé", 2, false},
		{"unbounded", `x:"é"`, "+", "ééé", 3, true},
		{"predicate captures", `x:@"é" [text($x)=="é"]`, "+", "éé", 2, true},
		{"predicate rejection", `x:@. [text($x)=="é"]`, "+", "a", 0, false},
		{"nil elements", `x:r`, "*", "éé", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "def main=(" + tc.body + ")" + tc.bound + " #stream $$\ndef r=\"é\" -> nil"
			plain := compile(t, strings.ReplaceAll(source, " #stream", ""))
			for _, disableMemo := range []bool{false, true} {
				p := compile(t, source, Options{DisableMemo: disableMemo})
				data, err := p.MarshalBinary("main")
				if err != nil {
					t.Fatal(err)
				}
				saved, _, err := LoadProgram(data, Options{DisableMemo: disableMemo})
				if err != nil {
					t.Fatal(err)
				}
				for _, program := range []*Program{p, saved} {
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						for _, unit := range []Unit{CodePoints, Bytes} {
							opts := ParseOptions{Backend: backend, Unit: unit}
							for _, recognize := range []bool{false, true} {
								opts.Recognize = recognize
								n, err := program.ParseWith("main", tc.input, opts)
								want, werr := plain.ParseWith("main", tc.input, opts)
								if (err == nil) != tc.ok || resultJSON(n, err) != resultJSON(want, werr) {
									t.Errorf("%s/%s recognition=%v: %s; want %s", backend, unit, recognize, resultJSON(n, err), resultJSON(want, werr))
								}
							}
							opts.Recognize = false
							count := 0
							err := program.ParseStreamWith("main", strings.NewReader(tc.input), func(*Node) error { count++; return nil }, opts)
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
			body = `x:@"é" [text($x)=="é"]`
		}
		source := "def main=(" + body + ")* #stream $$"
		// The old capture-bearing #stream recognizer is invalid. Its ordinary
		// repetition is the equivalent successful baseline, since #stream has
		// no effect on recognition. Keep grammar setup outside the timed loop.
		if os.Getenv("PEGO_BENCH_RECOGNITION_PLAIN") != "" {
			source = strings.ReplaceAll(source, " #stream", "")
		}
		g, err := syntax.Parse(source)
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
