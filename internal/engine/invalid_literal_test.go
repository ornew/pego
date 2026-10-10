package engine

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/ornew/pego/internal/syntax"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestInvalidUTF8LiteralMatching(t *testing.T) {
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
			for _, unit := range []Unit{CodePoints, Bytes} {
				t.Run(fmt.Sprintf("%q/%x/%s/%s", tc.literal, tc.input, backend, unit), func(t *testing.T) {
					p := compile(t, "def main="+fmt.Sprintf("%q", tc.literal)+" $$")
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
						if n.Children[0].Text != wantText {
							t.Fatalf("literal text=%q want %q", n.Children[0].Text, wantText)
						}
						if n.End != int32(wantEnd) {
							t.Fatalf("end=%d want %d", n.End, wantEnd)
						}
					}
				})
			}
		}
	}
}

func TestReplacementLiteralTextPredicate(t *testing.T) {
	prog := compile(t, `def main=c:"�" [text($c)=="�"] $$`)
	for _, input := range []string{"\xff", "\x80", "\xc3", "�"} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{Bytes, CodePoints} {
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
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, expr := range []string{`"�"`, `(?�)`, `"x" / "�"`, `"a�"`} {
				t.Run(fmt.Sprintf("%s/%s/%s", backend, unit, expr), func(t *testing.T) {
					p := compile(t, "def main=item* #stream $$\ndef item="+expr+` "\n"`)
					item := "\xff\n"
					if expr == `"a�"` {
						item = "a" + item
					}
					input := strings.Repeat(item, 1100)
					count := 0
					err := p.ParseStreamWith("main", strings.NewReader(input), func(n *Node) error {
						count++
						width := utf8.RuneCountInString(item)
						if unit == Bytes {
							width = len(item)
						}
						if n.Start != int32((count-1)*width) || n.End != int32(count*width) {
							t.Errorf("element %d range [%d,%d)", count, n.Start, n.End)
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

func invalidLiteralCorpus() []genCase {
	cases := []genCase{
		{"text predicate", `type N struct { T Match }
def main:N = t:"�" [len(text($t))==len($t)] $$ -> new N{T:$t}`, []string{"\xff", "\x80", "\xc3", "�", "é"}},
		{"replacement", `type N struct { T Match }
def main:N = t:"�" $$ -> new N{T:$t}`, []string{"\xff", "\x80", "\xc3", "�", "é", "", "\xffx"}},
		{"multiple", `type N struct { T Match }
def main:N = t:"���" $$ -> new N{T:$t}`, []string{"\xed\xa0\x80", "\xe0\x80\x80", "���", "\xff", "\xff\x80\x80x"}},
		{"mixed", `type N struct { T Match }
def main:N = t:"é�x" $$ -> new N{T:$t}`, []string{"é\xffx", "é\xc3x", "é�x", "é\xffy", "é\xff", "éx"}},
		{"long", `type N struct { T Match }
def main:N = t:"abcdefghijk�xyz" $$ -> new N{T:$t}`, []string{"abcdefghijk\xffxyz", "abcdefghijk�xyz", "abcdefghijk\xc3xyz", "abcdefghijk\xffxyq"}},
		{"choice", `type N struct { T Match }
def main:N = t:("x" / "�") $$ -> new N{T:$t}`, []string{"\xff", "\x80", "\xc3", "�", "x", "y"}},
		{"class", `type N struct { T Match }
def main:N = t:(?�) $$ -> new N{T:$t}`, []string{"\xff", "\x80", "\xc3", "�", "é"}},
	}
	return append(cases, rawLiteralGenCorpus()...)
}

// Go string literals preserve invalid bytes; JSON input fixtures would replace
// them before the generated parser sees them.
func TestGeneratedInvalidUTF8Literals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	dir := t.TempDir()
	write := func(name string, content []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module literaltest\n\ngo 1.24\n"))
	var imports, harness, want strings.Builder
	harness.WriteString(`func main(){`)
	i := 0
	for _, tc := range invalidLiteralCorpus() {
		g := literalCorpusGrammar(t, tc)
		prog, err := Compile(g, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"untyped", "direct", "convert"} {
			pkg := fmt.Sprintf("g%d", i)
			i++
			opts := GenOptions{Package: pkg, Start: "main", Recognize: true, Types: mode != "untyped", convertTypes: mode == "convert"}
			code, err := Generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			write(pkg+"/parser.go", code)
			fmt.Fprintf(&imports, "%s %q\n", pkg, "literaltest/"+pkg)
			for _, input := range tc.inputs {
				for _, unit := range []Unit{CodePoints, Bytes} {
					n, err := prog.ParseWith("main", input, ParseOptions{Unit: unit})
					want.WriteString(resultJSON(n, err) + "\n")
					fmt.Fprintf(&harness, "{ input:=%q; unit:=%s.%s; n,err:=%s.Parse(input,unit); rec:=%s.Recognize(input,unit); if fmt.Sprint(rec)!=fmt.Sprint(err){panic(\"recognition differs\")}\n", input, pkg, map[Unit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], pkg, pkg)
					if mode != "untyped" {
						fmt.Fprintf(&harness, `ast,ae:=%s.ParseAST(input,unit); if fmt.Sprint(ae)!=fmt.Sprint(err){panic("typed error differs")}; if err==nil {child:=n.Field("T").(*%s.Node); wantText:=input; if unit==%s.CodePoints{wantText=string([]rune(input))};if child.Text!=wantText{panic("matched text differs from input")}; if ast==nil || ast.T==nil || ast.T.Start!=int(child.Start) || ast.T.End!=int(child.End) || ast.T.Text!=child.Text {panic("typed literal value differs")}}`+"\n", pkg, pkg, pkg)
					}
					harness.WriteString(`out:=map[string]any{"node":n};if err!=nil{out["err"]=err.Error()};data,_:=json.Marshal(out);fmt.Println(string(data))}` + "\n")
				}
			}
		}
	}
	harness.WriteString("}\n")
	write("main.go", []byte("package main\nimport(\"encoding/json\";\"fmt\";\n"+imports.String()+")\n"+harness.String()))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != want.String() {
		t.Fatalf("generated Go: %v\ngot %s\nwant %s", err, out, want.String())
	}
}

// literalTextUnits computes JS text units from the documented byte-preserving
// surrogate convention, independently of the TypeScript matcher.
func literalTextUnits(input string, unit Unit) []rune {
	var units []rune
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if unit == Bytes && r == utf8.RuneError && size == 1 {
			units = append(units, 0xdc00+rune(input[i]))
		} else if r > 0xffff {
			r -= 0x10000
			units = append(units, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		} else {
			units = append(units, r)
		}
		i += size
	}
	return units
}

func TestGeneratedTSInvalidUTF8Literals(t *testing.T) {
	for _, tc := range invalidLiteralCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			g := literalCorpusGrammar(t, tc)
			prog, err := Compile(g, Options{})
			if err != nil {
				t.Fatal(err)
			}
			code, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true})
			if err != nil {
				t.Fatal(err)
			}
			var script, want strings.Builder
			script.WriteString(`import {parse,recognize,marshal,Node,CodePoints,Bytes} from "./parser.ts";` + "\n")
			for _, input := range tc.inputs {
				fmt.Fprintf(&script, `for (const unit of [CodePoints,Bytes]){const input=new Uint8Array(Buffer.from(%q,"base64"));const r=parse(input,unit);const rec=recognize(input,unit);if((rec?.message??null)!==(r.error?.message??null))throw new Error("recognition differs");const child=r.node?.field("T");const expected=unit===Bytes?String.fromCharCode(...%s):String.fromCharCode(...%s);if(!r.error && (!(child instanceof Node)||child.text!==expected))throw new Error("matched bytes differ");const out:Record<string,unknown>={};if(r.error)out["err"]=r.error.message;out["node"]=JSON.parse(marshal(r.node));console.log(JSON.stringify(out));}`+"\n", base64.StdEncoding.EncodeToString([]byte(input)), tsInts(literalTextUnits(input, Bytes)), tsInts(literalTextUnits(input, CodePoints)))
				for _, unit := range []Unit{CodePoints, Bytes} {
					n, err := prog.ParseWith("main", input, ParseOptions{Unit: unit})
					want.WriteString(resultJSON(n, err) + "\n")
				}
			}
			dir := t.TempDir()
			for name, data := range map[string][]byte{"package.json": []byte(`{"type":"module"}`), "parser.ts": code, "main.ts": []byte(script.String())} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			node, err := exec.LookPath("node")
			if err != nil {
				t.Skip("node not found")
			}
			cmd := exec.Command(node, "main.ts")
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil || string(out) != want.String() {
				t.Fatalf("generated TS: %v\n%s\ngot %s\nwant %s", err, stderr.String(), out, want.String())
			}
			if tsc, err := exec.LookPath("tsc"); err == nil {
				cmd := exec.Command(tsc, "--strict", "--target", "ES2022", "--module", "esnext", "--noEmit", "--skipLibCheck", "parser.ts")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("strict tsc: %v\n%s", err, out)
				}
			}
		})
	}
}

func TestOpenPipeInvalidUTF8Literal(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, input := range []string{"\xff\n", "\x80\n", "\xc3\n", "�\n"} {
				t.Run(fmt.Sprintf("%s/%s/%x", backend, unit, input), func(t *testing.T) {
					prog := compile(t, `def main=item* #stream $$
def item="�" "\n"`)
					pr, pw := io.Pipe()
					done := make(chan error, 1)
					emitted := make(chan *Node, 1)
					go func() {
						defer pr.Close()
						done <- prog.ParseStreamWith("main", pr, func(n *Node) error { emitted <- n; return nil }, ParseOptions{Backend: backend, Unit: unit})
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
						if n.End != int32(width) {
							t.Fatalf("end=%d want %d", n.End, width)
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
					g, err := syntax.Parse("def main=" + strconv.Quote(literal) + " $$")
					if err != nil {
						b.Fatal(err)
					}
					p, err := Compile(g, Options{})
					if err != nil {
						b.Fatal(err)
					}
					return p
				}()
				if _, err := prog.rule(backend, "main"); err != nil {
					b.Fatal(err)
				}
				for _, unit := range []Unit{Bytes, CodePoints} {
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
