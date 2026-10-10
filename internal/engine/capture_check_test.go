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

func TestDuplicateCaptureTypes(t *testing.T) {
	for _, tc := range []struct{ name, src, want, err string }{
		{"nested records", captureCheckSource(1024, "record"), "Seq{x: Seq{k: Match, v: []Seq{y: Match}}}", ""},
		{"mixed optional", captureCheckSource(32, "mixed"), "Seq{x: *(A | B)}", ""},
		{"recursive", captureCheckSource(64, "recursive"), "any", ""},
		{"choice", `def main = (x:(k:"a") x:(k:"b") / x:(k:"c")) $$`, "Seq{k: Match, x: Match}", ""},
		{"optional scope", `def main = (x:"a" x:"b")? $$`, "Seq{x: *Match}", ""},
		{"repetition scope", `def main = (x:"a" x:"b")* $$ -> $1`, "[]Seq{x: Match}", ""},
		{"nil action", "def empty = \"\" -> nil\ndef main = x:empty x:empty", "Seq{x: nil}", ""},
		{"action and predicate", `def main = x:"a" x:"b" [text($x)=="b"] -> $x`, "Match", ""},
		{"incompatible action", "type R struct { X Match }\ndef item = k:\"a\" v:(y:\"b\")*\ndef main = x:item x:item -> new R{X: $x}", "R", "3:35: cannot use Seq{k: Match, v: []Seq{y: Match}} as Match in field X of R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := captureCheckProgram(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			var errs ErrorList
			checkTypes(p, &errs)
			got, ok := p.RuleType("main")
			if !ok || got != tc.want {
				t.Errorf("type: got %q, %v; want %q", got, ok, tc.want)
			}
			if got := errs.Error(); got != tc.err {
				t.Errorf("errors: got %q; want %q", got, tc.err)
			}
		})
	}
	check(t, `def main = x:"a" x:"b" [text($x)=="b"] -> $x`, ok("ab", `"b"`))
}

// Compare the capture merger with the general union, including values whose
// top-level optional/union normalization must not be skipped.
func TestCaptureUnionEquivalence(t *testing.T) {
	record := func(t ty) ty { return recordTy{map[string]ty{"x": t, "y": listTy{tyMatch}}} }
	ts := []ty{nil, tyAny, tyNever, tyNil, tyMatch, tyInt, tyString,
		namedTy{"A", 's'}, namedTy{"A", 't'}, listTy{tyMatch},
		record(tyMatch), record(tyMatch), record(tyString),
		optTy{tyMatch}, optTy{optTy{tyMatch}}, optTy{tyNever},
		unionTy{[]ty{tyMatch, tySeq}}, unionTy{[]ty{tySeq, tyMatch}},
		unionTy{[]ty{tyMatch, tyMatch, tyNever}}, unionTy{[]ty{tyAny, tyMatch}},
		record(unionTy{[]ty{tyMatch, tySeq}}),
		record(unionTy{[]ty{tySeq, tyMatch}}), listTy{optTy{tyMatch}},
	}
	for i, a := range ts {
		for j, b := range ts {
			for _, flags := range [][2]bool{{false, false}, {false, true}, {true, false}, {true, true}} {
				sc := &scopeCheck{caps: caps{"x": {t: a, always: flags[0]}}}
				(&checker{}).addCapture(sc, "x", capInfo{t: b, always: flags[1]})
				got, want := sc.caps["x"], union(a, b)
				if inferredShape(got.t).key != inferredShape(want).key || got.t.String() != want.String() || got.always != (flags[0] || flags[1]) {
					t.Fatalf("pair %d/%d flags %v: got %s/%v, want %s", i, j, flags, got.t, got.always, want)
				}
			}
		}
	}
}

func TestDuplicateCaptureDiagnosticPositions(t *testing.T) {
	for _, n := range []int{1, 64, 1024} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			line := `def main = ` + strings.Repeat(`"😀" x:item `, n) + `-> new R{X: $x}`
			src := "type R struct { X Match }\ndef item = k:\"a\" v:(y:\"b\")*\n" + line
			col := utf8.RuneCountInString(line[:strings.Index(line, "X:")]) + 1
			want := fmt.Sprintf("3:%d: cannot use Seq{k: Match, v: []Seq{y: Match}} as Match in field X of R", col)
			if got := compileError(t, src); got != want {
				t.Errorf("got %q; want %q", got, want)
			}
		})
	}
}

func captureCheckProgram(src string) (*Program, error) {
	g, err := syntax.Parse(src)
	if err != nil {
		return nil, err
	}
	p := &Program{Grammar: g, types: map[string]grammar.TypeSpec{}}
	for _, d := range g.Types() {
		p.types[d.Name] = d.Spec
	}
	return p, nil
}

func captureCheckSource(n int, shape string) string {
	switch shape {
	case "recursive":
		return `def main = ` + strings.Repeat(`"😀" x:main `, n)
	case "record":
		return `def main = ` + strings.Repeat(`x:item `, n) + `$$
def item = k:"a" v:(y:"b")*`
	case "mixed":
		return `type A struct {}
type B struct {}
def a = "a" -> new A{}
def b = "b" -> new B{}
def main = ` + strings.Repeat(`x:a x:b? `, n) + `$$`
	case "unique":
		var s strings.Builder
		s.WriteString("def main = ")
		for i := range n {
			fmt.Fprintf(&s, "x%d:item ", i)
		}
		s.WriteString("$$\ndef item = k:\"a\"\n")
		return s.String()
	}
	panic(shape)
}

func BenchmarkCaptureChecking(b *testing.B) {
	for _, path := range []string{"../../examples/calculator/calc.pego", "../../examples/minilang/minilang.pego", "../../parsers/json/json.pego"} {
		b.Run("control/"+path[strings.LastIndex(path, "/")+1:], func(b *testing.B) {
			src, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			p, err := captureCheckProgram(string(src))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var errs ErrorList
				checkTypes(p, &errs)
				if len(errs) != 0 {
					b.Fatal(errs)
				}
			}
		})
	}
	for _, shape := range []string{"recursive", "record", "mixed", "unique"} {
		for _, n := range []int{1024, 8192, 32768} {
			b.Run(fmt.Sprintf("%s/%d", shape, n), func(b *testing.B) {
				p, err := captureCheckProgram(captureCheckSource(n, shape))
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					var errs ErrorList
					checkTypes(p, &errs)
					if len(errs) != 0 {
						b.Fatal(errs)
					}
				}
			})
		}
	}
	b.Run("recursive/131072", func(b *testing.B) {
		p, err := captureCheckProgram(captureCheckSource(131072, "recursive"))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			var errs ErrorList
			checkTypes(p, &errs)
			if len(errs) != 0 {
				b.Fatal(errs)
			}
		}
	})
}
