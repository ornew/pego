package lsp

import (
	"os"
	"sort"
	"testing"
	"time"
)

// TestCodeActionResolveProbe (PEGO_LSP_RESOLVE_PROBE=1) times the code action requests that cover
// one fixable finding of a large grammar (with a rule added at the end that nothing uses) (PEGO_LSP_EDITOR_GRAMMAR selects parsers/<name>/<name>.pego,
// duckdb by default, PEGO_LSP_RESOLVE_CHECK the check of the finding): listed with their edits, listed for a client that resolves, and resolved.
func TestCodeActionResolveProbe(t *testing.T) {
	if os.Getenv("PEGO_LSP_RESOLVE_PROBE") != "1" {
		t.Skip("set PEGO_LSP_RESOLVE_PROBE=1")
	}
	name := os.Getenv("PEGO_LSP_EDITOR_GRAMMAR")
	if name == "" {
		name = "duckdb"
	}
	data, err := os.ReadFile("../../parsers/" + name + "/" + name + ".pego")
	if err != nil {
		t.Fatal(err)
	}
	// The grammar has no fixable finding of its own: add a rule that nothing uses, with a class
	// of overlapping ranges.
	data = append(data, []byte("\nrule zz_unused = [a-cb-z] \"x\"\n")...)
	median := func(f func()) time.Duration {
		var ds []time.Duration
		for range 7 {
			start := time.Now()
			f()
			ds = append(ds, time.Since(start))
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return ds[len(ds)/2]
	}
	for _, resolve := range []bool{false, true} {
		c := newServer(t)
		capabilities := map[string]any{
			"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
			"experimental": map[string]any{"pego": map[string]any{"lintDiagnostics": true, "showExplanation": true}},
		}
		if resolve {
			capabilities["textDocument"] = map[string]any{"codeAction": map[string]any{
				"dataSupport": true, "resolveSupport": map[string]any{"properties": []string{"edit"}}}}
		}
		c.request("initialize", map[string]any{"capabilities": capabilities})
		const uri = "file:///probe.pego"
		diagnostics := c.open(uri, string(data))
		check := os.Getenv("PEGO_LSP_RESOLVE_CHECK")
		if check == "" {
			check = "unreachable-rule"
		}
		var target *Diagnostic
		for i, d := range diagnostics {
			if d.Source == "pego-lint" && d.Code == check {
				target = &diagnostics[i]
				break
			}
		}
		if target == nil {
			counts := map[string]int{}
			for _, d := range diagnostics {
				counts[d.Source+" "+d.Code]++
			}
			t.Skipf("no fixable finding among %v", counts)
		}
		list := func() []codeAction {
			var actions []codeAction
			c.requestInto("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri}, "range": target.Range,
				"context": map[string]any{"diagnostics": []Diagnostic{*target}}}, &actions)
			return actions
		}
		actions := list()
		t.Logf("resolve=%v finding %s at line %d: %d actions; list median %v", resolve, target.Code, target.Range.Start.Line+1, len(actions), median(func() { list() }))
		if resolve {
			for _, a := range actions {
				if a.Data == nil {
					continue
				}
				var out codeAction
				d := median(func() { c.requestInto("codeAction/resolve", a, &out) })
				t.Logf("  resolve %q: median %v, edit %v", a.Title, d, out.Edit != nil)
			}
		}
		c.exit()
	}
}
