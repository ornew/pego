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

// A name captured several times in one match has the join of the types of its captures: the
// type of the capture x in the value of main, or the type of main's value when it has an action.
func TestDuplicateCaptureTypes(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		action    bool
		want, err string
	}{
		{"nested records", captureCheckSource(1024, "record"), false, "item", ""},
		{"mixed optional", captureCheckSource(32, "mixed"), false, "*(a | b)", ""},
		{"recursive", captureCheckSource(64, "recursive"), false, "main", ""},
		{"choice", `rule main = (x:k:"a" x:k:"b" / x:k:"c") $$`, false, "node[string]", ""},
		{"optional scope", `rule main = (x:"a" x:"b")? $$`, false, "*node[string]", ""},
		{"different types", "rule item = k:\"a\" v:(y:\"b\")*\nrule main = x:\"a\" x:item $$", false, "node[string] | item", ""},
		// A capture that always matches leaves the name set whether or not a later one matches.
		{"always set", `rule main = x:"a" (x:"b")? $$`, false, "node[string]", ""},
		{"always set later", `rule main = (x:"a")? x:"b" $$`, false, "node[string]", ""},
		{"set to nil", `rule main = x:"a" x:"b"? $$`, false, "*node[string]", ""},
		{"never set", `rule main = (x:"a")? (x:"b")? $$`, false, "*node[string]", ""},
		{"always set access", "type R = { X node[string] }\nrule main = x:\"a\" (x:\"b\")? $$ => R{X: $x}", true, "R", ""},
		{"repetition scope", `rule main = v1:(x:"a" x:"b")* $$ => $v1`, true, `node[[]node[(node[string], node[string]), {"x": 0 | 1}]]`, ""},
		{"nil action", "rule empty = \"\" => nil\nrule main = x:empty x:empty", false, "empty", ""},
		{"action and predicate", `rule main = x:"a" x:"b" {text($x) == "b"} => $x`, true, "node[string]", ""},
		{"incompatible action", "type R = { X node[string] }\nrule item = k:\"a\" v:(y:\"b\")*\nrule main = x:item x:item => R{X: $x}", true, "R", "3:32: cannot use node[(node[string], node[[]node[(node[string],), {\"y\": 0}]]), {\"k\": 0, \"v\": 1}] as node[string] in field X of R"},
		{"later capture", "type R = { X node[string] }\nrule item = k:\"a\" v:(y:\"b\")*\nrule main = x:\"a\" x:item => R{X: $x}", true, "R", "3:31: cannot use node[string] | node[(node[string], node[[]node[(node[string],), {\"y\": 0}]]), {\"k\": 0, \"v\": 1}] as node[string] in field X of R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := captureCheckProgram(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			var errs ErrorList
			checkTypes(p, &errs)
			got, ok := captureType(p, "main", "x")
			if tc.action {
				got, ok = inferredType(p, "main")
			}
			if !ok || got != tc.want {
				t.Errorf("type: got %q, %v; want %q", got, ok, tc.want)
			}
			if got := errs.Error(); got != tc.err {
				t.Errorf("errors: got %q; want %q", got, tc.err)
			}
		})
	}
	check(t, `rule main = x:"a" x:"b" {text($x) == "b"} => $x`, ok("ab", `"b"`))
}

// Compare the capture merger with the union, including values whose top-level optional/union
// normalization must not be skipped.
func TestCaptureUnionEquivalence(t *testing.T) {
	str := tyStringNode
	tuple := nodeTy{value: tupleTy{[]ty{str, str}}}
	ts := []ty{tyAny, tyBottom, tyNil, str, tyInt, tyString, namedTy{"A", 's'}, listTy{str},
		optTy{str}, optTy{optTy{str}}, unionTy{[]ty{str, tuple}}, unionTy{[]ty{tuple, str}},
		unionTy{[]ty{str, str, tyBottom}}, unionTy{[]ty{tyAny, str}}, listTy{optTy{str}},
		nodeTy{value: listTy{unionTy{[]ty{str, tuple}}}}}
	for i, a := range ts {
		for j, b := range ts {
			got := mergeCap([]capIndex{{name: "x", paths: [][]int{{0}}, t: a}}, capIndex{name: "x", paths: [][]int{{1}}, t: b})
			if want := vunion(a, b); len(got) != 1 || got[0].t.String() != want.String() || len(got[0].paths) != 2 {
				t.Fatalf("pair %d/%d: got %v, want %s", i, j, got, want)
			}
		}
	}
}

func TestDuplicateCaptureDiagnosticPositions(t *testing.T) {
	for _, n := range []int{1, 64, 1024} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			line := `rule main = ` + strings.Repeat(`"😀" x:item `, n) + `=> R{X: $x}`
			src := "type R = { X node[string] }\nrule item = k:\"a\" v:(y:\"b\")*\n" + line
			col := utf8.RuneCountInString(line[:strings.Index(line, "X:")]) + 1
			want := fmt.Sprintf("3:%d: cannot use node[(node[string], node[[]node[(node[string],), {\"y\": 0}]]), {\"k\": 0, \"v\": 1}] as node[string] in field X of R", col)
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
		return `rule main = ` + strings.Repeat(`"😀" x:main `, n)
	case "record":
		return `rule main = ` + strings.Repeat(`x:item `, n) + `$$
rule item = k:"a" v:(y:"b")*`
	case "mixed":
		return `type A = {}
type B = {}
rule a = "a" => A{}
rule b = "b" => B{}
rule main = ` + strings.Repeat(`x:a x:b? `, n) + `$$`
	case "unique":
		var s strings.Builder
		s.WriteString("rule main = ")
		for i := range n {
			fmt.Fprintf(&s, "x%d:item ", i)
		}
		s.WriteString("$$\nrule item = k:\"a\"\n")
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
