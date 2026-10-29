package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

func TestNullableRepetitionMinimum(t *testing.T) {
	for _, body := range []string{`_`, `""`, `{true}`, `_ #atomic`, `x:_`, `r`} {
		for _, bound := range []struct {
			text  string
			count int
			ok    bool
		}{{"#repeat(0)", 0, true}, {"#repeat(0, 1)", 1, true}, {"#repeat(0, 2)", 1, true}, {"#repeat(1)", 1, true}, {"#repeat(2)", 1, false}, {"#repeat(2, )", 1, false}} {
			t.Run(body+bound.text, func(t *testing.T) {
				src := "rule main = (" + body + ")" + bound.text + " #stream $$\nrule r = _ => nil"
				for _, disabled := range []bool{false, true} {
					prog := mustCompile(t, src, compile.Options{DisableMemo: disabled})
					programs := []*Program{prog}
					for _, mode := range allModes {
						programs = append(programs, saveLoad(t, prog, "main", mode))
					}
					for _, p := range programs {
						_, loaded := p.Mode()
						for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
							if loaded && backend == Closure {
								_, err := p.ParseWith("main", "", servedBy(p.mode, ParseOptions{Backend: backend}))
								wantClosureRefused(t, err)
								continue
							}
							for _, unit := range []PositionUnit{CodePoints, Bytes} {
								opts := ParseOptions{Backend: backend, Unit: unit}
								for _, recognize := range []bool{false, true} {
									opts.Recognize = recognize
									if p.checkMode(opts, false) != nil {
										// A compiled grammar runs the parses of its mode only.
										continue
									}
									if _, err := p.ParseWith("main", "", opts); (err == nil) != bound.ok {
										t.Errorf("batch %s/%s recognize=%v: %v, want success=%v", backend, unit, recognize, err, bound.ok)
									}
								}
								opts.Recognize = false
								if p.checkMode(opts, false) == nil {
									count := 0
									err := p.ParseStreamWith("main", strings.NewReader(""), func(*value.Node) error { count++; return nil }, opts)
									if count != bound.count || (err == nil) != bound.ok {
										t.Errorf("stream %s/%s: %d values, %v; want %d, success=%v", backend, unit, count, err, bound.count, bound.ok)
									}
								}
								if p.checkMode(opts, true) != nil {
									continue
								}
								d, err := p.NewDocumentWith("main", "", opts)
								if err != nil {
									t.Fatal(err)
								}
								for range 2 {
									if _, err := d.Parse(); (err == nil) != bound.ok {
										t.Errorf("Document %s/%s: %v, want success=%v", backend, unit, err, bound.ok)
									}
								}
							}
						}
					}
				}
			})
		}
	}
}

func TestNullableRepetitionAfterConsumption(t *testing.T) {
	for _, tc := range []struct {
		input string
		min   int
		ok    bool
	}{{"", 2, false}, {"é", 2, true}, {"é", 3, false}, {"éé", 3, true}} {
		for _, body := range []string{`("é" / _)`, `("é" / _) #atomic`, `-("é" / _)`, `r`} {
			for _, bound := range []string{fmt.Sprintf("#repeat(%d)", tc.min), fmt.Sprintf("#repeat(%d, )", tc.min)} {
				p := mustCompile(t, "rule main = ("+body+")"+bound+" #stream $$\nrule r = \"é\" / _")
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						if _, err := p.ParseWith("main", tc.input, opts); (err == nil) != tc.ok {
							t.Errorf("%s%s/%q %s/%s: %v", body, bound, tc.input, backend, unit, err)
						}
						count := 0
						err := p.ParseStreamWith("main", strings.NewReader(tc.input), func(*value.Node) error { count++; return nil }, opts)
						wantCount := min(len([]rune(tc.input))+1, tc.min)
						if (err == nil) != tc.ok || count != wantCount {
							t.Errorf("stream %s%s/%q %s/%s: %d, %v; want %d success=%v", body, bound, tc.input, backend, unit, count, err, wantCount, tc.ok)
						}
					}
				}
			}
		}
	}
}

func TestNullableRepetitionResumedMinimum(t *testing.T) {
	const prefix = 1000
	p := mustCompile(t, fmt.Sprintf("rule main = item #repeat(%d, ) $$\nrule item = \"é\" / _", prefix+1))
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			d, err := p.NewDocumentWith("main", strings.Repeat("é", prefix), ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil || len(d.runs) == 0 {
				t.Fatalf("initial repetition not recorded: %v", err)
			}
			end := 1
			if unit == Bytes {
				end = len("é")
			}
			if err := d.Edit(0, end, ""); err != nil {
				t.Fatal(err)
			}
			got, err := d.Parse()
			want, werr := p.ParseWith("main", d.Text(), ParseOptions{Backend: backend, Unit: unit})
			if err == nil || resultJSON(got, err) != resultJSON(want, werr) {
				t.Errorf("resumed %s/%s: %v / %v", backend, unit, err, werr)
			}
			if d.resumed < prefix-10 {
				t.Errorf("tail was not reused: %d", d.resumed)
			}
			if err := d.Edit(0, 0, "é"); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Errorf("undo %s/%s: %v", backend, unit, err)
			}
		}
	}
}

func TestNullableRepetitionProjection(t *testing.T) {
	for _, minimum := range []int{1, 2} {
		p := mustCompile(t, fmt.Sprintf(`rule main = xs:(f:_) #repeat(%d, ) $$ => map($xs, (x) => x.$f)`, minimum))
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				if n, err := p.ParseWith("main", "", ParseOptions{Backend: backend, Unit: unit}); (err == nil) != (minimum == 1) || err == nil && len(value.D(n).Children) != 1 {
					t.Errorf("projected min=%d %s/%s: %v, %v", minimum, backend, unit, n, err)
				}
			}
		}
	}
}

func TestNullableRepetitionAnalysis(t *testing.T) {
	for _, minimum := range []int{0, 1, 2, 3} {
		g, err := parseTest(fmt.Sprintf("rule main = _ #repeat(%d, )", minimum))
		if err != nil {
			t.Fatal(err)
		}
		if got := compile.Analyze(g.Rules()).Nullable["main"]; got != (minimum <= 1) {
			t.Errorf("minimum %d: nullable=%v", minimum, got)
		}
		g, err = parseTest(fmt.Sprintf("rule main = _ #repeat(%d, ) r / \"a\"\nrule r = main", minimum))
		if err != nil {
			t.Fatal(err)
		}
		if got := compile.Analyze(g.Rules()).Cyclic["main"]; got != (minimum <= 1) {
			t.Errorf("minimum %d: left-call cycle=%v", minimum, got)
		}
	}
}

func BenchmarkNullableRepetitionControl(b *testing.B) {
	for _, tc := range []struct{ name, source string }{
		{"Ordinary", `rule main = item #repeat(2, ) $$
rule item = "a"`},
		{"NullableTail", `rule main = item #repeat(2, ) $$
rule item = "a" / _`},
		{"Projected", `rule main = xs:(f:"a" / f:_) #repeat(2, ) $$ => map($xs, (x) => x.$f)`},
	} {
		g, err := parseTest(strings.ReplaceAll(tc.source, "; ", "\n"))
		if err != nil {
			b.Fatal(err)
		}
		p, err := Compile(g, compile.Options{})
		if err != nil {
			b.Fatal(err)
		}
		input := strings.Repeat("a", 1000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/%s/recognize=%v", tc.name, backend, recognize), func(b *testing.B) {
					opts := ParseOptions{Backend: backend, Recognize: recognize}
					if _, err := p.ParseWith("main", input, opts); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(input)))
					for b.Loop() {
						if _, err := p.ParseWith("main", input, opts); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
