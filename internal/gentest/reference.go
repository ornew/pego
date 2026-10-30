package gentest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/compiletest"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/value"
)

// ReferenceParse parses input with a program compiled for Reference: with positions in bytes if
// bytes is set, otherwise in code points, and as a recognition (no tree) if recognize is set. It
// returns the tree and the error.
type ReferenceParse func(input string, bytes, recognize bool) (*value.Node, error)

// Reference compiles src with opts for the comparisons of generated parsers with the engine and
// returns the parse function of its rule main. The test package sets it, as it sets Compile: this
// package cannot import the engine. It fails the test if src does not compile.
var Reference func(t *testing.T, src string, opts compile.Options) ReferenceParse

func refProgram(t *testing.T, src string, opts compile.Options) ReferenceParse {
	t.Helper()
	if Reference == nil {
		t.Fatal("gentest.Reference is not set: the test package must set it to compile with the engine")
	}
	return Reference(t, src, opts)
}
func ResultJSON(n *value.Node, err error) string {
	out := map[string]any{"node": n}
	if err != nil {
		out["err"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// NodeViews renders a parse result through the Node API other than JSON: the fields, length and
// text of every node Walk visits, and the String of the result. NodeViewsSource is the same
// function for a generated package, whose name replaces PKG.
func NodeViews(n *value.Node) string {
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

const NodeViewsSource = `func() string {
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

func GeneratedParsersCorpus(t *testing.T, goBin string, cases []Case, opts gen.GenOptions) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", GoMod("gentest"))
	var imports, parsers, recognizers, viewers strings.Builder
	var inputs [][]string
	var want []string
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts.Package, opts.Start, opts.Recognize = fmt.Sprintf("g%d", i), "main", true
		code, err := GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; n, err := g%d.Parse(s, g%[3]d.WithUnit(u)); return n, err },\n", i, i, i)
		fmt.Fprintf(&recognizers, "\tfunc(s string, b bool) error { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.Recognize(s, g%[3]d.WithUnit(u)) },\n", i, i, i)
		fmt.Fprintf(&viewers, "\tfunc(n any) string { return %s },\n", strings.ReplaceAll(NodeViewsSource, "PKG", fmt.Sprintf("g%d", i)))
		inputs = append(inputs, c.Inputs)
		// The generated code projects (internal/compile/project.go), and records source information as asked.
		prog := refProgram(t, c.Src, compile.Options{NoProjections: true, SourceInfo: opts.SourceInfo})
		for _, in := range c.Inputs {
			for _, bytes := range []bool{false, true} {
				n, err := prog(in, bytes, false)
				out := ResultJSON(n, err) + " views: " + strconv.Quote(NodeViews(n))
				_, err = prog(in, bytes, true)
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
	cmd.Env = append(os.Environ(), "GOFLAGS="+GoFlags(t, "-mod=mod -trimpath"), "GOTOOLCHAIN=local")
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
			for _, unit := range []string{"codepoints", "bytes"} {
				if got[k] != want[k] {
					t.Errorf("%s (%v): input %q\n generated %s\n engine    %s", c.Name, unit, in, got[k], want[k])
				}
				k++
			}
		}
	}
}

func GeneratedTypesCorpus(t *testing.T, goBin string, cases []Case, reference gen.GenOptions, requiredTyped ...int) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", GoMod("gentest"))
	var imports, parsers, converters strings.Builder
	var inputs [][]string
	var wantErr []string
	// wantValue holds the canonical form of the engine's value of each input, and accepted tells
	// which inputs the engine accepts; the canonical typed values of accepted inputs must equal it.
	var wantValue []any
	var accepted []bool
	typedRuntime := 0
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code := GeneratePackage(t, dir, fmt.Sprintf("g%d", i), g, gen.GenOptions{Start: "main", Types: true, SourceInfo: true})
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
		GeneratePackage(t, dir, fmt.Sprintf("c%d", i), g, refOpts)
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n\tc%d \"gentest/c%d\"\n", i, i, i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.ParseAST(s, g%[3]d.WithUnit(u)) },\n", i, i, i)
		fmt.Fprintf(&converters, "\tfunc(s string, b bool) (any, error) { u := c%d.CodePoints; if b { u = c%d.Bytes }; return c%d.ParseAST(s, c%[3]d.WithUnit(u)) },\n", i, i, i)
		inputs = append(inputs, c.Inputs)
		prog := refProgram(t, c.Src, compile.Options{SourceInfo: true})
		for _, in := range c.Inputs {
			n, err := prog(in, false, false)
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
				if val, merr = CanonicalEngine(engineJSON); merr != nil {
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

`+CanonicalValueSource+`
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
	cmd.Env = append(os.Environ(), "GOFLAGS="+GoFlags(t, "-mod=mod -trimpath"), "GOTOOLCHAIN=local")
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
				if err := CanonicalTypedMatches(canonical[k], wantValue[k]); err != nil {
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
