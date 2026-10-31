package engine

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

func TestRawInvalidASTLiterals(t *testing.T) {
	t.Parallel()
	for _, raw := range gentest.RawLiteralValues() {
		for _, shape := range []string{`t:"�"`, `t:"�" .`, `t:("�" / "other")`, `t:("�" #repeat(2)) #atomic`} {
			t.Run(fmt.Sprintf("%x/%s", raw, shape), func(t *testing.T) {
				src := "type N = { T node[string]; S string }\nrule main N = " + shape + ` $$ => N{T:$t, S:"constant"}`
				g := mustParse(src)
				lit := gentest.ReplaceFirstLiteral(g.Rules()[0].Expr, raw)
				if lit == nil {
					t.Fatal("missing placeholder")
				}
				// Share the raw string with an action constant/string-table entry. VM
				// preparation must normalize matching data without changing action values.
				g.Rules()[0].Action.(*grammar.New).Fields[1].Value.(*grammar.StringLit).Value = raw
				p, err := Compile(g, compile.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if lit.Value != raw {
					t.Fatal("Compile mutated the AST")
				}
				canonical := mustParse(src)
				gentest.ReplaceFirstLiteral(canonical.Rules()[0].Expr, string([]rune(raw)))
				canonical.Rules()[0].Action.(*grammar.New).Fields[1].Value.(*grammar.StringLit).Value = raw
				want, err := Compile(canonical, compile.Options{})
				if err != nil {
					t.Fatal(err)
				}
				programs := map[string]*Program{"AST": p}
				modes := map[string]CompiledMode{}
				var recognizer *Program // the program loaded in recognize mode
				for _, mode := range allModes {
					data, err := p.Marshal("main", mode)
					if err != nil {
						t.Fatal(err)
					}
					loaded, _, err := LoadProgram(data)
					if err != nil {
						t.Fatal(err)
					}
					again, err := loaded.Marshal("main", mode)
					if err != nil || !bytes.Equal(data, again) {
						t.Fatalf("module bytes changed: %v", err)
					}
					if mode == CompiledRecognize {
						recognizer = loaded
						continue
					}
					name := "saved/" + mode.String()
					programs[name], modes[name] = loaded, mode
				}
				for name, prog := range programs {
					for _, backend := range []Backend{Default, Closure, Bytecode, BytecodeIterative} {
						if prog.Grammar == nil && backend == Closure {
							_, err := prog.ParseWith("main", raw, servedBy(modes[name], ParseOptions{Backend: backend}))
							wantClosureRefused(t, err)
							continue
						}
						for _, unit := range []PositionUnit{CodePoints, Bytes} {
							for _, input := range []string{raw, string([]rune(raw)), raw + "x", string([]rune(raw)) + "x", raw + raw, string([]rune(raw)) + string([]rune(raw)), "é", "éx", "other", "", "\xffx", "\x80x"} {
								opts := ParseOptions{Backend: backend, Unit: unit}
								n, err := prog.ParseWith("main", input, servedBy(modes[name], opts))
								wn, we := want.ParseWith("main", input, opts)
								if (err == nil) != (we == nil) {
									t.Errorf("%s %s/%s input=%x: %v, want success=%t", name, backend, unit, input, err, we == nil)
									continue
								}
								{
									recognizing := prog
									if prog.Grammar == nil {
										recognizing = recognizer
									}
									opts.Recognize = true
									_, re := recognizing.ParseWith("main", input, opts)
									if fmt.Sprint(re) != fmt.Sprint(err) {
										t.Errorf("%s recognition differs: %v / %v", name, re, err)
									}
								}
								if err == nil {
									// JSON replaces malformed strings, so compare raw match text too.
									gotMatch, wantMatch := n.Field("T").(*value.Node), wn.Field("T").(*value.Node)
									if value.D(gotMatch).Text != value.D(wantMatch).Text || value.D(gotMatch).Start != value.D(wantMatch).Start || value.D(gotMatch).End != value.D(wantMatch).End {
										t.Errorf("%s %s/%s input=%x: match %x [%d,%d), want %x [%d,%d)", name, backend, unit, input, value.D(gotMatch).Text, value.D(gotMatch).Start, value.D(gotMatch).End, value.D(wantMatch).Text, value.D(wantMatch).Start, value.D(wantMatch).End)
									}
									if got, w := gentest.ResultJSON(n, nil), gentest.ResultJSON(wn, nil); got != w {
										t.Errorf("%s %s/%s input=%x\ngot %s\nwant %s", name, backend, unit, input, got, w)
									}
									if n.Field("S") != raw {
										t.Errorf("%s action constant changed", name)
									}
								}
							}
						}
					}
					if prog.Grammar != nil {
						if first := gentest.FirstLiteral(prog.Grammar.Rules()[0].Expr); first == nil || first.Value != raw {
							t.Fatal("loaded AST changed literal bytes")
						}
					}
				}
				if lit.Value != raw {
					t.Fatal("serialization or execution mutated caller AST")
				}
				if !strings.Contains(strings.Join(p.Module().Strings, "\n"), raw) {
					t.Fatal("module lost original raw string")
				}
			})
		}
	}
}
