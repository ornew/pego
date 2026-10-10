package engine

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// Replace a source-valid placeholder with a public-AST value. Only matching
// expressions are visited: string constants in actions have separate semantics.
func firstLiteral(e grammar.Expr) *grammar.Literal {
	switch e := e.(type) {
	case *grammar.Literal:
		return e
	case *grammar.Seq:
		for _, item := range e.Items {
			if lit := firstLiteral(item); lit != nil {
				return lit
			}
		}
	case *grammar.Choice:
		for _, item := range e.Alts {
			if lit := firstLiteral(item); lit != nil {
				return lit
			}
		}
	case *grammar.Capture:
		return firstLiteral(e.Expr)
	case *grammar.Atomic:
		return firstLiteral(e.Expr)
	case *grammar.Repeat:
		return firstLiteral(e.Expr)
	}
	return nil
}

func replaceFirstLiteral(e grammar.Expr, value string) *grammar.Literal {
	lit := firstLiteral(e)
	if lit != nil {
		lit.Value = value
	}
	return lit
}

func TestRawInvalidASTLiterals(t *testing.T) {
	for _, raw := range rawLiteralValues() {
		for _, shape := range []string{`t:"�"`, `t:"�" .`, `t:("�" / "other")`, `t:@("�"{2})`} {
			t.Run(fmt.Sprintf("%x/%s", raw, shape), func(t *testing.T) {
				src := "type N struct { T Match, S string }\ndef main:N = " + shape + ` $$ -> new N{T:$t, S:"constant"}`
				g := mustParse(src)
				lit := replaceFirstLiteral(g.Rules()[0].Expr, raw)
				if lit == nil {
					t.Fatal("missing placeholder")
				}
				// Share the raw string with an action constant/string-table entry. VM
				// preparation must normalize matching data without changing action values.
				g.Rules()[0].Action.(*grammar.New).Fields[1].Value.(*grammar.StringLit).Value = raw
				p, err := Compile(g, Options{})
				if err != nil {
					t.Fatal(err)
				}
				if lit.Value != raw {
					t.Fatal("Compile mutated the AST")
				}
				canonical := mustParse(src)
				replaceFirstLiteral(canonical.Rules()[0].Expr, string([]rune(raw)))
				canonical.Rules()[0].Action.(*grammar.New).Fields[1].Value.(*grammar.StringLit).Value = raw
				want, err := Compile(canonical, Options{})
				if err != nil {
					t.Fatal(err)
				}
				programs := map[string]*Program{"AST": p}
				for _, omit := range []bool{false, true} {
					data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
					if err != nil {
						t.Fatal(err)
					}
					loaded, _, err := LoadProgram(data, Options{})
					if err != nil {
						t.Fatal(err)
					}
					again, err := loaded.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
					if err != nil || !bytes.Equal(data, again) {
						t.Fatalf("module bytes changed: %v", err)
					}
					programs[fmt.Sprintf("saved/omitAST=%t", omit)] = loaded
				}
				v1, _, err := LoadProgram(marshalV1(p, "main"), Options{})
				if err != nil {
					t.Fatal(err)
				}
				programs["v1"] = v1
				for name, prog := range programs {
					for _, backend := range []Backend{Default, Closure, Bytecode, BytecodeIterative} {
						if prog.Grammar == nil && backend == Closure {
							continue
						}
						for _, unit := range []Unit{CodePoints, Bytes} {
							for _, input := range []string{raw, string([]rune(raw)), raw + "x", string([]rune(raw)) + "x", raw + raw, string([]rune(raw)) + string([]rune(raw)), "é", "éx", "other", "", "\xffx", "\x80x"} {
								opts := ParseOptions{Backend: backend, Unit: unit}
								n, err := prog.ParseWith("main", input, opts)
								wn, we := want.ParseWith("main", input, opts)
								if (err == nil) != (we == nil) {
									t.Errorf("%s %s/%s input=%x: %v, want success=%t", name, backend, unit, input, err, we == nil)
									continue
								}
								if prog.Grammar != nil {
									opts.Recognize = true
									_, re := prog.ParseWith("main", input, opts)
									if fmt.Sprint(re) != fmt.Sprint(err) {
										t.Errorf("%s recognition differs: %v / %v", name, re, err)
									}
								}
								if err == nil {
									// JSON replaces malformed strings, so compare raw match text too.
									gotMatch, wantMatch := n.Field("T").(*Node), wn.Field("T").(*Node)
									if gotMatch.Text != wantMatch.Text || gotMatch.Start != wantMatch.Start || gotMatch.End != wantMatch.End {
										t.Errorf("%s %s/%s input=%x: match %x [%d,%d), want %x [%d,%d)", name, backend, unit, input, gotMatch.Text, gotMatch.Start, gotMatch.End, wantMatch.Text, wantMatch.Start, wantMatch.End)
									}
									if got, w := resultJSON(n, nil), resultJSON(wn, nil); got != w {
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
						if first := firstLiteral(prog.Grammar.Rules()[0].Expr); first == nil || first.Value != raw {
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

func rawLiteralValues() []string {
	return []string{"\xc3", "\x80", "\xff", "\xc0\xaf", "\xed\xa0\x80", "é\xc3x", "abcdefghijk\xffxyz"}
}

func rawLiteralGenCorpus() []genCase {
	var cases []genCase
	for _, raw := range rawLiteralValues() {
		canonical := string([]rune(raw))
		for _, shape := range []string{"exact", "suffix", "choice", "predicate"} {
			expr := strconv.Quote(canonical)
			predicate := ""
			switch shape {
			case "suffix":
				expr = "@(" + expr + " .)"
			case "predicate":
				predicate = " [text($t)==" + strconv.Quote(canonical) + "]"
			case "choice":
				expr = "(" + expr + ` / "other")`
			}
			cases = append(cases, genCase{fmt.Sprintf("raw AST/%x/%s", raw, shape),
				"type N struct { T Match }\ndef main:N = t:" + expr + predicate + " $$ -> new N{T:$t}",
				[]string{raw, canonical, raw + "x", canonical + "x", "é", "éx", "other", "", "\xffx", "\x80x"}})
		}
	}
	return cases
}

func literalCorpusGrammar(t *testing.T, tc genCase) *grammar.Grammar {
	t.Helper()
	g := mustParse(tc.src)
	if strings.HasPrefix(tc.name, "raw AST/") {
		for _, raw := range rawLiteralValues() {
			if strings.HasPrefix(tc.name, fmt.Sprintf("raw AST/%x/", raw)) {
				if replaceFirstLiteral(g.Rules()[0].Expr, raw) == nil {
					t.Fatal("missing literal")
				}
				return g
			}
		}
		t.Fatal("unknown raw literal case")
	}
	return g
}
