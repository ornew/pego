package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// A name captured several times in one match has the join of the types of its captures: the
// type of the capture x in the value of main, or the type of main's value when it has an action.
func TestDuplicateCaptureTypes(t *testing.T) {
	t.Parallel()
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
			var errs compile.ErrorList
			compile.CheckTypes(p.Static(), &errs)
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

func captureCheckProgram(src string) (*Program, error) {
	g, err := parseTest(src)
	if err != nil {
		return nil, err
	}
	p := newProgram(&compile.Program{Grammar: g, Types: map[string]grammar.TypeDecl{}}, nil)
	for _, d := range g.Types() {
		p.Types[d.DeclName()] = d
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
			g, err := parseTest(string(src))
			if err != nil {
				b.Fatal(err)
			}
			// The examples need the analysis of a full compile, which a hand-built program lacks.
			p, err := Compile(g, compile.Options{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var errs compile.ErrorList
				compile.CheckTypes(p.Static(), &errs)
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
					var errs compile.ErrorList
					compile.CheckTypes(p.Static(), &errs)
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
			var errs compile.ErrorList
			compile.CheckTypes(p.Static(), &errs)
			if len(errs) != 0 {
				b.Fatal(errs)
			}
		}
	})
}
