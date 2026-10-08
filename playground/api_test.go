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

const pairs = `type Pair struct { Key Match, Value Match }

def main = ws ps:pair+ $$ -> $ps
def pair: Pair = k:key "=" v:value ";"? ws -> new Pair{Key: $k, Value: $v}
def key = @(?a-z)+
def value = @(?0-9)+
def ws = (? \t\n)*
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
	if c.Rules[1].Type != "Pair" || c.Rules[1].Line != 4 || c.Rules[1].Col != 1 {
		t.Errorf("pair = %+v", c.Rules[1])
	}
	if len(c.Types) != 1 || c.Types[0].Kind != "struct" || c.Types[0].Spec != "struct { Key Match, Value Match }" {
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
		{"def main = (", 1, 13, "expected an expression"},
		{"def main = other", 1, 12, "undefined rule other"},
		{"", 0, 0, "no rules"},
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

func TestParse(t *testing.T) {
	r := call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=12; d=3"})
	if !r.Matched || len(r.Errors) != 0 || r.Error != "" {
		t.Fatalf("parse failed: %+v", r)
	}
	if want := `[(Pair Key="abc"@key Value="12"@value) (Pair Key="d"@key Value="3"@value)]`; r.SExpr != want {
		t.Errorf("sexpr = %s", r.SExpr)
	}
	if !strings.HasPrefix(r.JSON, "{\n  \"type\": \"List\",") || r.Nodes != 7 {
		t.Errorf("json = %s, nodes = %d", r.JSON, r.Nodes)
	}

	// A syntax error.
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "abc=x"})
	if r.Matched || len(r.Errors) != 1 || r.JSON != "" {
		t.Fatalf("expected one error: %+v", r)
	}
	if e := r.Errors[0]; e.Pos != 4 || e.Line != 1 || e.Col != 5 || e.Message != "syntax error: expected (?0-9)" {
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
	if r.Start != "" || r.Matched || r.Error != "start rule nope is not defined" || !r.Compile.OK {
		t.Errorf("unknown start: %+v", r)
	}
	g := call[generateResult](t, "generate", generateRequest{Grammar: pairs, Start: "nope"})
	if g.Code != "" || len(g.Diagnostics) != 1 || g.Diagnostics[0].Message != "start rule nope is not defined" {
		t.Errorf("generate with an unknown start: %+v", g)
	}
	// An empty start rule selects the default: main, or else the first rule.
	r = call[parseResult](t, "parse", parseRequest{Grammar: "def a = \"x\"\ndef b = \"y\"", Input: "x"})
	if r.Start != "a" || !r.Matched {
		t.Errorf("default start: %+v", r)
	}
	r = call[parseResult](t, "parse", parseRequest{Grammar: pairs, Input: "a=1", Backend: "nope"})
	if !strings.Contains(r.Error, "unknown backend") {
		t.Errorf("bad backend: %+v", r)
	}
}

func TestParseRecovered(t *testing.T) {
	src := `def main = stmt* $$
def stmt = s:((?a-z)+ ";") #recover(skip=(?^;)+ ";") -> $s
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
	f := call[formatResult](t, "format", grammarRequest{Grammar: "def main   =  \"a\"   // c\n"})
	if f.Formatted != "def main = \"a\" // c\n" {
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

func TestParseWithoutValue(t *testing.T) {
	// A start rule that matches without producing a value still matches.
	for _, recognize := range []bool{false, true} {
		r := call[parseResult](t, "parse", parseRequest{Grammar: `def main = -"a"`, Input: "a", Recognize: recognize})
		if !r.Matched || len(r.Errors) != 0 || r.Error != "" || r.JSON != "" {
			t.Errorf("recognize=%v: %+v", recognize, r)
		}
	}
	// So does a recognition that recovered from errors, which reports them.
	src := `def main = stmt* $$
def stmt = s:((?a-z)+ ";") #recover(skip=(?^;)+ ";") -> $s
`
	r := call[parseResult](t, "parse", parseRequest{Grammar: src, Input: "ab;1;", Recognize: true})
	if !r.Matched || !r.Recovered || len(r.Errors) != 1 || r.JSON != "" {
		t.Errorf("recognize with recovery: %+v", r)
	}
	// A runtime error is not a match.
	r = call[parseResult](t, "parse", parseRequest{Grammar: "type T struct { D int }\ndef main: T = \"a\" -> new T{D: depth}", Input: "a"})
	if r.Matched || r.Error == "" {
		t.Errorf("runtime error: %+v", r)
	}
}
