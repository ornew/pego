package lsp

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func TestDiagnostics(t *testing.T) {
	c := newInitialized(t)
	for _, tc := range []struct {
		name, text string
		want       []Diagnostic
	}{
		{"valid", "def main = \"a\"", nil},
		{
			// The ")" follows an astral character, which takes two UTF-16 code units.
			"syntax error", "def a = \"😀\" )\n",
			[]Diagnostic{{Range: rng(0, 13, 0, 14), Message: "expected 'def' or 'type', found ')'"}},
		},
		{
			"several syntax errors", "def a = (\ndef b = \"名前\" ]\ndef c = \"ok\"",
			[]Diagnostic{
				{Range: rng(1, 0, 1, 3), Message: "expected an expression, found 'def'"},
				{Range: rng(1, 13, 1, 14), Message: "expected 'def' or 'type', found ']'"},
			},
		},
		{
			"unterminated string", "def a = \"😀x\ndef b = \"y\"",
			[]Diagnostic{{Range: rng(0, 8, 0, 12), Message: "unterminated string"}},
		},
		{
			"undefined rule", "def main = \"😀\" 名前 x",
			[]Diagnostic{{Range: rng(0, 16, 0, 18), Message: "undefined rule 名前"}, {Range: rng(0, 19, 0, 20), Message: "undefined rule x"}},
		},
		{
			// The error of a definition is reported at its name.
			"duplicate rule", "def a = \"b\"\n\n  def   a = \"c\"\ndef main = a",
			[]Diagnostic{{Range: rng(2, 8, 2, 9), Message: "rule a is already defined"}},
		},
		{
			"type error", "type P struct { X Match }\ndef main = \"a\" -> new P{X: 1}",
			[]Diagnostic{{Range: rng(1, 24, 1, 25), Message: "cannot use int as Match in field X of P"}},
		},
		{
			"undefined type", "def main: Nope = \"a\"",
			[]Diagnostic{{Range: rng(0, 10, 0, 14), Message: "undefined type Nope"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := "file:///" + strings.ReplaceAll(tc.name, " ", "_") + ".pego"
			got := c.open(uri, tc.text)
			for i := range tc.want {
				tc.want[i].Severity, tc.want[i].Source = severityError, "pego"
			}
			if len(got) != len(tc.want) || len(got) > 0 && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
	c.exit()
}

func change(c *testClient, uri string, version int, changes ...map[string]any) []Diagnostic {
	c.t.Helper()
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": version},
		"contentChanges": changes,
	})
	p := c.diagnostics(uri)
	if p.Version == nil || *p.Version != version {
		c.t.Errorf("diagnostics for version %v, want %d", p.Version, version)
	}
	return p.Diagnostics
}

func edit(l1, c1, l2, c2 int, text string) map[string]any {
	return map[string]any{"range": rng(l1, c1, l2, c2), "text": text}
}

func messages(diags []Diagnostic) string {
	var ms []string
	for _, d := range diags {
		ms = append(ms, d.Message)
	}
	return strings.Join(ms, "; ")
}

func TestIncrementalChanges(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///inc.pego"
	if d := c.open(uri, "def main = \"😀\" x"); messages(d) != "undefined rule x" || d[0].Range != rng(0, 16, 0, 17) {
		t.Fatalf("diagnostics %+v", d)
	}
	// Replace x after the astral character.
	if d := change(c, uri, 2, edit(0, 16, 0, 17, `"y"`)); len(d) != 0 {
		t.Errorf("after the fix: %+v", d)
	}
	// Two changes in one notification: the second applies to the result of the first.
	d := change(c, uri, 3,
		edit(0, 14, 0, 14, "😀"),             // "😀😀"
		edit(0, 21, 0, 21, "\r\ndef y = z"), // at the end, after "y"
	)
	if messages(d) != "undefined rule z" || d[0].Range != rng(1, 8, 1, 9) {
		t.Errorf("diagnostics %+v", d)
	}
	// Delete across the line break, and replace the whole document.
	if d := change(c, uri, 4, edit(0, 21, 1, 9, "")); len(d) != 0 {
		t.Errorf("after deleting the line: %+v", d)
	}
	var syms []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams(uri), &syms)
	if len(syms) != 1 || syms[0].Name != "main" || syms[0].Range != rng(0, 0, 0, 21) {
		t.Errorf("symbols %+v", syms)
	}
	if d := change(c, uri, 5, map[string]any{"text": "def"}); messages(d) != "expected identifier, found end of file" {
		t.Errorf("after replacing the document: %+v", d)
	}
	// Saving and closing.
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}, "text": "def main = \"a\""})
	if d := c.diagnostics(uri); len(d.Diagnostics) != 0 {
		t.Errorf("after save: %+v", d)
	}
	c.notify("textDocument/didClose", docParams(uri))
	if d := c.diagnostics(uri); len(d.Diagnostics) != 0 || d.Version != nil {
		t.Errorf("after close: %+v", d)
	}
	if e := c.requestError("textDocument/documentSymbol", docParams(uri)); e.Code != codeRequestFailed {
		t.Errorf("closed document: %+v", e)
	}
	c.exit()
}

func formatDoc(c *testClient, uri, text string) string {
	c.t.Helper()
	var edits []TextEdit
	c.requestInto("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}, &edits)
	return applyEdits(text, edits)
}

func TestFormatting(t *testing.T) {
	c := newInitialized(t)
	src := "// The 😀 rule.\ndef  main =   \"😀\"   x // trailing\n\n\n\ndef x=\"名\" #error(message=\"e\")\n"
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := grammar.Format(g)
	c.open("file:///f.pego", src)
	if got := formatDoc(c, "file:///f.pego", src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Formatted source needs no edits.
	c.open("file:///formatted.pego", want)
	if r := c.request("textDocument/formatting", docParams("file:///formatted.pego")); string(r) != "[]" {
		t.Errorf("edits for formatted source: %s", r)
	}
	// CRLF line terminators are kept.
	crlf := strings.ReplaceAll(src, "\n", "\r\n")
	c.open("file:///crlf.pego", crlf)
	if got := formatDoc(c, "file:///crlf.pego", crlf); got != strings.ReplaceAll(want, "\n", "\r\n") {
		t.Errorf("CRLF: got %q", got)
	}
	// Source with syntax errors is not formatted: the definitions with errors would be lost.
	bad := "def  a = \"x\"\ndef b = (\n"
	c.open("file:///bad.pego", bad)
	if r := c.request("textDocument/formatting", docParams("file:///bad.pego")); string(r) != "[]" {
		t.Errorf("edits for source with errors: %s", r)
	}
	c.exit()
}

// navSrc is the grammar of the navigation tests. Positions in the tests are in UTF-16 code units;
// "😀" takes two.
const navSrc = `// A number.
type Num terminal
type Pair struct {
    Left  Num // the left
    Right *Num
}
type Any = Pair | Num // anything

def num: Num = @(?0-9)+

// Two numbers
// with an emoji.
def pair = l:num "😀" r:num? -> new Pair{Left: $l, Right: $r}
def main = (pair / num) $$`

func TestDefinitionAndReferences(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///nav.pego"
	if d := c.open(uri, navSrc); len(d) != 0 {
		t.Fatalf("diagnostics %+v", d)
	}
	numDef := Location{URI: uri, Range: rng(8, 4, 8, 7)}
	pairType := Location{URI: uri, Range: rng(2, 5, 2, 9)}
	for _, tc := range []struct {
		line, char int
		want       []Location
	}{
		{12, 13, []Location{numDef}},                            // l:num
		{12, 16, []Location{numDef}},                            // just after l:num
		{12, 24, []Location{numDef}},                            // r:num after the emoji
		{13, 19, []Location{numDef}},                            // (pair / num)
		{8, 5, []Location{numDef}},                              // the definition itself
		{12, 39, []Location{pairType}},                          // new Pair
		{6, 11, []Location{pairType}},                           // type Any = Pair
		{3, 10, []Location{{URI: uri, Range: rng(1, 5, 1, 8)}}}, // Left Num
		{12, 9, nil}, // the capture label l
		{0, 3, nil},  // a comment
	} {
		var got []Location
		c.requestInto("textDocument/definition", at(uri, tc.line, tc.char), &got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("definition at %d:%d: got %+v, want %+v", tc.line, tc.char, got, tc.want)
		}
	}

	refs := func(line, char int, decl bool) []Range {
		var locs []Location
		p := at(uri, line, char)
		p["context"] = map[string]any{"includeDeclaration": decl}
		c.requestInto("textDocument/references", p, &locs)
		var rs []Range
		for _, l := range locs {
			rs = append(rs, l.Range)
		}
		return rs
	}
	if got, want := refs(8, 5, true), []Range{rng(8, 4, 8, 7), rng(12, 13, 12, 16), rng(12, 24, 12, 27), rng(13, 19, 13, 22)}; !reflect.DeepEqual(got, want) {
		t.Errorf("references of num: %v", got)
	}
	if got, want := refs(12, 25, false), []Range{rng(12, 13, 12, 16), rng(12, 24, 12, 27), rng(13, 19, 13, 22)}; !reflect.DeepEqual(got, want) {
		t.Errorf("references of num without the declaration: %v", got)
	}
	// The type Num and the rule num are different names.
	if got, want := refs(1, 6, true), []Range{rng(1, 5, 1, 8), rng(3, 10, 3, 13), rng(4, 11, 4, 14), rng(6, 18, 6, 21), rng(8, 9, 8, 12)}; !reflect.DeepEqual(got, want) {
		t.Errorf("references of Num: %v", got)
	}

	var hs []documentHighlight
	c.requestInto("textDocument/documentHighlight", at(uri, 13, 12), &hs)
	if len(hs) != 2 || hs[0].Range != rng(12, 4, 12, 8) || hs[0].Kind != highlightWrite || hs[1].Kind != highlightRead {
		t.Errorf("highlights %+v", hs)
	}
	c.exit()
}

func TestDocumentSymbols(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///sym.pego"
	c.open(uri, navSrc)
	var syms []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams(uri), &syms)
	type sym struct {
		Name, Detail string
		Kind         int
		Range, Sel   Range
		Children     int
	}
	var got []sym
	for _, s := range syms {
		got = append(got, sym{s.Name, s.Detail, s.Kind, s.Range, s.SelectionRange, len(s.Children)})
		for _, f := range s.Children {
			got = append(got, sym{"  " + f.Name, f.Detail, f.Kind, f.Range, f.SelectionRange, 0})
		}
	}
	want := []sym{
		{"Num", "terminal", symbolKindString, rng(1, 0, 1, 17), rng(1, 5, 1, 8), 0},
		{"Pair", "struct", symbolKindStruct, rng(2, 0, 5, 1), rng(2, 5, 2, 9), 2},
		{"  Left", "Num", symbolKindField, rng(3, 4, 3, 13), rng(3, 4, 3, 8), 0},
		{"  Right", "*Num", symbolKindField, rng(4, 4, 4, 14), rng(4, 4, 4, 9), 0},
		{"Any", "= Pair | Num", symbolKindInterface, rng(6, 0, 6, 21), rng(6, 5, 6, 8), 0},
		{"num", "Num", symbolKindFunction, rng(8, 0, 8, 23), rng(8, 4, 8, 7), 0},
		{"pair", "Pair", symbolKindFunction, rng(12, 0, 12, 61), rng(12, 4, 12, 8), 0},
		{"main", "Seq", symbolKindFunction, rng(13, 0, 13, 26), rng(13, 4, 13, 8), 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
	// With syntax errors, the definitions that parsed are still listed.
	syms = nil
	c.open("file:///broken.pego", "def a = (\ndef b = \"x\"")
	c.requestInto("textDocument/documentSymbol", docParams("file:///broken.pego"), &syms)
	if len(syms) != 1 || syms[0].Name != "b" || syms[0].Detail != "" {
		t.Errorf("symbols with errors: %+v", syms)
	}
	c.exit()
}

func hoverText(c *testClient, uri string, line, char int) (string, *Range) {
	c.t.Helper()
	var h *hover
	c.requestInto("textDocument/hover", at(uri, line, char), &h)
	if h == nil {
		return "", nil
	}
	if h.Contents.Kind != "markdown" {
		c.t.Errorf("hover kind %s", h.Contents.Kind)
	}
	return h.Contents.Value, h.Range
}

func TestHover(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///hover.pego"
	src := navSrc + "\ndef lst = xs:num* [len($xs) > 0] #error(message=\"e\") -> $xs\ndef s = x:num [$x.startPos > 0]"
	if d := c.open(uri, src); len(d) != 0 {
		t.Fatalf("diagnostics %+v", d)
	}
	for _, tc := range []struct {
		line, char int
		want       string
		r          Range
	}{
		{12, 6, "```pego\ndef pair: Pair\n```\n\n*Inferred type.*\n\nTwo numbers\nwith an emoji.", rng(12, 4, 12, 8)},
		{13, 13, "```pego\ndef pair: Pair\n```\n\n*Inferred type.*\n\nTwo numbers\nwith an emoji.", rng(13, 12, 13, 16)},
		{12, 25, "```pego\ndef num: Num\n```", rng(12, 24, 12, 27)},
		{13, 5, "```pego\ndef main: Seq\n```\n\n*Inferred type.*", rng(13, 4, 13, 8)},
		{14, 5, "```pego\ndef lst: []Num\n```\n\n*Inferred type.*", rng(14, 4, 14, 7)},
		{1, 6, "```pego\ntype Num terminal\n```\n\nA number.", rng(1, 5, 1, 8)},
		{12, 39, "```pego\ntype Pair struct {\n    Left  Num // the left\n    Right *Num\n}\n```", rng(12, 36, 12, 40)},
		{6, 6, "```pego\ntype Any = Pair | Num\n```\n\nanything", rng(6, 5, 6, 8)},
		{14, 20, "```pego\nlen(x) int\n```\n" + findBuiltin(builtinFuncs, "len").doc, rng(14, 19, 14, 22)},
		{14, 37, "```pego\n#error(message=\"...\")\n```\n" + findBuiltin(attributes, "error").doc, rng(14, 34, 14, 39)},
		{15, 20, "```pego\nstartPos int\n```\n" + findBuiltin(builtinMembers, "startPos").doc, rng(15, 18, 15, 26)},
	} {
		got, r := hoverText(c, uri, tc.line, tc.char)
		if got != tc.want || r == nil || *r != tc.r {
			t.Errorf("hover at %d:%d: got %q %v, want %q %v", tc.line, tc.char, got, r, tc.want, tc.r)
		}
	}
	if got, _ := hoverText(c, uri, 12, 9); got != "" {
		t.Errorf("hover on a capture label: %q", got)
	}

	// Built-in and undefined names; a grammar with errors has no inferred types.
	c.open("file:///hover2.pego", "type T struct { M Match }\ndef a = x\ndef b: T = \"b\" -> new T{M: $1}")
	for _, tc := range []struct {
		line, char int
		want       string
	}{
		{0, 19, "```pego\ntype Match\n```\n" + findBuiltin(builtinTypes, "Match").doc},
		{1, 8, "```pego\ndef x\n```\nUndefined rule."},
		{1, 4, "```pego\ndef a\n```\n\n*The type is inferred when the grammar has no errors.*"},
		{2, 4, "```pego\ndef b: T\n```"},
	} {
		if got, _ := hoverText(c, "file:///hover2.pego", tc.line, tc.char); got != tc.want {
			t.Errorf("hover at %d:%d: got %q, want %q", tc.line, tc.char, got, tc.want)
		}
	}
	c.exit()
}

func TestRename(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///rename.pego"
	c.open(uri, navSrc)
	var prep *prepareRenameResult
	c.requestInto("textDocument/prepareRename", at(uri, 12, 25), &prep)
	if prep == nil || prep.Range != rng(12, 24, 12, 27) || prep.Placeholder != "num" {
		t.Errorf("prepareRename %+v", prep)
	}
	if r := c.request("textDocument/prepareRename", at(uri, 12, 9)); string(r) != "null" {
		t.Errorf("prepareRename on a capture label: %s", r)
	}
	rename := func(line, char int, name string) map[string]any {
		p := at(uri, line, char)
		p["newName"] = name
		return p
	}
	var we workspaceEdit
	c.requestInto("textDocument/rename", rename(12, 25, "数字"), &we)
	got := applyEdits(navSrc, we.Changes[uri])
	want := strings.NewReplacer("def num:", "def 数字:", "l:num", "l:数字", "r:num", "r:数字", "/ num)", "/ 数字)").Replace(navSrc)
	if got != want {
		t.Errorf("rename num: got\n%s", got)
	}
	we = workspaceEdit{}
	c.requestInto("textDocument/rename", rename(6, 12, "Couple"), &we)
	if got := applyEdits(navSrc, we.Changes[uri]); got != strings.ReplaceAll(navSrc, "Pair", "Couple") {
		t.Errorf("rename Pair: got\n%s", got)
	}
	for _, tc := range []struct {
		line, char int
		name, want string
	}{
		{12, 25, "pair", "rule pair is already defined"},
		{12, 25, "def", "def is a keyword"},
		{12, 25, "a-b", `"a-b" is not a valid rule name`},
		{12, 25, "_", `"_" is not a valid rule name`},
		{2, 6, "Num", "type Num is already defined"},
		{2, 6, "pair", "type pair must start with an uppercase letter"},
		{2, 6, "Match", "type Match is reserved"},
	} {
		if e := c.requestError("textDocument/rename", rename(tc.line, tc.char, tc.name)); e.Message != tc.want {
			t.Errorf("rename to %s: %q, want %q", tc.name, e.Message, tc.want)
		}
	}
	// Built-in and undefined names cannot be renamed, nor anything while there are syntax errors.
	c.open("file:///r2.pego", "type T struct { M Match }\ndef a = x")
	if e := c.requestError("textDocument/prepareRename", at("file:///r2.pego", 0, 19)); e.Message != "cannot rename the built-in type Match" {
		t.Errorf("built-in type: %+v", e)
	}
	if e := c.requestError("textDocument/prepareRename", at("file:///r2.pego", 1, 8)); e.Message != "rule x is not defined" {
		t.Errorf("undefined rule: %+v", e)
	}
	c.open("file:///r3.pego", "def a = b\ndef b = (")
	if e := c.requestError("textDocument/prepareRename", at("file:///r3.pego", 0, 8)); !strings.Contains(e.Message, "syntax errors") {
		t.Errorf("syntax errors: %+v", e)
	}
	c.exit()
}

func completionLabels(c *testClient, uri string, line, char int) []string {
	c.t.Helper()
	var list completionList
	c.requestInto("textDocument/completion", at(uri, line, char), &list)
	var labels []string
	for _, it := range list.Items {
		labels = append(labels, it.Label)
	}
	sort.Strings(labels)
	return labels
}

func TestCompletion(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///comp.pego"
	// Each line is a context; the cursor is at the end of the line unless noted.
	lines := []string{
		"type Num terminal",              // 0
		"type P struct { A Num, B Num }", // 1
		"type Q struct { A Num, ",        // 2: a field name
		"type R struct { A Nu",           // 3: a field type
		"def num: N",                     // 4: the type of a rule
		"def a = n",                      // 5: a rule name
		"def b = x:num #",                // 6: an attribute
		"def c = x:num y:num -> $",       // 7: a capture after $
		"def d = x:num -> foldl($x, $x, (acc, e) => new ", // 8: a struct type after new
		"def e = x:num -> $x.",                            // 9: a member
		"def f = x:num [",                                 // 10: in a predicate
		"def g = pratt { operand num level { infix ",      // 11: an associativity
		"def h = \"// not a comment",                      // 12: in a string
		"// def i = ",                                     // 13: in a comment
		"def j = x:num -> $x",                             // 14: typing a capture name
		"type ",                                           // 15: the name of a new type
		"type S ",                                         // 16: after a type name
		"def k = pratt { operand ",                        // 17: an operand of a pratt expression
		"def l = num -> foldl($1, $1, (acc, e) => $",      // 18: a lambda parameter
	}
	src := strings.Join(lines, "\n")
	c.open(uri, src)
	end := func(line int) int { return len([]rune(lines[line])) }
	rules := []string{"a", "b", "c", "d", "e", "f", "g", "h", "j", "k", "l", "num"}
	builtinTypeNames := []string{"Error", "List", "Match", "Operator", "Seq", "bool", "int", "node", "string", "terminal"}
	sorted := func(xs ...[]string) []string {
		var out []string
		for _, x := range xs {
			out = append(out, x...)
		}
		sort.Strings(out)
		return out
	}
	userTypes := []string{"Num", "P", "Q", "R", "S"}
	for _, tc := range []struct {
		line int
		char int // -1: the end of the line
		want []string
	}{
		{2, -1, nil},
		{3, -1, sorted(userTypes, builtinTypeNames, statementKeywords)},
		{4, -1, sorted(userTypes, builtinTypeNames)},
		{5, -1, sorted(rules, statementKeywords, bodyKeywords)},
		{5, 8, sorted(rules, statementKeywords, bodyKeywords)},
		{6, -1, []string{"error", "recover", "stream"}},
		{7, -1, []string{"x", "y"}},
		{8, -1, []string{"P", "Q", "R"}},
		{9, -1, sorted([]string{"A", "B", "children", "endPos", "startPos"})},
		{10, -1, sorted([]string{"concat", "foldl", "foldr", "len", "list", "map", "text"}, termKeywords)},
		{11, -1, []string{"left", "none", "right"}},
		{12, -1, nil},
		{13, -1, nil},
		{14, -1, []string{"x"}},
		{15, -1, nil},
		{16, -1, sorted(typeKeywords)},
		{17, -1, sorted(rules, statementKeywords, prattKeywords)},
		{18, -1, []string{"acc", "e"}},
	} {
		char := tc.char
		if char < 0 {
			char = end(tc.line)
		}
		if got := completionLabels(c, uri, tc.line, char); !reflect.DeepEqual(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
			t.Errorf("line %d (%q) at %d:\ngot  %q\nwant %q", tc.line, lines[tc.line], char, got, tc.want)
		}
	}
	// At the start of an empty file.
	c.open("file:///empty.pego", "")
	if got := completionLabels(c, "file:///empty.pego", 0, 0); !reflect.DeepEqual(got, []string{"def", "package", "type"}) {
		t.Errorf("empty file: %q", got)
	}
	// Items carry the rule's type and documentation, and snippets for functions.
	c.open("file:///items.pego", "// The digits.\ndef num = (?0-9)+\ndef main = num")
	var list completionList
	c.requestInto("textDocument/completion", at("file:///items.pego", 2, 14), &list)
	var num CompletionItem
	for _, it := range list.Items {
		if it.Label == "num" {
			num = it
		}
	}
	if num.Kind != completionKindFunction || num.Detail != "[]Match" || num.Documentation == nil || num.Documentation.Value != "The digits." {
		t.Errorf("item %+v", num)
	}
	c.exit()
}

func TestSemanticTokens(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///sem.pego"
	src := "type N terminal // c\ndef 名前 = x:n \"s😀\" (?a) #stream -> new P{F: len($x).startPos}\ndef n: N = a [v = 1] infix left"
	c.open(uri, src)
	var res struct {
		Data []int `json:"data"`
	}
	c.requestInto("textDocument/semanticTokens/full", docParams(uri), &res)
	if len(res.Data)%5 != 0 {
		t.Fatalf("data %v", res.Data)
	}
	idx := newTextIndex(src)
	var got []string
	line, char := 0, 0
	for i := 0; i < len(res.Data); i += 5 {
		if res.Data[i] > 0 {
			char = 0
		}
		line += res.Data[i]
		char += res.Data[i+1]
		start := idx.offset(Position{line, char})
		end := idx.offset(Position{line, char + res.Data[i+2]})
		var mods []string
		for b, m := range semanticTokenModifiers {
			if res.Data[i+4]&(1<<b) != 0 {
				mods = append(mods, m)
			}
		}
		got = append(got, src[start:end]+" "+strings.Join(append([]string{semanticTokenTypes[res.Data[i+3]]}, mods...), "."))
	}
	want := []string{
		"type keyword", "N type.declaration", "terminal keyword", "// c comment",
		"def keyword", "名前 function.declaration", "x variable.declaration", "n function", `"s😀" string`, "(?a) regexp",
		"stream decorator", "new keyword", "P type", "F property", "len function.defaultLibrary", "$x parameter",
		"startPos property.defaultLibrary",
		"def keyword", "n function.declaration", "N type", "a function", "v variable.declaration", "1 number",
		"infix keyword", "left keyword",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	c.exit()
}

// TestExampleGrammars checks that the example grammars have no diagnostics, that every name
// resolves to a definition, and that the formatting edits give what pego fmt prints.
func TestExampleGrammars(t *testing.T) {
	paths, err := filepath.Glob("../../examples/*/*.pego")
	more, err2 := filepath.Glob("../../parsers/*/*.pego")
	paths = append(paths, more...)
	if err != nil || err2 != nil || len(paths) == 0 {
		t.Fatalf("no example grammars: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		a := analyze(src)
		if len(a.diags) != 0 {
			t.Errorf("%s: diagnostics %+v", path, a.diags)
		}
		for _, o := range a.occs {
			if a.lookup(o.kind, o.name) == nil && !(o.kind == kindType && findBuiltin(builtinTypes, o.name) != nil) {
				t.Errorf("%s: %s %s does not resolve", path, o.kind, o.name)
			}
			if a.text[o.start:o.end] != o.name {
				t.Errorf("%s: occurrence %q at %d is %q", path, o.name, o.start, a.text[o.start:o.end])
			}
		}
		if a.prog == nil {
			t.Errorf("%s: not compiled", path)
		}
		if got := applyEdits(src, formatEdits(a)); got != grammar.Format(a.g) {
			t.Errorf("%s: formatting edits give\n%s", path, got)
		}
	}
}

// BenchmarkAnalyze measures the analysis that runs after every change, on the largest example
// grammars.
func BenchmarkAnalyze(b *testing.B) {
	for _, name := range []string{"golang/golang.pego", "python/python.pego"} {
		data, err := os.ReadFile("../../parsers/" + name)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(filepath.Dir(name), func(b *testing.B) {
			for b.Loop() {
				analyze(string(data))
			}
		})
	}
}
