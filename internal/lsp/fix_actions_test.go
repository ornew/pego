package lsp

import (
	"context"
	"encoding/json"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/lint"
	"strings"
	"testing"
)

// fixActions opens src with a client that takes versioned edits and returns, for each lint
// diagnostic, the edited text of each of its fix actions (other than the suppression).
//
// With resolve, the client resolves the edits of actions: the listed actions then carry no edit,
// and each edit comes from codeAction/resolve.
func fixActions(t *testing.T, uri, src string, resolve bool) map[string][]string {
	t.Helper()
	c := newServer(t)
	defer c.exit()
	capabilities := map[string]any{
		"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
		"experimental": map[string]any{"pego": map[string]any{"lintDiagnostics": true, "showExplanation": true}},
	}
	if resolve {
		capabilities["textDocument"] = map[string]any{"codeAction": map[string]any{
			"dataSupport": true, "resolveSupport": map[string]any{"properties": []string{"edit"}}}}
	}
	c.request("initialize", map[string]any{"capabilities": capabilities})
	out := map[string][]string{}
	for _, d := range c.open(uri, src) {
		if d.Source != "pego-lint" {
			continue
		}
		var actions []codeAction
		c.requestInto("textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": uri}, "range": d.Range,
			"context": map[string]any{"diagnostics": []Diagnostic{d}},
		}, &actions)
		for _, a := range actions {
			if resolve && a.Edit != nil {
				t.Fatalf("an action listed with its edit for a client that resolves: %+v", a)
			}
			if resolve && a.Data != nil {
				var resolved codeAction
				c.requestInto("codeAction/resolve", a, &resolved)
				if resolved.Title != a.Title {
					t.Fatalf("resolve changed the title: %q to %q", a.Title, resolved.Title)
				}
				a = resolved
			}
			if a.Edit == nil || strings.HasPrefix(a.Title, "Suppress ") {
				continue
			}
			if len(a.Edit.DocumentChanges) != 1 || a.Edit.DocumentChanges[0].TextDocument.Version != 1 || a.Edit.DocumentChanges[0].TextDocument.URI != uri {
				t.Fatalf("an edit that is not versioned for the document: %+v", a.Edit)
			}
			out[d.Code] = append(out[d.Code], a.Title+" => "+applyEdits(src, a.Edit.DocumentChanges[0].Edits))
		}
	}
	return out
}

func TestLintFixActions(t *testing.T) {
	for _, tc := range []struct {
		name, src, check string
		want             []string
	}{
		{"overlapping ranges", "@start\nrule main = [a-cb-z] \"x\"\n", "char-class",
			[]string{"Merge the overlapping ranges of this character class => @start\nrule main = [a-z] \"x\"\n"}},
		{"a range that spans punctuation", "@start\nrule main = [A-z]\n", "char-class", nil},
		{"a lookahead that always succeeds", "@start\nrule main = \"a\" &\"b\"* \"c\"\n", "useless-lookahead",
			[]string{"Remove the lookahead that always succeeds => @start\nrule main = \"a\" \"c\"\n"}},
		{"a lookahead with a call", "@start\nrule main = &w* \"c\"\nrule w = \"a\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard in a labelled group", "@start\nrule main = g:(\"a\" - &\"b\"* \"c\") => len($g.Value)\n", "useless-lookahead", nil},
		{"a lookahead under a discard before a literal", "@start\nrule main = \"a\" - &\"b\"* \"c\" => \"k\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard before a group", "@start\nrule main = \"a\" - &\"b\"* (\"c\" \"d\") => \"k\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard before a call", "@start\nrule main = x:(\"a\" - &\"b\"* w) => len($x.Value)\nrule w = \"c\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard in a repeated group", "@start\nrule main = xs:(\"a\" - &\"b\"* \"c\")+ => len($xs)\n", "useless-lookahead", nil},
		{"a lookahead in a sequence of two", "@start\nrule main = &\"b\"* \"c\"\n", "useless-lookahead", nil},
		{"a lookahead in a sequence of two in an inline action", "@start\nrule main = (g:(&\"b\"* \"cc\") => len($g.Value)) \"d\"\n", "useless-lookahead", nil},
		{"a lookahead in a sequence of three in an inline action", "@start\nrule main = (g:(\"a\" &\"b\"* \"cc\") => len($g.Value)) \"d\"\n", "useless-lookahead",
			[]string{"Remove the lookahead that always succeeds => @start\nrule main = (g:(\"a\" \"cc\") => len($g.Value)) \"d\"\n"}},
		{"an unused rule with an inline action", "@start\nrule main = (g:(\"a\" \"b\") => len($g.Value)) \"d\"\nrule x = (h:(\"a\" \"b\") => len($h.Value))\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = (g:(\"a\" \"b\") => len($g.Value)) \"d\"\n"}},
		{"a lookahead that another construct holds", "@start\nrule main = \"a\" (&\"b\"*) \"c\"\n", "useless-lookahead", nil},
		{"a lookahead in a group of a sequence", "@start\nrule main = (\"a\" &\"b\"* \"c\")\n", "useless-lookahead",
			[]string{"Remove the lookahead that always succeeds => @start\nrule main = (\"a\" \"c\")\n"}},
		{"a lookahead that never succeeds", "@start\nrule main = !\"a\"? \"c\"\n", "useless-lookahead", nil},
		{"an unused rule", "@start\nrule main = \"a\"\nrule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\n"}},
		{"an unused rule with its comment", "@start\nrule main = \"a\"\n// Not used.\nrule x = \"b\" // trailing\nrule y = \"c\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\nrule y = \"c\"\n", "Remove the unused rule y => @start\nrule main = \"a\"\n// Not used.\nrule x = \"b\" // trailing\n"}},
		{"an unused rule called by another unused rule", "@start\nrule main = \"a\"\nrule x = y\nrule y = \"c\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\nrule y = \"c\"\n"}},
		{"an unused rule after another on the line", "@start\nrule main = \"a\"\nrule x = \"b\"; rule y = \"c\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\nrule y = \"c\"\n", "Remove the unused rule y => @start\nrule main = \"a\"\nrule x = \"b\"\n"}},
		{"an unused rule after another on the line, spaced", "@start\nrule main = \"a\"\nrule x = \"b\" ;  rule y = \"c\" // note\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\nrule y = \"c\" // note\n", "Remove the unused rule y => @start\nrule main = \"a\"\nrule x = \"b\" // note\n"}},
		{"an unused rule with a block comment on its line", "@start\nrule main = \"a\"\n/* block */ rule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\n"}},
		{"an unused rule with a block comment above", "@start\nrule main = \"a\"\n/* block */\nrule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\n"}},
		{"an unused rule with a block comment over several lines above", "@start\nrule main = \"a\"\n/* one\n   two */\n// three\nrule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\n"}},
		{"an unused rule keeps a file-wide directive in a block comment", "@start\nrule main = \"a\"\n/* lint:file-ignore char-class */\nrule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\"\n/* lint:file-ignore char-class */\n"}},
		{"an unused rule keeps the block comment of the rule before it", "@start\nrule main = \"a\" /* about main */\nrule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => @start\nrule main = \"a\" /* about main */\n"}},
		{"a file-wide directive stays", "// lint:file-ignore char-class\n@start\nrule main = \"a\"\nrule x = \"b\"\n", "unreachable-rule",
			[]string{"Remove the unused rule x => // lint:file-ignore char-class\n@start\nrule main = \"a\"\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, resolve := range []bool{false, true} {
				got := fixActions(t, "file:///fix.pego", tc.src, resolve)[tc.check]
				if strings.Join(got, "|") != strings.Join(tc.want, "|") {
					t.Errorf("fixes (resolve %v) = %q, want %q", resolve, got, tc.want)
				}
			}
		})
	}
	// The grammar of the language server itself is not edited on a suggestion.
	for _, resolve := range []bool{false, true} {
		if got := fixActions(t, "file:///repo/pego.pego", "@start\nrule main = \"a\"\nrule x = \"b\"\n", resolve)["unreachable-rule"]; got != nil {
			t.Errorf("fixes for pego.pego (resolve %v) = %q", resolve, got)
		}
	}
}

// forgedFix runs the verification of the fix of the first finding of check in src with the
// rewrite changed by edit, as if the proof in the lint package had been wrong.
func forgedFix(t *testing.T, ctx context.Context, src, check string, edit func(*lint.Rewrite, *analysis)) bool {
	t.Helper()
	a := analyze(src)
	d := &document{uri: "file:///forged.pego", text: src, an: a, good: a, version: 1, generation: 1}
	findings, err := a.lintFindingsContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rewrites, err := lint.Rewrites(context.Background(), a.g, a.prog.DefaultStart(), findings)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range findings {
		if f.Check != check {
			continue
		}
		edit(&rewrites[i], a)
		_, ok := d.verifiedFixControlled(ctx, findings, i, rewrites[i], &renderControl{ctx: ctx})
		return ok
	}
	t.Fatalf("no %s finding with a rewrite in %q", check, src)
	return false
}

func TestLintFixVerificationRejects(t *testing.T) {
	ctx := context.Background()
	keep := func(*lint.Rewrite, *analysis) {}
	const class = "@start\nrule main = [a-cb-z] \"x\"\n"
	const lookahead = "@start\nrule main = \"a\" &\"b\"* \"c\"\n"
	const unused = "@start\nrule main = \"a\"\nrule x = \"b\"\n"
	for _, tc := range []struct {
		name, src, check string
		edit             func(*lint.Rewrite, *analysis)
		want             bool
	}{
		{"class as proven", class, "char-class", keep, true},
		{"class that is still overlapping", class, "char-class", func(r *lint.Rewrite, _ *analysis) { r.NewText = "[a-za-z]" }, false},
		{"class that is not a class", class, "char-class", func(r *lint.Rewrite, _ *analysis) { r.NewText = "[a-" }, false},
		{"lookahead as proven", lookahead, "useless-lookahead", keep, true},
		{"lookahead replaced by a new finding", lookahead, "useless-lookahead", func(r *lint.Rewrite, _ *analysis) { r.NewText = "(\"b\"*)*" }, false},
		{"lookahead under a discard in a labelled group", "@start\nrule main = g:(\"a\" - &\"b\"* \"c\") => len($g.Value)\n", "useless-lookahead", lookaheadSpanFor(`&"b"*`), false},
		{"lookahead under a discard before a literal", "@start\nrule main = \"a\" - &\"b\"* \"c\" => \"k\"\n", "useless-lookahead", lookaheadSpanFor(`&"b"*`), false},
		{"lookahead under a discard in a repeated group", "@start\nrule main = xs:(\"a\" - &\"b\"* \"c\")+ => len($xs)\n", "useless-lookahead", lookaheadSpanFor(`&"b"*`), false},
		{"lookahead in an inline action", "@start\nrule main = (g:(&\"b\"* \"cc\") => len($g.Value)) \"d\"\n", "useless-lookahead", lookaheadSpanFor(`&"b"*`), false},
		{"lookahead in an inline action of three items, as proven", "@start\nrule main = (g:(\"a\" &\"b\"* \"cc\") => len($g.Value)) \"d\"\n", "useless-lookahead", lookaheadSpanFor(`&"b"* `), true},
		{"unused rule as proven", unused, "unreachable-rule", keep, true},
		{"unused rule that is replaced by a start rule", "@start(default)\nrule main = \"a\"\nrule x = \"b\"\n", "unreachable-rule", func(r *lint.Rewrite, _ *analysis) { r.NewText = "@start\nrule x = \"b\"" }, false},
		{"class that is replaced by a capture", class, "char-class", func(r *lint.Rewrite, _ *analysis) { r.NewText = "c:[a-z] {len($c) > 0}" }, false},
		{"start rule", unused, "unreachable-rule", func(r *lint.Rewrite, a *analysis) {
			r.Start, r.End = a.g.Rules()[0].Pos, a.g.Rules()[0].End
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := forgedFix(t, ctx, tc.src, tc.check, tc.edit); got != tc.want {
				t.Errorf("verified = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if forgedFix(t, cancelled, unused, "unreachable-rule", keep) {
			t.Error("a fix was verified under a cancelled context")
		}
	})
}

// lookaheadSpanFor makes the edit replace the lookahead text, as a rewrite that the lint package
// would not propose, to show that the verification alone rejects it.
func lookaheadSpanFor(text string) func(*lint.Rewrite, *analysis) {
	return func(r *lint.Rewrite, a *analysis) {
		line := strings.Split(a.text, "\n")[1]
		col := strings.Index(line, text)
		r.Start = grammar.Pos{Line: 2, Col: col + 1}
		r.End = grammar.Pos{Line: 2, Col: col + 1 + len(text)}
		r.NewText = ""
	}
}

func resolvingClient(t *testing.T, responsive bool) (*testClient, map[string]any) {
	t.Helper()
	c := newServer(t)
	pego := map[string]any{"lintDiagnostics": true, "showExplanation": true}
	if responsive {
		pego["responsiveAnalysis"] = true
	}
	var result struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	c.requestInto("initialize", map[string]any{"capabilities": map[string]any{
		"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
		"experimental": map[string]any{"pego": pego},
		"textDocument": map[string]any{"codeAction": map[string]any{
			"dataSupport": true, "resolveSupport": map[string]any{"properties": []string{"edit"}}}},
	}}, &result)
	return c, result.Capabilities
}

func listed(t *testing.T, c *testClient, uri string, d Diagnostic) []codeAction {
	t.Helper()
	var actions []codeAction
	c.requestInto("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "range": d.Range,
		"context": map[string]any{"diagnostics": []Diagnostic{d}},
	}, &actions)
	return actions
}

func TestCodeActionResolve(t *testing.T) {
	for _, responsive := range []bool{false, true} {
		t.Run(map[bool]string{false: "synchronous", true: "responsive"}[responsive], func(t *testing.T) {
			c, capabilities := resolvingClient(t, responsive)
			defer c.exit()
			provider, _ := capabilities["codeActionProvider"].(map[string]any)
			if provider["resolveProvider"] != true {
				t.Fatalf("codeActionProvider = %v", capabilities["codeActionProvider"])
			}
			const uri, src = "file:///resolve.pego", "@start\nrule main = (\n  [a-cb-z] \"x\"\n)\n"
			var finding Diagnostic
			for _, d := range c.open(uri, src) {
				if d.Code == "char-class" {
					finding = d
				}
			}
			actions := listed(t, c, uri, finding)
			var suppress, merge *codeAction
			for i := range actions {
				if actions[i].Edit != nil {
					t.Fatalf("an action listed with its edit: %+v", actions[i])
				}
				switch actions[i].Title {
				case "Suppress char-class on this finding line":
					suppress = &actions[i]
				case "Merge the overlapping ranges of this character class":
					merge = &actions[i]
				}
			}
			if suppress == nil || merge == nil {
				t.Fatalf("actions = %+v", actions)
			}
			resolve := func(a codeAction) codeAction {
				var out codeAction
				c.requestInto("codeAction/resolve", a, &out)
				return out
			}
			edited := func(a codeAction) string {
				if a.Edit == nil {
					return ""
				}
				if got := a.Edit.DocumentChanges[0].TextDocument; got.Version != 1 || got.URI != uri {
					t.Fatalf("edit for %+v", got)
				}
				return applyEdits(src, a.Edit.DocumentChanges[0].Edits)
			}
			if got := edited(resolve(*merge)); got != "@start\nrule main = (\n  [a-z] \"x\"\n)\n" {
				t.Errorf("merge = %q", got)
			}
			if got := edited(resolve(*suppress)); got != "@start\nrule main = (\n// lint:ignore char-class\n  [a-cb-z] \"x\"\n)\n" {
				t.Errorf("suppress = %q", got)
			}
			// Nothing the server did not list is resolved.
			wrongTitle := *merge
			wrongTitle.Title = "Remove the unused rule main"
			wrongSuppression := *suppress
			wrongSuppression.Title = "Suppress shadowed-alternative on this finding line"
			if got := resolve(wrongSuppression); got.Edit != nil {
				t.Errorf("a suppression with another title got an edit: %+v", got)
			}
			if got := resolve(wrongTitle); got.Edit != nil {
				t.Errorf("an action with another title got an edit: %+v", got)
			}
			forged := map[string]any{"title": merge.Title, "kind": "quickfix", "data": map[string]any{
				"uri": uri, "version": 1, "key": "99:char-class", "generation": merge.Data.(map[string]any)["generation"], "action": "fix"}}
			if got := resolve(codeAction{Title: merge.Title, Data: forged["data"]}); got.Edit != nil {
				t.Errorf("a finding that does not exist got an edit: %+v", got)
			}
			other := map[string]any{"uri": uri, "version": 1, "key": "0:unreachable-rule", "generation": merge.Data.(map[string]any)["generation"], "action": "fix"}
			if got := resolve(codeAction{Title: merge.Title, Data: other}); got.Edit != nil {
				t.Errorf("a finding of another check got an edit: %+v", got)
			}
			if e := c.requestError("codeAction/resolve", map[string]any{"title": "x"}); e.Code == 0 {
				t.Error("an action without data was resolved")
			}
			// An action for an older version of the document gets no edit.
			c.notify("textDocument/didChange", map[string]any{
				"textDocument":   map[string]any{"uri": uri, "version": 2},
				"contentChanges": []any{map[string]any{"text": "@start\nrule main = (\n  [a-cb-z] \"y\"\n)\n"}},
			})
			c.diagnostics(uri)
			stale := c.call("codeAction/resolve", merge)
			if _, failed := stale["error"]; !failed {
				var out codeAction
				if err := json.Unmarshal(stale["result"], &out); err != nil || out.Edit != nil {
					t.Errorf("a stale action got an edit: %s", stale["result"])
				}
			}
		})
	}
}
