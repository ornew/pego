package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func TestInvalidCharacterClassEntryPoints(t *testing.T) {
	for _, cr := range []grammar.CharRange{{Lo: -1, Hi: 'z'}, {Lo: 'a', Hi: 0xd800}, {Lo: 0xdfff, Hi: utf8.MaxRune}, {Lo: 0, Hi: utf8.MaxRune + 1}, {Lo: 'z', Hi: 'a'}} {
		t.Run(fmt.Sprintf("%x-%x", cr.Lo, cr.Hi), func(t *testing.T) {
			cc := &grammar.CharClass{Pos: grammar.Pos{Line: 3, Col: 7}, Ranges: []grammar.CharRange{cr}}
			g := &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{Name: "main", Expr: cc}}}
			for name, run := range map[string]func() error{
				"compile":            func() error { _, err := Compile(g, Options{}); return err },
				"unchecked compile":  func() error { _, err := Compile(g, Options{NoTypeCheck: true}); return err },
				"saved AST rebuild":  func() error { _, err := build(g, Options{}, []ruleFlags{{}}); return err },
				"Go generator":       func() error { _, err := Generate(g, GenOptions{Start: "main", Package: "p"}); return err },
				"typed Go generator": func() error { _, err := Generate(g, GenOptions{Start: "main", Package: "p", Types: true}); return err },
				"TS generator":       func() error { _, err := GenerateTS(g, GenOptions{Start: "main"}); return err },
			} {
				t.Run(name, func(t *testing.T) {
					err := run()
					if errs, ok := err.(ErrorList); !ok || len(errs) != 1 || errs[0].Pos != cc.Pos || !strings.Contains(err.Error(), "$.statements[0].expr.ranges[0]") {
						t.Fatalf("got %v, want positioned range error", err)
					}
				})
			}
			// Encode a malformed AST alongside an otherwise valid module to
			// verify the actual compiled-file intake, not just its rebuild helper.
			p := compile(t, `def main=(?a-z)`)
			p.Grammar = g
			data, err := p.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			if loaded, _, err := LoadProgram(data, Options{}); loaded != nil || err == nil || !strings.Contains(err.Error(), "ranges[0]") {
				t.Fatalf("compiled intake accepted invalid AST: %v, %v", loaded != nil, err)
			}
		})
	}
}

func TestUnicodeScalarRangeBackends(t *testing.T) {
	for _, source := range []string{`def main=(?\u0000-\u{10FFFF}) $$`, `def main=(?\uD7FF-\uE000) $$`, `def main=(?^\uD7FF-\uE000) $$`} {
		p := compile(t, source)
		g := mustParse(source)
		cc := g.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.CharClass)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{Bytes, CodePoints} {
				for _, r := range []rune{0, 'a', 0xd7fe, 0xd7ff, 0xe000, 0xe001, 0xfffd, 0xffff, 0x10000, utf8.MaxRune} {
					want := r >= cc.Ranges[0].Lo && r <= cc.Ranges[0].Hi
					if cc.Negated {
						want = !want
					}
					for _, recognize := range []bool{false, true} {
						_, err := p.ParseWith("main", string(r), ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
						if (err == nil) != want {
							t.Fatalf("%s %s %U recognize=%v: %v, want accepted=%v", backend, unit, r, recognize, err, want)
						}
					}
				}
			}
		}
	}
}

func BenchmarkCharacterRangeValidation(b *testing.B) {
	for _, tc := range []struct{ name, path string }{{"JSON", "../../parsers/json/json.pego"}, {"Go", "../../parsers/golang/golang.pego"}, {"YAML", "../../parsers/yaml/yaml.pego"}} {
		b.Run(tc.name, func(b *testing.B) {
			path := tc.path
			if tc.name == "YAML" && os.Getenv("PEGO_BENCH_YAML_GRAMMAR") != "" {
				path = os.Getenv("PEGO_BENCH_YAML_GRAMMAR")
			}
			source, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			g, err := syntax.Parse(string(source))
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Validate", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := grammar.Validate(g); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Compile", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := Compile(g, Options{}); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
