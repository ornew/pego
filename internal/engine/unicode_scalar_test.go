package engine

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestInvalidCharacterClassEntryPoints(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testInvalidCharacterClassEntryPoints)
}

func testInvalidCharacterClassEntryPoints(t *testing.T) {
	for _, cr := range []grammar.CharRange{{Lo: -1, Hi: 'z'}, {Lo: 'a', Hi: 0xd800}, {Lo: 0xdfff, Hi: utf8.MaxRune}, {Lo: 0, Hi: utf8.MaxRune + 1}, {Lo: 'z', Hi: 'a'}} {
		t.Run(fmt.Sprintf("%x-%x", cr.Lo, cr.Hi), func(t *testing.T) {
			cc := &grammar.CharClass{SourceInfo: grammar.SourceInfo{Pos: grammar.Pos{Line: 3, Col: 7}}, Ranges: []grammar.CharRange{cr}}
			g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: cc}}}}
			for name, run := range map[string]func() error{
				"compile":           func() error { _, err := Compile(g, compile.Options{}); return err },
				"saved AST rebuild": func() error { _, err := build(g, compile.Options{}, []compile.RuleFlags{{}}); return err },
				"Go generator": func() error {
					_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p"})
					return err
				},
				"typed Go generator": func() error {
					_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p", Types: true})
					return err
				},
				"TS generator": func() error { _, err := generateTSBundle(g, gen.GenOptions{Start: "main"}); return err },
			} {
				t.Run(name, func(t *testing.T) {
					err := run()
					if errs, ok := err.(compile.ErrorList); !ok || len(errs) != 1 || errs[0].Pos != cc.Pos || !strings.Contains(err.Error(), "$.file.decls[0].expr.ranges[0]") {
						t.Fatalf("got %v, want positioned range error", err)
					}
				})
			}
			// A compiled file holds the module alone: a malformed AST beside a valid module
			// does not reach the file, which loads as the module's own.
			p := mustCompile(t, `rule main = [a-z]`)
			want, err := p.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			p.Grammar = g
			data, err := p.MarshalBinary("main")
			if err != nil || !bytes.Equal(data, want) {
				t.Fatalf("compiled file depends on the AST: %v", err)
			}
			if loaded, _, err := LoadProgram(data); err != nil || loaded.Grammar != nil {
				t.Fatalf("load: %v", err)
			}
		})
	}
}

func TestUnicodeScalarRangeBackends(t *testing.T) {
	t.Parallel()
	for _, source := range []string{`rule main = [\u{0}-\u{10ffff}] $$`, `rule main = [\u{d7ff}-\u{e000}] $$`, `rule main = [^\u{d7ff}-\u{e000}] $$`} {
		p := mustCompile(t, source)
		g := mustParse(source)
		cc := g.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.CharClass)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{Bytes, CodePoints} {
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
			g, err := parseTest(string(source))
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
					if _, err := Compile(g, compile.Options{}); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
