package lsp

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestNegotiatedDocumentationProtocol(t *testing.T) {
	for _, formats := range [][]string{nil, {"plaintext"}, {"markdown"}, {"plaintext", "markdown"}} {
		t.Run(strings.Join(formats, "+"), func(t *testing.T) {
			c := newServer(t)
			c.request("initialize", map[string]any{"capabilities": map[string]any{"textDocument": map[string]any{
				"hover": map[string]any{"contentFormat": formats}, "completion": map[string]any{"completionItem": map[string]any{"documentationFormat": formats}},
			}}})
			defer c.exit()
			uri := "file:///formats.pego"
			c.open(uri, "rule main = helper\n// Uses `text`.\nrule helper = \"a\"")
			var h hover
			c.requestInto("textDocument/hover", at(uri, 0, 13), &h)
			kind := "plaintext"
			for _, f := range formats {
				if f == "markdown" || f == "plaintext" {
					kind = f
					break
				}
			}
			if h.Contents.Kind != kind || !strings.Contains(h.Contents.Value, "rule helper") {
				t.Fatalf("hover = %+v", h)
			}
			if kind == "plaintext" && strings.Contains(h.Contents.Value, "`") {
				t.Fatalf("markup in plaintext: %q", h.Contents.Value)
			}
			var completions completionList
			c.requestInto("textDocument/completion", at(uri, 0, 18), &completions)
			found := false
			for _, item := range completions.Items {
				if item.Label == "helper" && item.Documentation != nil {
					found = true
					if item.Documentation.Kind != kind {
						t.Fatalf("completion = %+v", item)
					}
				}
			}
			if !found {
				t.Fatal("missing documented helper completion")
			}
		})
	}
}

func TestCompletionCurrentContextsProtocol(t *testing.T) {
	for _, tc := range []struct {
		source       string
		want, absent []string
	}{
		{"type Box struct { Text string; Count int }\nrule main = \"a\" => Box{|}", []string{"Text", "Count"}, []string{"len", "rule"}},
		{"type Box struct { Text string; Count int }\nrule main = \"a\" => Box{Text: \"a\", |}", []string{"Count"}, []string{"Text", "len"}},
		{"rule main = \"a\" #recover(|)", []string{"skip"}, []string{"len", "main"}},
		{"rule main = \"a\" #recover(skip=|)", []string{"main"}, []string{"len", "skip"}},
		{"rule expr = operators {\n operand [0-9]\n level sum { infix left \"+\" }\n level product { infix left \"*\" }\n}\nrule main = level(expr, |)", []string{"sum", "product"}, []string{"expr", "len", "rule"}},
		{"rule main = \"a\" // |", nil, []string{"main", "rule", "skip"}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			c := newInitialized(t)
			defer c.exit()
			uri := "file:///contexts.pego"
			off := strings.Index(tc.source, "|")
			source := strings.Replace(tc.source, "|", "", 1)
			c.open(uri, source)
			position := newTextIndex(source).position(off)
			var got completionList
			c.requestInto("textDocument/completion", at(uri, position.Line, position.Character), &got)
			labels := map[string]bool{}
			for _, item := range got.Items {
				labels[item.Label] = true
			}
			for _, label := range tc.want {
				if !labels[label] {
					t.Errorf("missing %s in %+v", label, got.Items)
				}
			}
			for _, label := range tc.absent {
				if labels[label] {
					t.Errorf("unexpected %s in %+v", label, got.Items)
				}
			}
		})
	}
}

func TestCommentAssociativityCurrentSyntaxProtocol(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		for _, assoc := range []string{"left", "right", "none"} {
			c := newInitialized(t)
			uri := "file:///associativity.pego"
			source := "rule main = operators {" + newline + "operand [0-9]" + newline + "level { infix " + assoc + " ( // 😀 gap" + newline + "// second" + newline + "\"+\" ) }" + newline + "}"
			if diags := c.open(uri, source); len(diags) != 0 {
				t.Fatalf("current grammar diagnostics: %+v", diags)
			}
			var result struct {
				Data []int `json:"data"`
			}
			c.requestInto("textDocument/semanticTokens/full", docParams(uri), &result)
			idx := newTextIndex(source)
			line, column := 0, 0
			found, comments := false, 0
			for i := 0; i < len(result.Data); i += 5 {
				if result.Data[i] > 0 {
					column = 0
				}
				line += result.Data[i]
				column += result.Data[i+1]
				start := idx.offset(Position{line, column})
				end := idx.offset(Position{line, column + result.Data[i+2]})
				if source[start:end] == assoc && result.Data[i+3] == int(semKeyword) {
					found = true
				}
				if result.Data[i+3] == int(semComment) {
					comments++
				}
			}
			if !found || comments != 2 {
				t.Errorf("associativity=%v comments=%d data=%v", found, comments, result.Data)
			}
			c.exit()
		}
	}
}

func TestPlainDocumentationPreservesCode(t *testing.T) {
	got := documentation("```pego\nrule main = \"*`*\"\n```\n\n*Inferred type.*\nUses `text`.\nstart_pos_name foo__bar__baz café_pos_nom\n__Strong__ and _emphasized_, plus `_literal_` and __start_pos_name__.", []string{"plaintext"})
	want := markupContent{Kind: "plaintext", Value: "rule main = \"*`*\"\n\nInferred type.\nUses text.\nstart_pos_name foo__bar__baz café_pos_nom\nStrong and emphasized, plus _literal_ and start_pos_name."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestRejectedHeaderCommentsProtocol(t *testing.T) {
	for _, source := range []string{
		"rule main = \"a\" @ /* gap */ error(message=\"e\")",
		"rule main = operators { operand [0-9]; level { infix /* gap */ left \"+\" } }",
		"rule main = operators {\noperand [0-9]\nlevel { infix // gap\nleft \"+\" }\n}",
	} {
		c := newInitialized(t)
		if diagnostics := c.open("file:///invalid-header.pego", source); len(diagnostics) == 0 {
			t.Errorf("invalid header accepted: %s", source)
		}
		c.exit()
	}
}

func TestAnalysisQueueCoalescesAndCancels(t *testing.T) {
	started := make(chan analysisSnapshot, 4)
	release := make(chan struct{})
	q := newAnalysisQueue(func(ctx context.Context, snapshot analysisSnapshot) (*analysis, error) {
		started <- snapshot
		if snapshot.Version == 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}
		}
		return newAnalysis(snapshot.Text), nil
	})
	defer q.stop()
	q.submit(analysisSnapshot{URI: "file:///a", Version: 1, Generation: 1})
	first := <-started
	if first.Version != 1 {
		t.Fatalf("first = %+v", first)
	}
	// Hold the queue lock while replacing pending work to make coalescing deterministic.
	q.mu.Lock()
	ctx2, cancel2 := context.WithCancel(q.ctx)
	q.pending[first.URI] = pendingAnalysis{snapshot: analysisSnapshot{URI: first.URI, Version: 2, Generation: 2}, ctx: ctx2, cancel: cancel2}
	q.order = append(q.order, first.URI)
	q.mu.Unlock()
	q.submit(analysisSnapshot{URI: first.URI, Version: 3, Generation: 3})
	latest := <-started
	if latest.Version != 3 {
		t.Fatalf("obsolete work started: %+v", latest)
	}
	result := <-q.results
	if !matchesSnapshot(latest, result.Snapshot) || matchesSnapshot(first, result.Snapshot) {
		t.Fatalf("snapshot guard: %+v", result)
	}
	if ctx2.Err() != context.Canceled {
		t.Fatal("pending generation was not cancelled")
	}
}

func TestAnalyzeContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err := analyzeContext(ctx, "rule main = \"a\"")
	if a != nil || err != context.Canceled {
		t.Fatalf("analysis=%v error=%v", a, err)
	}
}

func TestGuardedCodeActionsProtocol(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(newline, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
			c := newServer(t)
			c.request("initialize", map[string]any{"capabilities": map[string]any{
				"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
				"experimental": map[string]any{"pego": map[string]any{"lintDiagnostics": true, "showExplanation": true}},
			}})
			defer c.exit()
			uri := "file:///actions.pego"
			source := "rule main = (" + newline + "  a:\"a\" b:\"b\"" + newline + ") => $a"
			diagnostics := c.open(uri, source)
			var target Diagnostic
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "unused-capture" {
					target = diagnostic
					break
				}
			}
			if target.Data == nil || target.Severity != 2 || target.Source != "pego-lint" {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
			params := map[string]any{"textDocument": map[string]any{"uri": uri}, "range": target.Range, "context": map[string]any{"diagnostics": []Diagnostic{target}}}
			var actions []codeAction
			c.requestInto("textDocument/codeAction", params, &actions)
			if len(actions) != 2 || actions[0].Command == nil || actions[0].Command.Command != "pego.showExplanation" || actions[1].Edit == nil {
				t.Fatalf("actions = %+v", actions)
			}
			change := actions[1].Edit.DocumentChanges[0]
			if change.TextDocument.URI != uri || change.TextDocument.Version != target.Data.Version {
				t.Fatalf("unversioned edit: %+v", change)
			}
			if !strings.HasSuffix(change.Edits[0].NewText, newline) {
				t.Fatalf("wrong newline: %q", change.Edits[0].NewText)
			}
			edited := applyEdits(source, change.Edits)
			candidate := analyze(edited)
			if candidate.prog == nil || len(candidate.syntaxErrs) != 0 {
				t.Fatalf("invalid suppression: %s %+v", edited, candidate.diags)
			}
			for _, f := range candidate.lintFindings() {
				if f.Check == target.Code {
					t.Fatalf("finding remains: %+v", f)
				}
			}
			stale := target
			data := *target.Data
			data.Version++
			stale.Data = &data
			params["context"] = map[string]any{"diagnostics": []Diagnostic{stale}}
			c.requestInto("textDocument/codeAction", params, &actions)
			if len(actions) != 0 {
				t.Fatalf("stale diagnostic actions: %+v", actions)
			}
			params["context"] = map[string]any{"diagnostics": []Diagnostic{target}, "only": []string{"refactor"}}
			c.requestInto("textDocument/codeAction", params, &actions)
			if len(actions) != 0 {
				t.Fatalf("unexpected refactor actions: %+v", actions)
			}
		})
	}
}

func TestLintDiagnosticsPrototypeIsOptIn(t *testing.T) {
	c := newInitialized(t)
	defer c.exit()
	uri := "file:///ordinary.pego"
	if got := c.open(uri, "rule main = \"a\" / \"ab\""); len(got) != 0 {
		t.Fatalf("changed default diagnostics: %+v", got)
	}
	var actions []codeAction
	c.requestInto("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri}, "range": Range{}, "context": map[string]any{"diagnostics": []Diagnostic{}}}, &actions)
	if len(actions) != 0 {
		t.Fatalf("non-opt-in actions: %+v", actions)
	}
}
