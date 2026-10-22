package lsp

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// hiddenSrc has hidden rules (names that begin with "_") that are called, one with an action,
// and an unused one, plus an inline action.
const hiddenSrc = `rule _sp = " "*
@start
rule main = _sp word _sp (n:_num => $n) _tail
rule word = a:[a-z]+ #atomic => $a
rule _tail = "!"?
rule _num = d:[0-9]+ #atomic => $d
rule _dead = "z"
rule other = word
`

func hiddenAt(line int, name string, nth int) (int, int) {
	lines := strings.Split(hiddenSrc, "\n")
	col := -1
	for range nth {
		col = strings.Index(lines[line][col+1:], name) + col + 1
	}
	return line, col + 1
}

func TestHiddenRuleNavigation(t *testing.T) {
	c := newServer(t)
	capabilities := map[string]any{"experimental": map[string]any{"pego": map[string]any{"lintDiagnostics": true}}}
	for k, v := range fullCapabilities {
		capabilities[k] = v
	}
	c.request("initialize", map[string]any{"capabilities": capabilities})
	uri := "file:///hidden.pego"
	var unreachable []string
	for _, d := range c.open(uri, hiddenSrc) {
		if d.Code != "unreachable-rule" {
			t.Fatalf("unexpected diagnostic %+v", d)
		}
		unreachable = append(unreachable, d.Message[:strings.Index(d.Message, " is never used")])
	}
	// A hidden rule is a rule: _dead and other are unused, while called hidden rules, one called
	// only inside an inline action, and the rules their inline actions call are not.
	if want := []string{"rule _dead", "rule other"}; !reflect.DeepEqual(unreachable, want) {
		t.Errorf("unreachable rules = %q, want %q", unreachable, want)
	}

	spDecl := Location{URI: uri, Range: rng(0, 5, 0, 8)}
	for _, tc := range []struct {
		name         string
		line         int
		nth          int
		wantDef      Location
		wantRefCount int
	}{
		{"_sp", 2, 1, spDecl, 3},
		{"_sp", 2, 2, spDecl, 3},
		{"_sp", 0, 1, spDecl, 3},
		{"_tail", 2, 1, Location{URI: uri, Range: rng(4, 5, 4, 10)}, 2},
		// Called inside the inline action's parentheses.
		{"_num", 2, 1, Location{URI: uri, Range: rng(5, 5, 5, 9)}, 2},
	} {
		line, char := hiddenAt(tc.line, tc.name, tc.nth)
		var def []Location
		c.requestInto("textDocument/definition", at(uri, line, char), &def)
		if !reflect.DeepEqual(def, []Location{tc.wantDef}) {
			t.Errorf("definition of %s at %d:%d = %+v, want %+v", tc.name, line, char, def, tc.wantDef)
		}
		params := at(uri, line, char)
		params["context"] = map[string]any{"includeDeclaration": true}
		var refs []Location
		c.requestInto("textDocument/references", params, &refs)
		if len(refs) != tc.wantRefCount || !slices.Contains(refs, tc.wantDef) {
			t.Errorf("references of %s = %+v, want %d including the declaration", tc.name, refs, tc.wantRefCount)
		}
		params["context"] = map[string]any{"includeDeclaration": false}
		c.requestInto("textDocument/references", params, &refs)
		if len(refs) != tc.wantRefCount-1 || slices.Contains(refs, tc.wantDef) {
			t.Errorf("references of %s without the declaration = %+v", tc.name, refs)
		}
	}

	// A hidden rule without an action has no type of its own; one with an action has the type of
	// the action, as any rule does.
	for _, tc := range []struct {
		name       string
		line, nth  int
		want       string
		wantNoType bool
	}{
		{"_sp", 2, 1, "```pego\nrule _sp\n```\n\n*Hidden rule: it makes no node of its own.*", true},
		{"_tail", 2, 1, "```pego\nrule _tail\n```\n\n*Hidden rule: it makes no node of its own.*", true},
		{"_num", 5, 1, "", false},
	} {
		line, char := hiddenAt(tc.line, tc.name, tc.nth)
		got, r := hoverText(c, uri, line, char)
		if r == nil {
			t.Errorf("no hover on %s", tc.name)
			continue
		}
		if tc.wantNoType && got != tc.want {
			t.Errorf("hover on %s = %q, want %q", tc.name, got, tc.want)
		}
		if !tc.wantNoType && (strings.Contains(got, "Hidden rule") || !strings.HasPrefix(got, "```pego\nrule "+tc.name+" node[")) {
			t.Errorf("hover on %s = %q, want the type of its action", tc.name, got)
		}
	}

	// Hidden rules are listed with the others.
	var symbols []symbolInformation
	c.requestInto("textDocument/documentSymbol", docParams(uri), &symbols)
	var names []string
	for _, s := range symbols {
		names = append(names, s.Name)
	}
	if want := []string{"_sp", "main", "word", "_tail", "_num", "_dead", "other"}; !reflect.DeepEqual(names, want) {
		t.Errorf("document symbols = %q, want %q", names, want)
	}

	// A highlight marks the declaration as a write and each call as a read.
	line, char := hiddenAt(2, "_sp", 2)
	var highlights []documentHighlight
	c.requestInto("textDocument/documentHighlight", at(uri, line, char), &highlights)
	if len(highlights) != 3 || highlights[0].Kind != highlightWrite || highlights[1].Kind != highlightRead {
		t.Errorf("highlights = %+v", highlights)
	}
	c.exit()
}

func TestHiddenRuleCompletionAndRename(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///hidden-rename.pego"
	c.open(uri, hiddenSrc+"rule use = main")
	labels := completionLabels(c, uri, 8, len("rule use = "))
	for _, name := range []string{"_sp", "_tail", "_num", "_dead", "main"} {
		if !contains(labels, name) {
			t.Errorf("completion lacks %s: %q", name, labels)
		}
	}
	// "_" alone is no rule name.
	if contains(labels, "_") {
		t.Errorf("completion offers _: %q", labels)
	}

	rename := func(line int, name string, nth int, to string) (string, *rpcError) {
		l, ch := hiddenAt(line, name, nth)
		p := at(uri, l, ch)
		p["newName"] = to
		reply := c.call("textDocument/rename", p)
		if reply["error"] != nil {
			var e rpcError
			if err := json.Unmarshal(reply["error"], &e); err != nil {
				t.Fatal(err)
			}
			return "", &e
		}
		var edit workspaceEdit
		if err := json.Unmarshal(reply["result"], &edit); err != nil {
			t.Fatal(err)
		}
		return applyEdits(hiddenSrc+"rule use = main", edit.Changes[uri]), nil
	}
	// A hidden rule keeps its leading underscore, so that it remains hidden, and a visible rule
	// cannot gain one: hiding changes the nodes a parse makes and removes the rule as an entry.
	got, err := rename(2, "_sp", 1, "_ws")
	if err != nil {
		t.Fatalf("rename _sp to _ws: %+v", err)
	}
	if want := strings.ReplaceAll(hiddenSrc+"rule use = main", "_sp", "_ws"); got != want {
		t.Errorf("rename _sp to _ws:\n%s\nwant\n%s", got, want)
	}
	for _, tc := range []struct {
		line       int
		name, to   string
		wantSubstr string
	}{
		{2, "_sp", "sp", "would make it a visible rule"},
		{2, "word", "_word", "would hide it"},
		{2, "_sp", "_", "not a valid rule name"},
		{2, "_sp", "_tail", "already defined"},
		{2, "_sp", "_a-b", "not a valid rule name"},
	} {
		if _, err := rename(tc.line, tc.name, 1, tc.to); err == nil || !strings.Contains(err.Message, tc.wantSubstr) {
			t.Errorf("rename %s to %s: %+v, want an error containing %q", tc.name, tc.to, err, tc.wantSubstr)
		}
	}
	c.exit()
}

// TestInlineActionCaptureScopes checks that completion after "$" offers only the captures that the
// action can read: an inline action reads the captures made inside its parentheses and the
// rule's action those made outside every inline action.
func TestInlineActionCaptureScopes(t *testing.T) {
	c := newInitialized(t)
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"inline action", "rule num = [0-9]+\nrule q = a:num (b:num => $", []string{"b"}},
		{"inline action before the cursor's group", "rule num = [0-9]+\nrule q = a:num (c:num => $c) (b:num => $", []string{"b"}},
		{"rule action", "rule num = [0-9]+\nrule q = a:num (b:num => $b) => $", []string{"a"}},
		{"rule action, capture after the group", "rule num = [0-9]+\nrule q = a:num (b:num => $b) c:num => $", []string{"a", "c"}},
		{"nested inline action", "rule num = [0-9]+\nrule q = (a:num (b:num => $b) => $", []string{"a"}},
		{"plain group", "rule num = [0-9]+\nrule q = a:num (b:num) => $", []string{"a", "b"}},
		{"lambda in the rule action", "rule num = [0-9]+\nrule q = a:num (b:num => $b) xs:num* => map($xs, (e) => $", []string{"a", "xs"}},
		{"lambda in an inline action", "rule num = [0-9]+\nrule q = a:num (xs:num* => map($xs, (e) => $", []string{"xs"}},
		{"inline action around a closed one", "rule num = [0-9]+\nrule q = a:num ((b:num => $b) c:num => $", []string{"c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := "file:///scopes-" + strings.ReplaceAll(tc.name, " ", "-") + ".pego"
			c.open(uri, tc.src)
			lines := strings.Split(tc.src, "\n")
			last := len(lines) - 1
			if got := completionLabels(c, uri, last, len([]rune(lines[last]))); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("captures = %q, want %q", got, tc.want)
			}
		})
	}
	c.exit()
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// TestInlineActionDiagnostics checks that lint findings inside an inline action reach the editor at
// the position in the source, and name no rule that the compiler generates for the inline action.
func TestInlineActionDiagnostics(t *testing.T) {
	c := newServer(t)
	capabilities := map[string]any{"experimental": map[string]any{"pego": map[string]any{"lintDiagnostics": true}}}
	for k, v := range fullCapabilities {
		capabilities[k] = v
	}
	c.request("initialize", map[string]any{"capabilities": capabilities})
	src := "@start\nrule main = (\"a\" / \"ab\" => \"x\") (a:\"c\" \"d\" => \"y\")"
	got := map[string]Range{}
	for _, d := range c.open("file:///inline-lint.pego", src) {
		if strings.Contains(d.Message, "_main_") {
			t.Errorf("a generated name in %+v", d)
		}
		got[d.Code] = d.Range
	}
	want := map[string]Range{
		"shadowed-alternative": rng(1, 19, 1, 23),
		"unused-capture":       rng(1, 33, 1, 34),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diagnostics = %+v, want %+v", got, want)
	}
	c.exit()
}
