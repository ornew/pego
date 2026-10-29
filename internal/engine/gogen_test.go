package engine

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
	"github.com/ornew/pego/internal/value"
)

func resultJSON(n *value.Node, err error) string {
	out := map[string]any{"node": n}
	if err != nil {
		out["err"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// TestGeneratedParsersMatchEngine checks that generated parsers return the same results as the engine:
// the same trees, positions included, and the same errors, and that Recognize returns the errors of
// the engine's recognition.
func TestGeneratedParsersMatchEngine(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedParsersMatchEngine)
}

func testGeneratedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := gentest.GenCorpus(t)
	// Exactly at and just beyond the default nesting limit (DefaultMaxDepth rule calls). Only
	// here: the iterative VM, which other tests run on the corpus, has a higher default.
	cases = append(cases, gentest.Case{Name: "nesting limit", Src: `rule main = n
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	// A direct leaf still counts as a rule call, including when a generic
	// recursive rule invokes it or a rule-table entry reaches its body.
	cases = append(cases, gentest.Case{Name: "direct leaf nesting limit", Src: `rule main = n
rule n = "(" n ")" / leaf
rule leaf = "x"`, Inputs: []string{strings.Repeat("(", DefaultMaxDepth-3) + "x" + strings.Repeat(")", DefaultMaxDepth-3),
		strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2)}})
	// Chains of prefix and right-associative operators count against the limit too (a OperatorsRule
	// expression parses them by recursion).
	cases = append(cases, gentest.Case{Name: "operatorsRule nesting limit", Src: `rule main = x
rule x = #operators {
    operand "x"
    level { infix right "^" }
    level { prefix "-" }
}`, Inputs: []string{strings.Repeat("-", 3*DefaultMaxDepth) + "x", strings.Repeat("x^", 3*DefaultMaxDepth) + "x", strings.Repeat("-", DefaultMaxDepth/4) + "x"}})
	t.Run("values", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{}) })
	t.Run("source info", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{SourceInfo: true}) })
}

// nodeViews renders a parse result through the Node API other than JSON: the fields, length and
// text of every node Walk visits, and the String of the result. nodeViewsSource is the same
// function for a generated package, whose name replaces PKG.
func nodeViews(n *value.Node) string {
	if n == nil {
		return "nil"
	}
	// scalar prints a scalar value and only the presence of any other: printing a node or a list
	// of nodes inside every visit would print its subtree again.
	scalar := func(x any) string {
		switch x.(type) {
		case nil, string, int, bool:
			return fmt.Sprint(x)
		}
		return "value"
	}
	// A node shared by several edges, such as an operand that is also a capture of its
	// operator, is printed once: walking it again on every edge can take exponential time.
	seen := map[*value.Node]bool{}
	var b strings.Builder
	err := value.Walk(n, func(v value.WalkVisit) (value.WalkAction, error) {
		fmt.Fprintf(&b, "%d %d %d %s %d ", v.Depth, v.Edge.Kind, v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex)
		if v.Node == nil {
			fmt.Fprintf(&b, "%s;", scalar(v.Value))
			return value.WalkContinue, nil
		}
		if seen[v.Node] {
			b.WriteString("seen;")
			return value.WalkSkip, nil
		}
		seen[v.Node] = true
		for name, x := range v.Node.Fields() {
			fmt.Fprintf(&b, "%s=%s=%t,", name, scalar(x), v.Node.Field(name) != nil)
		}
		if v.Node.Len() == 0 {
			// The text of a node whose operands are fields spans its subtree: print the
			// length of a long one, or a deep tree prints its input once per level.
			if s := v.Node.Text(); len(s) <= 64 {
				b.WriteString(s)
			} else {
				fmt.Fprintf(&b, "text(%d)", len(s))
			}
		}
		b.WriteString(";")
		return value.WalkContinue, nil
	})
	if err != nil {
		b.WriteString(err.Error())
	}
	return b.String() + n.String()
}

const nodeViewsSource = `func() string {
	n, _ := n.(*PKG.Node)
	if n == nil {
		return "nil"
	}
	// scalar prints a scalar value and only the presence of any other: printing a node or a list
	// of nodes inside every visit would print its subtree again.
	scalar := func(x any) string {
		switch x.(type) {
		case nil, string, int, bool:
			return fmt.Sprint(x)
		}
		return "value"
	}
	seen := map[*PKG.Node]bool{}
	var b strings.Builder
	err := PKG.Walk(n, func(v PKG.WalkVisit) (PKG.WalkAction, error) {
		fmt.Fprintf(&b, "%d %d %d %s %d ", v.Depth, v.Edge.Kind, v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex)
		if v.Node == nil {
			fmt.Fprintf(&b, "%s;", scalar(v.Value))
			return PKG.WalkContinue, nil
		}
		if seen[v.Node] {
			b.WriteString("seen;")
			return PKG.WalkSkip, nil
		}
		seen[v.Node] = true
		for name, x := range v.Node.Fields() {
			fmt.Fprintf(&b, "%s=%s=%t,", name, scalar(x), v.Node.Field(name) != nil)
		}
		if v.Node.Len() == 0 {
			// The text of a node whose operands are fields spans its subtree: print the
			// length of a long one, or a deep tree prints its input once per level.
			if s := v.Node.Text(); len(s) <= 64 {
				b.WriteString(s)
			} else {
				fmt.Fprintf(&b, "text(%d)", len(s))
			}
		}
		b.WriteString(";")
		return PKG.WalkContinue, nil
	})
	if err != nil {
		b.WriteString(err.Error())
	}
	return b.String() + n.String()
}()`

func testGeneratedParsersCorpus(t *testing.T, goBin string, cases []gentest.Case, opts gen.GenOptions) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("gentest"))
	var imports, parsers, recognizers, viewers strings.Builder
	var inputs [][]string
	var want []string
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts.Package, opts.Start, opts.Recognize = fmt.Sprintf("g%d", i), "main", true
		code, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; n, err := g%d.Parse(s, g%[3]d.WithUnit(u)); return n, err },\n", i, i, i)
		fmt.Fprintf(&recognizers, "\tfunc(s string, b bool) error { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.Recognize(s, g%[3]d.WithUnit(u)) },\n", i, i, i)
		fmt.Fprintf(&viewers, "\tfunc(n any) string { return %s },\n", strings.ReplaceAll(nodeViewsSource, "PKG", fmt.Sprintf("g%d", i)))
		inputs = append(inputs, c.Inputs)
		// The generated code projects (internal/compile/project.go), and records source information as asked.
		prog := compileBare(t, c.Src, compile.Options{NoProjections: true, SourceInfo: opts.SourceInfo})
		for _, in := range c.Inputs {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				n, err := prog.ParseWith("main", in, ParseOptions{Unit: unit})
				out := resultJSON(n, err) + " views: " + strconv.Quote(nodeViews(n))
				_, err = prog.ParseWith("main", in, ParseOptions{Unit: unit, Recognize: true})
				want = append(want, out+" recognize: "+strconv.Quote(fmt.Sprint(err)))
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

var recognizers = []func(string, bool) error{
`+recognizers.String()+`}

var viewers = []func(any) string{
`+viewers.String()+`}

func main() {
	var inputs [][][]byte
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i, in := range inputs {
		for _, b := range in {
			s := string(b)
			for _, bytes := range []bool{false, true} {
				n, err := parsers[i](s, bytes)
				out := map[string]any{"node": n}
				if err != nil {
					out["err"] = err.Error()
				}
				b, _ := json.Marshal(out)
				fmt.Println(string(b) + " views: " + strconv.Quote(viewers[i](n)) + " recognize: " + strconv.Quote(fmt.Sprint(recognizers[i](s, bytes))))
			}
		}
	}
}
`)
	data, _ := json.Marshal(inputBytes(inputs))
	write("inputs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	k := 0
	for _, c := range cases {
		for _, in := range c.Inputs {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				if got[k] != want[k] {
					t.Errorf("%s (%v): input %q\n generated %s\n engine    %s", c.Name, unit, in, got[k], want[k])
				}
				k++
			}
		}
	}
}

func TestGenerateErrors(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateErrors)
}

func testGenerateErrors(t *testing.T) {
	g, _ := syntax.Parse("@start\nrule main = \"a\" b\nrule b = \"b\"")
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "nope"}); err == nil || err.Error() != "rule nope is not defined" {
		t.Errorf("got %v", err)
	}
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "b"}); err == nil || err.Error() != "rule b is not a start rule; parsing can begin only at a rule marked @start" {
		t.Errorf("not a start rule: got %v", err)
	}
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main"}); err == nil {
		t.Error("expected an error for a missing package name")
	}
	for _, pkg := range []string{"foo-bar", "type", "1p", "_"} {
		if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main"}); err == nil || err.Error() != fmt.Sprintf("invalid package name %q", pkg) {
			t.Errorf("package %q: got %v", pkg, err)
		}
	}
	bad, _ := parseTest(`rule main = x`)
	if _, err := gentest.GenerateCode(t, bad, gen.GenOptions{Package: "p", Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}

// mutants returns n random edits of each input (a character inserted, deleted or replaced, or two
// characters swapped, up to three times), with characters from the inputs, reproducibly for a seed.
func mutants(inputs []string, n int, seed uint64) []string {
	rng := rand.New(rand.NewPCG(seed, 1))
	var pool []rune
	for _, in := range inputs {
		pool = append(pool, []rune(in)...)
	}
	if len(pool) == 0 {
		return nil
	}
	var out []string
	for _, in := range inputs {
		for range n {
			rs := []rune(in)
			for range 1 + rng.IntN(3) {
				i := rng.IntN(len(rs) + 1)
				switch c := pool[rng.IntN(len(pool))]; rng.IntN(4) {
				case 0:
					rs = append(rs[:i], append([]rune{c}, rs[i:]...)...)
				case 1:
					if i < len(rs) {
						rs = append(rs[:i], rs[i+1:]...)
					}
				case 2:
					if i < len(rs) {
						rs[i] = c
					}
				default:
					if i+1 < len(rs) {
						rs[i], rs[i+1] = rs[i+1], rs[i]
					}
				}
			}
			out = append(out, string(rs))
		}
	}
	return out
}

// TestGeneratedTypes checks ParseAST of parsers generated with Types: it compiles for every
// grammar of the corpus and returns the errors of Parse, and it converts the trees of
// typedGrammar into the expected values.
func TestGeneratedTypes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypes)
}

func testGeneratedTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	testGeneratedTypesCorpus(t, goBin, generatedTypesCases(t), gen.GenOptions{ConvertTypes: true})
}

// generatedTypesCases returns the grammars of TestGeneratedTypes: the corpus, a nesting limit and
// the typed grammar, with mutated inputs for the grammars written for direct rules.
func generatedTypesCases(t *testing.T) []gentest.Case {
	// Exactly at and just beyond the nesting limit, through direct rules (as in
	// TestGeneratedParsersMatchEngine).
	cases := append(gentest.GenCorpus(t), gentest.Case{Name: "nesting limit", Src: `type R = { T node[string] }
rule main R = t:n #atomic => R{T: $t}
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	cases = append(cases, typedLeaderValueCases...)
	cases = append(cases, gentest.Case{Name: "typed", Src: gentest.TypedGrammar, Inputs: []string{"f(1,x)!?;[3];zz", "f(1,(;g();", "f(a);"}})
	// The grammars written for direct rules also get mutated inputs, which reach other partial
	// matches and failures than the written ones.
	for i := range cases {
		if strings.Contains(cases[i].Name, "typed/direct_") {
			cases[i].Inputs = append(cases[i].Inputs, mutants(cases[i].Inputs, 8, uint64(i))...)
		}
	}
	return cases
}

func testGeneratedTypesCorpus(t *testing.T, goBin string, cases []gentest.Case, reference gen.GenOptions, requiredTyped ...int) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("gentest"))
	var imports, parsers, converters strings.Builder
	var inputs [][]string
	var wantErr []string
	// wantValue holds the canonical form of the engine's value of each input, and accepted tells
	// which inputs the engine accepts; the canonical typed values of accepted inputs must equal it.
	var wantValue []any
	var accepted []bool
	typedRuntime := 0
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code := gentest.GeneratePackage(t, dir, fmt.Sprintf("g%d", i), g, gen.GenOptions{Start: "main", Types: true, SourceInfo: true})
		if strings.Contains(code, "TParse(trules") {
			typedRuntime++
		}
		if c.Name == "OperatorsRule prefix-part cuts" && !strings.Contains(code, "TParse(trules") {
			t.Error("OperatorsRule prefix-part cuts: typed runtime is not exercised")
		}
		// The same through the reference route: Node conversion for the full
		// corpus, or a disabled optimization for focused differential tests.
		refOpts := reference
		refOpts.Start, refOpts.Types = "main", true
		refOpts.SourceInfo = true
		gentest.GeneratePackage(t, dir, fmt.Sprintf("c%d", i), g, refOpts)
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n\tc%d \"gentest/c%d\"\n", i, i, i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.ParseAST(s, g%[3]d.WithUnit(u)) },\n", i, i, i)
		fmt.Fprintf(&converters, "\tfunc(s string, b bool) (any, error) { u := c%d.CodePoints; if b { u = c%d.Bytes }; return c%d.ParseAST(s, c%[3]d.WithUnit(u)) },\n", i, i, i)
		inputs = append(inputs, c.Inputs)
		prog := mustCompile(t, c.Src)
		for _, in := range c.Inputs {
			n, err := prog.Parse("main", in)
			wantErr = append(wantErr, fmt.Sprint(err))
			accepted = append(accepted, err == nil)
			var val any
			if err == nil {
				b, merr := json.Marshal(n)
				if merr != nil {
					t.Fatalf("%s: input %q: %v", c.Name, in, merr)
				}
				var engineJSON any
				if merr := json.Unmarshal(b, &engineJSON); merr != nil {
					t.Fatalf("%s: input %q: %v", c.Name, in, merr)
				}
				if val, merr = canonicalEngine(engineJSON); merr != nil {
					t.Fatalf("%s: input %q: engine value: %v", c.Name, in, merr)
				}
			}
			wantValue = append(wantValue, val)
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

var converters = []func(string, bool) (any, error){
`+converters.String()+`}

// typeName is the dynamic type of v without package qualifiers, also inside type arguments.
func typeName(v any) string {
	var out []byte
	word := 0 // where the current qualified name starts in out
	for _, c := range []byte(fmt.Sprintf("%T", v)) {
		switch {
		case c == '.':
			out = out[:word] // drop the package qualifier
		case c == '_' || c == '/' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z':
			out = append(out, c)
		default:
			out = append(out, c)
			word = len(out)
		}
	}
	return string(out)
}

`+canonicalValueSource+`
func main() {
	var inputs [][][]byte
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i, in := range inputs {
		for _, b := range in {
			s := string(b)
			for _, bytes := range []bool{false, true} {
				v, err := parsers[i](s, bytes)
				cv, cerr := converters[i](s, bytes)
				typed, _ := json.Marshal(map[string]any{"value": v, "err": fmt.Sprint(err)})
				conv, _ := json.Marshal(map[string]any{"value": cv, "err": fmt.Sprint(cerr)})
				if string(typed) != string(conv) || typeName(v) != typeName(cv) {
					fmt.Fprintf(os.Stderr, "MISMATCH %d %q (bytes: %v)\n typed %s %s\n conv  %s %s\n", i, s, bytes, typeName(v), typed, typeName(cv), conv)
				}
				if !bytes {
					b, _ := json.Marshal(map[string]any{"value": v, "canonical": canonicalValue(reflect.ValueOf(v)), "type": fmt.Sprintf("%T", v), "err": fmt.Sprint(err)})
					fmt.Println(string(b))
				}
			}
		}
	}
	// Parsers of the typed runtime are pooled: parsing concurrently gives the same results.
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, in := range inputs {
				for _, b := range in {
					s := string(b)
					v, err := parsers[i](s, false)
					cv, cerr := converters[i](s, false)
					a, _ := json.Marshal(map[string]any{"value": v, "err": fmt.Sprint(err)})
					b, _ := json.Marshal(map[string]any{"value": cv, "err": fmt.Sprint(cerr)})
					if string(a) != string(b) {
						fmt.Fprintf(os.Stderr, "CONCURRENT MISMATCH %d %q\n", i, s)
					}
				}
			}
		}()
	}
	wg.Wait()
}
`)
	data, _ := json.Marshal(inputBytes(inputs))
	write("inputs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("the typed runtime differs from the reference route:\n%s", stderr.String())
	}
	minimum := 5
	if len(requiredTyped) != 0 {
		minimum = requiredTyped[0]
	}
	if typedRuntime < minimum {
		t.Errorf("only %d of %d grammars use the typed runtime", typedRuntime, len(cases))
	}
	t.Logf("%d of %d grammars use the typed runtime", typedRuntime, len(cases))
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(wantErr) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(wantErr), out)
	}
	var got []struct {
		Value json.RawMessage
		Type  string
		Err   string
	}
	for _, l := range lines {
		var r struct {
			Value json.RawMessage
			Type  string
			Err   string
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		got = append(got, r)
	}
	canonical := make([]json.RawMessage, len(lines))
	for i, l := range lines {
		var r struct{ Canonical json.RawMessage }
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		canonical[i] = r.Canonical
	}
	k := 0
	for _, c := range cases {
		for _, in := range c.Inputs {
			if got[k].Err != wantErr[k] {
				t.Errorf("%s: input %q: error %s, want %s", c.Name, in, got[k].Err, wantErr[k])
			}
			if accepted[k] {
				if err := canonicalTypedMatches(canonical[k], wantValue[k]); err != nil {
					t.Errorf("%s: input %q: typed value %s: %v", c.Name, in, got[k].Value, err)
				}
			}
			if want, ok := aliasTypedGoldens[c.Name]; ok && in == "éé" && string(got[k].Value) != want {
				t.Errorf("%s: typed alias value is %s, want %s", c.Name, got[k].Value, want)
			}
			k++
		}
	}
	if cases[len(cases)-1].Name != "typed" {
		return
	}
	typed := got[len(got)-3:]
	for i, want := range []string{
		`{"Value":[` +
			`{"Start":0,"End":9,"Fn":{"Start":0,"End":1,"Text":"f"},"Args":[{"Start":2,"End":3,"Text":"1"},{"Start":4,"End":5,"Text":"x"}],"Span_":{"Start":6,"End":7,"Value":"!"},"N":2,"Opt":{"Start":7,"End":8,"Value":"?"},"Rows":[[]],"Ok":true},` +
			`{"Start":9,"End":13,"Value":[{"Start":9,"End":10,"Value":"["},{"Start":10,"End":11,"Text":"3"},{"Start":11,"End":12,"Value":"]"},{"Start":12,"End":13,"Value":";"}]},` +
			`{"Start":13,"End":15,"Text":"zz"}],"Type":"[]types.Node","Err":"<nil>"}`,
		`{"Value":[` +
			`{"Start":0,"End":1,"Text":"f"},` +
			`{"Start":1,"End":6,"Text":"(1,(;","Message":"1:2: syntax error: expected \"[\", [0-9], [a-z]"},` +
			`{"Start":6,"End":7,"Text":"g"},` +
			`{"Start":7,"End":10,"Text":"();","Message":"1:8: syntax error: expected \"[\", [0-9], [a-z]"}],"Type":"[]types.Node","Err":"1:2: syntax error: expected \"[\", [0-9], [a-z]\n1:8: syntax error: expected \"[\", [0-9], [a-z]"}`,
		`{"Value":[{"Start":0,"End":5,"Fn":{"Start":0,"End":1,"Text":"f"},"Args":[{"Start":2,"End":3,"Text":"a"}],"Span_":{"Start":4,"End":4,"Value":""},"N":1,"Opt":null,"Rows":[[]],"Ok":true}],"Type":"[]types.Node","Err":"<nil>"}`,
	} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.Encode(typed[i])
		want = strings.ReplaceAll(want, "g37", fmt.Sprintf("g%d", len(cases)-1))
		if got := strings.TrimSuffix(b.String(), "\n"); got != want {
			t.Errorf("typed %q:\n got %s\nwant %s", cases[len(cases)-1].Inputs[i], got, want)
		}
	}
}

// inputBytes returns inputs as byte slices, which JSON carries exactly (as base64); JSON
// strings would replace invalid UTF-8 with U+FFFD.
func inputBytes(inputs [][]string) [][][]byte {
	out := make([][][]byte, len(inputs))
	for i, in := range inputs {
		for _, s := range in {
			out[i] = append(out[i], []byte(s))
		}
	}
	return out
}
