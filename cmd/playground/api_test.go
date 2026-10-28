package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func call[T any](t *testing.T, method string, req any) T {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	resp := handle(method, b)
	if err := json.Unmarshal(resp, &v); err != nil {
		t.Fatalf("%s: %v: %s", method, err, resp)
	}
	return v
}

const pairs = `type Pair = { Key node[string]; Value node[string] }

@start(default)
rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
@start
rule key = [a-z]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
`

func TestCompileInfo(t *testing.T) {
	c := call[compileResult](t, "compile", grammarRequest{Grammar: pairs})
	if !c.OK || len(c.Diagnostics) != 0 {
		t.Fatalf("compile failed: %+v", c)
	}
	var names []string
	for _, r := range c.Rules {
		names = append(names, r.Name)
	}
	if got := strings.Join(names, " "); got != "main pair key value ws" {
		t.Errorf("rules = %s", got)
	}
	if c.Rules[1].Type != "Pair" || c.Rules[1].Line != 5 || c.Rules[1].Col != 1 {
		t.Errorf("pair = %+v", c.Rules[1])
	}
	// Only the rules marked @start can begin a parse.
	for i, want := range []bool{true, false, true, false, false} {
		if c.Rules[i].Start != want {
			t.Errorf("rule %s: start = %t, want %t", c.Rules[i].Name, c.Rules[i].Start, want)
		}
	}
	if len(c.Types) != 1 || c.Types[0].Kind != "struct" || c.Types[0].Spec != "{ Key node[string]; Value node[string] }" {
		t.Errorf("types = %+v", c.Types)
	}
	if c.Start != "main" {
		t.Errorf("start = %q", c.Start)
	}
}

func TestCompileDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		src        string
		line, col  int
		msgContain string
	}{
		{"rule main = (", 1, 14, "expected an expression"},
		{"@start\nrule main = other", 2, 13, "undefined rule other"},
		{"rule main = \"a\"", 1, 1, "the grammar has no start rule"},
		{"", 0, 0, "the grammar has no start rule"},
	} {
		c := call[compileResult](t, "compile", grammarRequest{Grammar: tc.src})
		if c.OK || len(c.Diagnostics) == 0 {
			t.Errorf("%q: no diagnostics: %+v", tc.src, c)
			continue
		}
		d := c.Diagnostics[0]
		if d.Line != tc.line || d.Col != tc.col || !strings.Contains(d.Message, tc.msgContain) {
			t.Errorf("%q: diagnostic = %+v", tc.src, d)
		}
	}
}

// TestParseNamedNodeTypes checks that the S-expression names the type of a node of a named node
// type, as pego parse -f sexpr does, and that a plain node[string] stays bare.
func TestParseNamedNodeTypes(t *testing.T) {
	const named = `type Ident = node[string]
type Out = { Id Ident; Plain node[string] }
@start
rule main Out = id:ident "," plain:digits $$ => Out{Id: $id, Plain: $plain}
rule ident Ident = [a-z]+ #atomic
rule digits = [0-9]+ #atomic
`
	r := call[parseResult](t, "parse", parseRequest{Grammar: named, Input: "ab,12"})
	if want := `Out{Id: Ident("ab"), Plain: "12"}`; !r.Matched || r.SExpr != want {
		t.Errorf("sexpr = %s, matched %t", r.SExpr, r.Matched)
	}
}

func TestParse(t *testing.T) {
	r := call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3"})
	if !r.Matched || len(r.Errors) != 0 || r.Error != "" {
		t.Fatalf("parse failed: %+v", r)
	}
	if want := `[Pair{Key: "abc", Value: "12"} Pair{Key: "d", Value: "3"}]`; r.SExpr != want {
		t.Errorf("sexpr = %s", r.SExpr)
	}
	if !strings.HasPrefix(r.JSON, "{\n  \"start\": 0,\n  \"end\": 11,\n  \"value\": [") || r.Nodes != 7 {
		t.Errorf("json = %s, nodes = %d", r.JSON, r.Nodes)
	}

	// Source information adds the rules and capture names, as pego parse -source-info does.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3", SourceInfo: true})
	if want := `[Pair{Key: "abc"@key, Value: "12"@value} Pair{Key: "d"@key, Value: "3"@value}]`; r.SExpr != want {
		t.Errorf("source info sexpr = %s", r.SExpr)
	}
	if !strings.Contains(r.JSON, `"rule": "key"`) {
		t.Errorf("source info json = %s", r.JSON)
	}

	// Default values drop the actions, so the tree is the default node tree.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3", DefaultValue: true})
	if !r.Matched || r.Error != "" || r.SExpr == "" || strings.Contains(r.SExpr, "Pair{") {
		t.Errorf("default value: %+v", r)
	}
	again := call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3", DefaultValue: true, SourceInfo: true})
	if again.SExpr == "" || strings.Contains(again.SExpr, "Pair{") || again.SExpr == r.SExpr {
		t.Errorf("default value with source info: %s", again.SExpr)
	}

	// A syntax error.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=x"})
	if r.Matched || len(r.Errors) != 1 || r.JSON != "" {
		t.Fatalf("expected one error: %+v", r)
	}
	if e := r.Errors[0]; e.Pos != 4 || e.Line != 1 || e.Col != 5 || e.Message != "syntax error: expected [0-9]" {
		t.Errorf("error = %+v", e)
	}

	// Another start rule, recognition, byte positions and a backend.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "あ", Start: "key", Recognize: true})
	if r.Start != "key" || r.Matched || len(r.Errors) != 1 {
		t.Errorf("recognize key: %+v", r)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc", Start: "key", Recognize: true, Backend: "bytecode-iterative"})
	if !r.Matched || r.JSON != "" || len(r.Errors) != 0 {
		t.Errorf("recognize ok: %+v", r)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "x=1;\nyy=", Unit: "bytes", Backend: "bytecode"})
	if len(r.Errors) != 1 || r.Errors[0].Pos != 8 || r.Errors[0].Line != 2 || r.Errors[0].Col != 4 {
		t.Errorf("bytes: %+v", r)
	}
	// An unknown start rule is an error, not a silent fallback to the default.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "a=1", Start: "nope"})
	if r.Start != "" || r.Matched || r.Error != "rule nope is not defined" || !r.Compile.OK {
		t.Errorf("unknown start: %+v", r)
	}
	g := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Start: "nope"})
	if g.Code != "" || len(g.Diagnostics) != 1 || g.Diagnostics[0].Message != "rule nope is not defined" {
		t.Errorf("generate with an unknown start: %+v", g)
	}
	// A rule that is not marked @start cannot begin a parse.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "a=1", Start: "pair"})
	if r.Start != "" || r.Matched || r.Error != "rule pair is not a start rule; parsing can begin only at a rule marked @start" || !r.Compile.OK {
		t.Errorf("not a start rule: %+v", r)
	}
	g = call[generateResult](t, "generate", generateRequest{Grammar: pairs, Start: "pair"})
	if g.Code != "" || len(g.Diagnostics) != 1 || g.Diagnostics[0].Message != "rule pair is not a start rule; parsing can begin only at a rule marked @start" {
		t.Errorf("generate with a rule that is not a start rule: %+v", g)
	}
	// An empty start rule selects the rule marked @start(default), wherever it is.
	r = call[parseResult](t, "parse", parseRequest{Grammar: "@start\nrule a = \"x\"\n@start(default)\nrule b = \"y\"", Input: "y"})
	if r.Start != "b" || !r.Matched {
		t.Errorf("default start: %+v", r)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: "@start\nrule a = \"x\"\nrule b = \"y\"", Input: "x"})
	if r.Start != "a" || !r.Matched {
		t.Errorf("only start: %+v", r)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "a=1", Backend: "nope"})
	if !strings.Contains(r.Error, "unknown backend") {
		t.Errorf("bad backend: %+v", r)
	}
}

func TestParseRecovered(t *testing.T) {
	src := `@start
rule main = stmt* $$
rule stmt = s:([a-z]+ ";") #recover(skip=[^;]+ ";") => $s
`
	r := call[parseResult](t, "parse", parseRequest{Grammar: src, Input: "ab;1;cd;"})
	if !r.Matched || !r.Recovered || len(r.Errors) != 1 || r.SExpr == "" {
		t.Fatalf("recovered: %+v", r)
	}
	if r.Errors[0].Pos != 3 {
		t.Errorf("error = %+v", r.Errors[0])
	}
}

func TestFormatAndGenerate(t *testing.T) {
	f := call[formatResult](t, "format", grammarRequest{Grammar: "rule main = \"a\" // c\n"})
	if f.Formatted != "rule main = \"a\" // c\n" {
		t.Errorf("formatted = %q", f.Formatted)
	}
	f = call[formatResult](t, "format", grammarRequest{Grammar: "def ="})
	if len(f.Diagnostics) == 0 || f.Formatted != "" {
		t.Errorf("format error: %+v", f)
	}
	g := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Package: "pairs", Types: true})
	if !strings.Contains(g.Code, "package pairs") || !strings.Contains(g.Code, "func ParseAST(") {
		t.Errorf("generated code lacks package or ParseAST: %.200s %+v", g.Code, g.Diagnostics)
	}
	// Each file is also given on its own, to be saved: the displayed text joins them under banners.
	if len(g.Files) != 3 || g.Files[0].Path != "parser.go" || g.Files[1].Path != "api.go" || g.Files[2].Path != "types/types.go" {
		t.Fatalf("generated files = %+v", g.Files)
	}
	for _, f := range g.Files {
		pkg := "package pairs\n"
		if f.Path == "types/types.go" {
			pkg = "package types\n"
		}
		if !strings.HasPrefix(f.Content, "// Code generated by pego.") || !strings.Contains(f.Content, pkg) || strings.Contains(f.Content, "// =====") {
			t.Errorf("%s is not a source file of the package: %.200s", f.Path, f.Content)
		}
	}
	ts := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Language: "typescript"})
	if len(ts.Files) != 2 || ts.Files[0].Path != "runtime.ts" || ts.Files[1].Path != "parser.ts" {
		t.Fatalf("TypeScript files = %+v", ts.Files)
	}
	for _, f := range ts.Files {
		if !strings.HasPrefix(f.Content, "// Code generated by pego.") || strings.Contains(f.Content, "// =====") || !strings.Contains(ts.Code, "// ===== "+f.Path+" =====") {
			t.Errorf("%s is not a source file of the parser: %.200s", f.Path, f.Content)
		}
	}
	if i, j := strings.Index(ts.Code, "// ===== parser.ts ====="), strings.Index(ts.Code, "// ===== runtime.ts ====="); i < 0 || j < i {
		t.Errorf("the combined view must show parser.ts before runtime.ts (%d, %d)", i, j)
	}
	typed := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Language: "typescript", Types: true})
	if len(typed.Files) != 3 || typed.Files[2].Path != "types.ts" {
		t.Errorf("typed TypeScript files = %+v", typed.Files)
	}
}

func TestBadRequests(t *testing.T) {
	for _, tc := range []struct{ method, req string }{
		{"nope", "{}"},
		{"parse", "{"},
	} {
		var v map[string]any
		if err := json.Unmarshal(handle(tc.method, []byte(tc.req)), &v); err != nil || v["error"] == nil {
			t.Errorf("%s %s: %v %v", tc.method, tc.req, v, err)
		}
	}
	v := call[versionResult](t, "version", nil)
	if !strings.HasPrefix(v.Go, "go") {
		t.Errorf("version = %+v", v)
	}
}

func TestDepthLimits(t *testing.T) {
	defer func(d, td int) { maxDepth, maxTreeDepth = d, td }(maxDepth, maxTreeDepth)
	maxDepth, maxTreeDepth = 50, 30
	src := `@start
rule main = value $$
rule value = list / "x"
rule list = "[" value* "]"
`
	nested := func(n int) string { return strings.Repeat("[", n) + "x" + strings.Repeat("]", n) }

	// Rule calls nest too deeply: an engine error, not a crash.
	for _, backend := range []string{"", "closure", "bytecode"} {
		r := call[parseResult](t, "parse", parseRequest{Grammar: src, Input: nested(40), Backend: backend})
		if r.Matched || r.Error != "nesting too deep: more than 50 rule calls" {
			t.Errorf("backend %q: %+v", backend, r)
		}
	}
	// The iterative backend is not limited, but the tree is too deep to show.
	r := call[parseResult](t, "parse", parseRequest{Grammar: src, Input: nested(40), Backend: "bytecode-iterative"})
	if !r.Matched || r.JSON != "" || r.SExpr != "" || r.Depth != 82 || r.Nodes != 162 ||
		r.Error != "the tree is nested 82 levels deep, more than the 30 levels the playground can show" {
		t.Errorf("iterative: %+v", r)
	}
	// Within the limits, and with an explicit limit.
	r = call[parseResult](t, "parse", parseRequest{Grammar: src, Input: nested(10)})
	if !r.Matched || r.Error != "" || r.Depth != 22 || r.Nodes != 42 || r.JSON == "" {
		t.Errorf("within the limits: %+v", r)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: src, Input: nested(10), MaxDepth: 5})
	if r.Matched || !strings.Contains(r.Error, "more than 5 rule calls") {
		t.Errorf("explicit limit: %+v", r)
	}
}

func TestDeepTreeNatively(t *testing.T) {
	// encoding/json refuses trees nested more than 10,000 levels; the limit reports them first.
	src := `@start
rule main = value $$
rule value = list / "x"
rule list = "[" value* "]"
`
	n := 3000
	r := call[parseResult](t, "parse", parseRequest{Grammar: src, Input: strings.Repeat("[", n) + "x" + strings.Repeat("]", n), Backend: "bytecode-iterative"})
	if !r.Matched || r.Depth != 2*n+2 || !strings.HasPrefix(r.Error, "the tree is nested 6002 levels deep") {
		t.Errorf("matched %v, depth %d, error %q", r.Matched, r.Depth, r.Error)
	}
}

func TestParseWithoutValue(t *testing.T) {
	// A start rule whose elements are all dropped matches with the node of the empty tuple, and
	// recognition matches without a tree.
	for _, recognize := range []bool{false, true} {
		r := call[parseResult](t, "parse", parseRequest{Grammar: "@start\nrule main = -\"a\"", Input: "a", Recognize: recognize})
		want := "()"
		if recognize {
			want = ""
		}
		if !r.Matched || len(r.Errors) != 0 || r.Error != "" || r.SExpr != want || (r.JSON == "") != recognize {
			t.Errorf("recognize=%v: %+v", recognize, r)
		}
	}
	// So does a recognition that recovered from errors, which reports them.
	src := `@start
rule main = stmt* $$
rule stmt = s:([a-z]+ ";") #recover(skip=[^;]+ ";") => $s
`
	r := call[parseResult](t, "parse", parseRequest{Grammar: src, Input: "ab;1;", Recognize: true})
	if !r.Matched || !r.Recovered || len(r.Errors) != 1 || r.JSON != "" {
		t.Errorf("recognize with recovery: %+v", r)
	}
	// A runtime error is not a match.
	r = call[parseResult](t, "parse", parseRequest{Grammar: "type T = { D int }\n@start\nrule main T = \"a\" => T{D: 1 / 0}", Input: "a"})
	if r.Matched || r.Error == "" {
		t.Errorf("runtime error: %+v", r)
	}
}

func TestParseRuns(t *testing.T) {
	// A request times one parse by default. Runs times that many parses, and the result comes from
	// the first of them, so the runs do not change the tree.
	r := call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3"})
	if r.Runs != 1 || r.Micros < 0 {
		t.Errorf("default runs = %d, micros = %d", r.Runs, r.Micros)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3", Runs: 3})
	if !r.Matched || r.Runs != 3 || r.Micros < 0 || r.SExpr != `[Pair{Key: "abc", Value: "12"} Pair{Key: "d", Value: "3"}]` {
		t.Errorf("runs = %+v", r)
	}
	// Runs beyond the limit are capped, and the count reports what was timed.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3", Runs: maxParseRuns + 1})
	if r.Runs != maxParseRuns {
		t.Errorf("capped runs = %d, want %d", r.Runs, maxParseRuns)
	}
	// A failed match is timed too.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=x", Runs: 2})
	if r.Matched || r.Runs != 2 || len(r.Errors) == 0 {
		t.Errorf("failed runs = %+v", r)
	}
	// The median is the middle value of the sorted durations, and the lower one for an even count.
	for _, tc := range []struct {
		times []int64
		want  int64
	}{
		{[]int64{7}, 7},
		{[]int64{5, 1, 3}, 3},
		{[]int64{9, 2, 4, 1}, 2},
	} {
		if got := medianMicros(tc.times); got != tc.want {
			t.Errorf("medianMicros(%v) = %d, want %d", tc.times, got, tc.want)
		}
	}
}
