package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestFiniteStreamRepetitions(t *testing.T) {
	for _, tc := range []struct {
		name, repeat, input, suffix, definitions string
		count                                    int
		ok                                       bool
	}{
		{"zero", `"a"{0}`, "", "", "", 0, true},
		{"zero rejects element", `"a"{0}`, "a", "", "", 0, false},
		{"zero leaves suffix", `"a"{0}`, "a", `"a"`, "", 0, true},
		{"optional empty", `"a"{0,1}`, "", "", "", 0, true},
		{"optional one", `"a"{0,1}`, "a", "", "", 1, true},
		{"optional excess", `"a"{0,1}`, "aa", "", "", 1, false},
		{"below minimum", `"a"{1,2}`, "", "", "", 0, false},
		{"at minimum", `"a"{1,2}`, "a", "", "", 1, true},
		{"at maximum", `"a"{1,2}`, "aa", "", "", 2, true},
		{"beyond maximum", `"a"{1,2}`, "aaa", "", "", 2, false},
		{"same-token suffix", `"a"{1,2}`, "aaa", `"a"`, "", 2, true},
		{"distinct suffix", `"a"{1,2}`, "aa!", `"!"`, "", 2, true},
		{"excess before suffix", `"a"{1,2}`, "aaa!", `"!"`, "", 2, false},
		{"exact below minimum", `"a"{2}`, "a", "", "", 1, false},
		{"exact", `"a"{2}`, "aa", "", "", 2, true},
		{"exact excess", `"a"{2}`, "aaa", "", "", 2, false},
		{"empty zero", `""{0}`, "", "", "", 0, true},
		{"empty stalls", `""{0,2}`, "", "", "", 1, true},
		{"captures", `(k:@"é"){1,2}`, "ééé", `"é"`, "", 2, true},
		{"nil results", `r{1,2}`, "aaa", `"a"`, "\ndef r=\"a\" -> nil", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf("def main=%s #stream %s $$%s", tc.repeat, tc.suffix, tc.definitions)
			prog := compile(t, src)
			data, err := prog.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			saved, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []Unit{CodePoints, Bytes} {
				var expected []string
				refErr := prog.ParseStreamWith("main", strings.NewReader(tc.input), func(n *Node) error {
					expected = append(expected, resultJSON(n, nil))
					return nil
				}, ParseOptions{Backend: Closure, Unit: unit})
				if len(expected) != tc.count || (refErr == nil) != tc.ok {
					t.Fatalf("closure/%s: emitted %d, error %v; want %d, success %v", unit, len(expected), refErr, tc.count, tc.ok)
				}
				for _, p := range []*Program{prog, saved} {
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						if _, err := p.ParseWith("main", tc.input, opts); (err == nil) != tc.ok {
							t.Errorf("batch %s/%s: error %v, want success %v", backend, unit, err, tc.ok)
						}
						var got []string
						err := p.ParseStreamWith("main", strings.NewReader(tc.input), func(n *Node) error {
							got = append(got, resultJSON(n, nil))
							return nil
						}, opts)
						if strings.Join(got, "\n") != strings.Join(expected, "\n") || resultJSON(nil, err) != resultJSON(nil, refErr) {
							t.Errorf("stream %s/%s: emitted %d, error %v; want %d, error %v", backend, unit, len(got), err, len(expected), refErr)
						}
					}
				}
			}
		})
	}
}

func TestZeroRepetitionDoesNotRunElement(t *testing.T) {
	for _, src := range []string{
		`def main=("a" "b"){0} "ab" $$`,
		`def main=-("a" "b"){0} "ab" $$`,
		`def main=xs:(f:"a" "b"){0} "ab" $$ -> map($xs, (x) => $x.f)`,
		"def main=r{0} \"ab\" $$\ndef r=\"a\" -> nil",
	} {
		// The zero bound must leave the entire input for the suffix,
		// including when the compiler projects or discards element values.
		t.Run(src, func(t *testing.T) {
			prog := compile(t, src)
			checkBackends(t, prog, "main", "ab")
			for _, unit := range []Unit{CodePoints, Bytes} {
				want, err := prog.ParseWith("main", "ab", ParseOptions{Unit: unit})
				if err != nil {
					t.Fatal(err)
				}
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					d, err := prog.NewDocumentWith("main", "ab", ParseOptions{Backend: backend, Unit: unit})
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						n, err := d.Parse()
						if resultJSON(n, err) != resultJSON(want, nil) {
							t.Errorf("Document %s/%s: got %s, want %s", backend, unit, resultJSON(n, err), resultJSON(want, nil))
						}
					}
				}
			}
		})
	}
}

func TestFiniteStreamConsumerError(t *testing.T) {
	prog := compile(t, `def main="a"{1,2} #stream $$`)
	stop := errors.New("consumer stopped")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			count := 0
			err := prog.ParseStreamWith("main", strings.NewReader("aaa"), func(*Node) error {
				count++
				return stop
			}, ParseOptions{Backend: backend, Unit: unit})
			if count != 1 || !errors.Is(err, stop) {
				t.Errorf("%s/%s: emitted %d, error %v; want one, consumer error", backend, unit, count, err)
			}
		}
	}
}

func BenchmarkStreamRepetitionLimit(b *testing.B) {
	for _, tc := range []struct{ name, bound string }{{"Finite", "{10000}"}, {"Unbounded", "*"}} {
		g, err := syntax.Parse("def main=\"a\"" + tc.bound + " #stream $$")
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, Options{})
		if err != nil {
			b.Fatal(err)
		}
		input := strings.Repeat("a", 10000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			b.Run(tc.name+"/"+backend.String(), func(b *testing.B) {
				if _, err := prog.rule(backend, "main"); err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(input)))
				b.ReportAllocs()
				for b.Loop() {
					count := 0
					err := prog.ParseStreamWith("main", strings.NewReader(input), func(*Node) error {
						count++
						return nil
					}, ParseOptions{Backend: backend})
					if err != nil || count != 10000 {
						b.Fatalf("emitted %d: %v", count, err)
					}
				}
			})
		}
	}
}
