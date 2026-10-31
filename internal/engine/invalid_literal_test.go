package engine

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/value"
)

func TestInvalidUTF8LiteralMatching(t *testing.T) {
	t.Parallel()
	cases := []struct {
		literal, input string
		ok             bool
	}{
		{"�", "\xff", true}, {"�", "\x80", true}, {"�", "\xc3", true},
		{"��", "\xc0\xaf", true}, {"���", "\xed\xa0\x80", true},
		{"���", "\xe0\x80\x80", true}, {"����", "\xf4\x90\x80\x80", true},
		{"é�x", "é\xffx", true}, {"é�x", "é\xc3x", true}, {"�é", "\xffé", true},
		{"�", "�", true}, {"é�", "é�", true},
		{"�", "é", false}, {"�", "\xff\x80", false}, {"��", "\xff", false},
		{"�x", "\xffy", false}, {"�x", "\xff", false}, {"é�x", "é\xffy", false},
	}
	for _, tc := range cases {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				t.Run(fmt.Sprintf("%q/%x/%s/%s", tc.literal, tc.input, backend, unit), func(t *testing.T) {
					p := mustCompile(t, "rule main = "+fmt.Sprintf("%q", tc.literal)+" $$")
					opts := ParseOptions{Backend: backend, Unit: unit}
					n, err := p.ParseWith("main", tc.input, opts)
					if (err == nil) != tc.ok {
						t.Fatalf("success=%v want %v: %v", err == nil, tc.ok, err)
					}
					opts.Recognize = true
					_, re := p.ParseWith("main", tc.input, opts)
					if fmt.Sprint(re) != fmt.Sprint(err) {
						t.Fatalf("recognize=%v parse=%v", re, err)
					}
					if tc.ok {
						wantEnd := utf8.RuneCountInString(tc.input)
						if unit == Bytes {
							wantEnd = len(tc.input)
						}
						wantText := tc.input
						if unit == CodePoints {
							wantText = string([]rune(tc.input))
						}
						if value.D(value.D(n).Children[0]).Text != wantText {
							t.Fatalf("literal text=%q want %q", value.D(value.D(n).Children[0]).Text, wantText)
						}
						if value.D(n).End != int32(wantEnd) {
							t.Fatalf("end=%d want %d", value.D(n).End, wantEnd)
						}
					}
				})
			}
		}
	}
}

func TestReplacementLiteralTextPredicate(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = c:"�" {text($c) == "�"} $$`)
	for _, input := range []string{"\xff", "\x80", "\xc3", "�"} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{Bytes, CodePoints} {
				for _, recognize := range []bool{false, true} {
					_, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
					want := unit == CodePoints || utf8.ValidString(input)
					if (err == nil) != want {
						t.Errorf("%s/%s recognize=%v input=%x: %v", backend, unit, recognize, input, err)
					}
				}
			}
		}
	}
}

func TestInvalidUTF8LiteralStream(t *testing.T) {
	t.Parallel()
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, expr := range []string{`"�"`, `[�]`, `"x" / "�"`, `"a�"`} {
				t.Run(fmt.Sprintf("%s/%s/%s", backend, unit, expr), func(t *testing.T) {
					p := mustCompile(t, "rule main = item* #stream $$\nrule item = "+expr+` "\n"`)
					item := "\xff\n"
					if expr == `"a�"` {
						item = "a" + item
					}
					input := strings.Repeat(item, 1100)
					count := 0
					err := p.ParseStreamWith("main", strings.NewReader(input), func(n *value.Node) error {
						count++
						width := utf8.RuneCountInString(item)
						if unit == Bytes {
							width = len(item)
						}
						if value.D(n).Start != int32((count-1)*width) || value.D(n).End != int32(count*width) {
							t.Errorf("element %d range [%d,%d)", count, value.D(n).Start, value.D(n).End)
						}
						return nil
					}, ParseOptions{Backend: backend, Unit: unit})
					if err != nil || count != 1100 {
						t.Fatalf("emitted %d: %v", count, err)
					}
				})
			}
		}
	}
}

func TestOpenPipeInvalidUTF8Literal(t *testing.T) {
	t.Parallel()
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range []string{"\xff\n", "\x80\n", "\xc3\n", "�\n"} {
				t.Run(fmt.Sprintf("%s/%s/%x", backend, unit, input), func(t *testing.T) {
					prog := mustCompile(t, `rule main = item* #stream $$
rule item = "�" "\n"`)
					pr, pw := io.Pipe()
					done := make(chan error, 1)
					emitted := make(chan *value.Node, 1)
					go func() {
						defer pr.Close()
						done <- prog.ParseStreamWith("main", pr, func(n *value.Node) error { emitted <- n; return nil }, ParseOptions{Backend: backend, Unit: unit})
					}()
					t.Cleanup(func() {
						pw.Close()
						if t.Failed() {
							pr.Close()
						}
						select {
						case err := <-done:
							if !t.Failed() && err != nil {
								t.Error(err)
							}
						case <-time.After(2 * time.Second):
							pr.Close()
							t.Error("stream did not stop")
						}
						pr.Close()
					})
					if _, err := io.WriteString(pw, input); err != nil {
						t.Fatal(err)
					}
					select {
					case n := <-emitted:
						width := len(input)
						if unit == CodePoints {
							width = utf8.RuneCountInString(input)
						}
						if value.D(n).End != int32(width) {
							t.Fatalf("end=%d want %d", value.D(n).End, width)
						}
					case <-time.After(time.Second):
						t.Fatal("literal waited for future input or EOF")
					}
				})
			}
		}
	}
}

func BenchmarkByteLiteralMatching(b *testing.B) {
	for _, literal := range []string{"keyword", "日本語", "�", "é�x"} {
		for _, match := range []bool{true, false} {
			input := literal
			if !match {
				rs := []rune(literal)
				rs[len(rs)-1] = '!'
				input = string(rs)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				prog := func() *Program {
					g, err := parseTest("rule main = " + strconv.Quote(literal) + " $$")
					if err != nil {
						b.Fatal(err)
					}
					p, err := Compile(g, compile.Options{})
					if err != nil {
						b.Fatal(err)
					}
					return p
				}()
				if _, err := prog.rule(backend, "main"); err != nil {
					b.Fatal(err)
				}
				for _, unit := range []PositionUnit{Bytes, CodePoints} {
					for _, recognize := range []bool{true, false} {
						b.Run(fmt.Sprintf("%x/%t/%s/%s/Recognize%t", literal, match, backend, unit, recognize), func(b *testing.B) {
							opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
							b.ReportAllocs()
							for b.Loop() {
								_, err := prog.ParseWith("main", input, opts)
								if (err == nil) != match {
									b.Fatal(err)
								}
							}
						})
					}
				}
			}
		}
	}
}
