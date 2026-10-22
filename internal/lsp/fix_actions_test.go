package lsp

import (
	"context"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/lint"
	"strings"
	"testing"
)

// fixActions opens src with a client that takes versioned edits and returns, for each lint
// diagnostic, the edited text of each of its fix actions (other than the suppression).
func fixActions(t *testing.T, uri, src string) map[string][]string {
	t.Helper()
	c := newServer(t)
	defer c.exit()
	capabilities := map[string]any{
		"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
		"experimental": map[string]any{"pego": map[string]any{"lintDiagnostics": true, "showExplanation": true}},
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
			[]string{"Merge the overlapping ranges of [a-cb-z] => @start\nrule main = [a-z] \"x\"\n"}},
		{"a range that spans punctuation", "@start\nrule main = [A-z]\n", "char-class", nil},
		{"a lookahead that always succeeds", "@start\nrule main = \"a\" &\"b\"* \"c\"\n", "useless-lookahead",
			[]string{"Remove the lookahead that always succeeds => @start\nrule main = \"a\" \"c\"\n"}},
		{"a lookahead with a call", "@start\nrule main = &w* \"c\"\nrule w = \"a\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard in a labelled group", "@start\nrule main = g:(\"a\" - &\"b\"* \"c\") => len($g.Value)\n", "useless-lookahead", nil},
		{"a lookahead under a discard before a literal", "@start\nrule main = \"a\" - &\"b\"* \"c\" => \"k\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard before a group", "@start\nrule main = \"a\" - &\"b\"* (\"c\" \"d\") => \"k\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard before a call", "@start\nrule main = x:(\"a\" - &\"b\"* w) => len($x.Value)\nrule w = \"c\"\n", "useless-lookahead", nil},
		{"a lookahead under a discard in a repeated group", "@start\nrule main = xs:(\"a\" - &\"b\"* \"c\")+ => len($xs)\n", "useless-lookahead", nil},
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
			got := fixActions(t, "file:///fix.pego", tc.src)[tc.check]
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("fixes = %q, want %q", got, tc.want)
			}
		})
	}
	// The grammar of the language server itself is not edited on a suggestion.
	if got := fixActions(t, "file:///repo/pego.pego", "@start\nrule main = \"a\"\nrule x = \"b\"\n")["unreachable-rule"]; got != nil {
		t.Errorf("fixes for pego.pego = %q", got)
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
