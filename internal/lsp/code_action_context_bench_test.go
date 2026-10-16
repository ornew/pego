package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const codeActionURI = "file:///code-action.pego"

var codeActionBenchmarkResult any

func codeActionSource(newline string) string {
	return strings.Join([]string{"rule main = (", "  a:\"a\" b:\"b\"", ") => $a"}, newline)
}
func codeActionServer(a *analysis, ctx context.Context, enabled, explain, edit bool) *Server {
	s := &Server{docs: map[string]*document{codeActionURI: {uri: codeActionURI, text: a.text, version: 7, generation: 11, an: a}}, state: stateInitialized, requestContext: ctx}
	s.client.Experimental.Pego.LintDiagnostics = enabled
	s.client.Experimental.Pego.ShowExplanation = explain
	s.client.Workspace.WorkspaceEdit.DocumentChanges = edit
	return s
}
func codeActionAnalysis(tb testing.TB, text string) *analysis {
	tb.Helper()
	a := analyze(text)
	if err := a.cacheLintContext(context.Background()); err != nil {
		tb.Fatal(err)
	}
	return a
}
func codeActionTarget(tb testing.TB, a *analysis) Diagnostic {
	tb.Helper()
	for _, d := range codeActionServer(a, nil, true, true, true).docs[codeActionURI].lintDiagnostics() {
		if d.Code == "unused-capture" {
			return d
		}
	}
	tb.Fatal("missing unused-capture diagnostic")
	return Diagnostic{}
}
func codeActionMessage(r Range, diagnostics []Diagnostic, only []string) *incoming {
	params, _ := json.Marshal(map[string]any{"textDocument": textDocumentIdentifier{URI: codeActionURI}, "range": r, "context": map[string]any{"diagnostics": diagnostics, "only": only}})
	return &incoming{Method: "textDocument/codeAction", Params: params}
}
func codeActionSupplied(d Diagnostic, count int, match bool) []Diagnostic {
	out := make([]Diagnostic, count)
	for i := range out {
		out[i] = d
		data := *d.Data
		data.Key = fmt.Sprintf("other%d", i)
		out[i].Data = &data
	}
	if match {
		out = append(out, d)
	}
	return out
}

type codeActionCase struct {
	name                   string
	a                      *analysis
	msg                    *incoming
	enabled, explain, edit bool
}

func codeActionResponseCorpus(tb testing.TB) []codeActionCase {
	tb.Helper()
	var cases []codeActionCase
	add := func(name string, a *analysis, msg *incoming, enabled, explain, edit bool) {
		cases = append(cases, codeActionCase{name, a, msg, enabled, explain, edit})
	}
	var a *analysis
	var target Diagnostic
	for _, nl := range []struct{ name, text string }{{"lf", "\n"}, {"crlf", "\r\n"}, {"cr", "\r"}} {
		a = codeActionAnalysis(tb, codeActionSource(nl.text))
		target = codeActionTarget(tb, a)
		for bits := range 8 {
			add(fmt.Sprintf("%s/capabilities%d", nl.name, bits), a, codeActionMessage(target.Range, []Diagnostic{target}, nil), bits&1 != 0, bits&2 != 0, bits&4 != 0)
		}
	}
	a = codeActionAnalysis(tb, codeActionSource("\n"))
	target = codeActionTarget(tb, a)
	for _, name := range []string{"data", "uri", "version", "generation", "key", "range", "code", "message", "source", "severity"} {
		d := target
		data := *d.Data
		d.Data = &data
		switch name {
		case "data":
			d.Data = nil
		case "uri":
			d.Data.URI = "file:///other.pego"
		case "version":
			d.Data.Version++
		case "generation":
			d.Data.Generation++
		case "key":
			d.Data.Key += "stale"
		case "range":
			d.Range.End.Character++
		case "code":
			d.Code = "other"
		case "message":
			d.Message += "other"
		case "source":
			d.Source = "other"
		case "severity":
			d.Severity++ // Severity is deliberately not part of identity.
		}
		add("modified/"+name, a, codeActionMessage(target.Range, []Diagnostic{d}, nil), true, true, true)
	}
	for _, only := range []string{"", "quickfix", "refactor", "quickfix.fixAll"} {
		add("only/"+only, a, codeActionMessage(target.Range, []Diagnostic{target}, []string{only}), true, true, true)
	}
	add("no-diagnostics", a, codeActionMessage(target.Range, nil, nil), true, true, true)
	add("outside-range", a, codeActionMessage(Range{Start: Position{Line: 100}, End: Position{Line: 101}}, []Diagnostic{target}, nil), true, true, true)
	add("late-match", a, codeActionMessage(target.Range, codeActionSupplied(target, 128, true), nil), true, true, true)
	for _, fixture := range []struct{ name, text string }{
		{"existing-ignore-text", "rule main = (\n a:\"lint:ignore\" b:\"b\"\n) => $a"},
		{"multiline-token", "rule main = (\n a:\"a\" /*\n comment\n */ b:\"b\"\n) => $a"},
		{"mixed-newlines", "/* comment\r\n */\r" + codeActionSource("\r")},
	} {
		b := codeActionAnalysis(tb, fixture.text)
		d := codeActionTarget(tb, b)
		add(fixture.name, b, codeActionMessage(d.Range, []Diagnostic{d}, nil), true, true, true)
	}
	b := codeActionAnalysis(tb, "rule main = \"a\"\nrule unused = \"b\"")
	ds := codeActionServer(b, nil, true, true, true).docs[codeActionURI].lintDiagnostics()
	if len(ds) == 0 {
		tb.Fatal("missing unreachable-rule diagnostic")
	}
	add("declaration-wide", b, codeActionMessage(Range{End: Position{Line: 100}}, ds, nil), true, true, true)
	for _, text := range []string{"rule main = )", "rule main = missing"} {
		b := codeActionAnalysis(tb, text)
		add("invalid/"+text, b, codeActionMessage(target.Range, []Diagnostic{target}, nil), true, true, true)
	}
	for _, name := range []string{"missing-params", "bad-params", "unknown-document"} {
		msg := codeActionMessage(target.Range, []Diagnostic{target}, nil)
		switch name {
		case "missing-params":
			msg.Params = nil
		case "bad-params":
			msg.Params = json.RawMessage(`{"textDocument":1}`)
		case "unknown-document":
			msg.Params = json.RawMessage(strings.ReplaceAll(string(msg.Params), codeActionURI, "file:///unknown.pego"))
		}
		add(name, a, msg, true, true, true)
	}
	return cases
}

// The corpus only uses the existing request API and runs unchanged against the parent.
func TestCodeActionResponseFingerprints(t *testing.T) {
	for _, tc := range codeActionResponseCorpus(t) {
		ctx, cancel := context.WithCancel(context.Background())
		value, err := codeActionServer(tc.a, ctx, tc.enabled, tc.explain, tc.edit).request(tc.msg)
		cancel()
		t.Logf("response %s %x", tc.name, sha256.Sum256(occurrenceResponseJSON(t, value, err)))
	}
}

func codeActionBenchmarkCases(tb testing.TB) []codeActionCase {
	tb.Helper()
	a := codeActionAnalysis(tb, codeActionSource("\n"))
	target := codeActionTarget(tb, a)
	msg := codeActionMessage(target.Range, []Diagnostic{target}, nil)
	only := make([]string, 4096)
	for i := range only {
		only[i] = "refactor"
	}
	cases := []codeActionCase{
		{"disabled", a, msg, false, true, true},
		{"explanation", a, msg, true, true, false},
		{"suppression", a, msg, true, true, true},
		{"only4096", a, codeActionMessage(target.Range, []Diagnostic{target}, only), true, true, true},
		{"supplied4096-match", a, codeActionMessage(target.Range, codeActionSupplied(target, 4096, true), nil), true, true, false},
		{"supplied4096-miss", a, codeActionMessage(target.Range, codeActionSupplied(target, 4096, false), nil), true, true, false},
	}
	long := codeActionAnalysis(tb, "rule atom = \"a\"\nrule main = (\n"+strings.Repeat("atom ", 512)+"a:\"a\" b:\"b\"\n) => $a")
	d := codeActionTarget(tb, long)
	cases = append(cases, codeActionCase{"suppression512", long, codeActionMessage(d.Range, []Diagnostic{d}, nil), true, true, true})
	var text strings.Builder
	text.WriteString("rule main = \"a\"\n")
	for i := range 128 {
		fmt.Fprintf(&text, "rule unused%d = \"b\"\n", i)
	}
	many := codeActionAnalysis(tb, text.String())
	ds := codeActionServer(many, nil, true, true, false).docs[codeActionURI].lintDiagnostics()
	cases = append(cases, codeActionCase{"findings128", many, codeActionMessage(Range{End: Position{Line: 1000}}, ds, nil), true, true, false})
	return cases
}

func BenchmarkCodeActionRequests(b *testing.B) {
	for _, tc := range codeActionBenchmarkCases(b) {
		for _, active := range []bool{false, true} {
			mode := "ordinary"
			if active {
				mode = "cancellable"
			}
			b.Run(tc.name+"/"+mode, func(b *testing.B) {
				var ctx context.Context
				if active {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(context.Background())
					defer cancel()
				}
				s := codeActionServer(tc.a, ctx, tc.enabled, tc.explain, tc.edit)
				b.ReportAllocs()
				for b.Loop() {
					var err *rpcError
					codeActionBenchmarkResult, err = s.request(tc.msg)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
