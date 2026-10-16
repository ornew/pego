package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ornew/pego/internal/lint"
)

type findingData struct {
	URI        string `json:"uri"`
	Version    int    `json:"version"`
	Key        string `json:"key"`
	Generation uint64 `json:"generation,omitempty"`
}

type textDocumentEdit struct {
	TextDocument versionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

type actionEdit struct {
	DocumentChanges []textDocumentEdit `json:"documentChanges"`
}
type actionCommand struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments"`
}
type codeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind"`
	Diagnostics []Diagnostic   `json:"diagnostics"`
	Edit        *actionEdit    `json:"edit,omitempty"`
	Command     *actionCommand `json:"command,omitempty"`
}

// lintFindings shares the already successful compilation with the linter.
// Without main, reachability is omitted because no editor start-rule setting exists.
func (a *analysis) lintFindings() []lint.Finding {
	findings, _ := a.lintFindingsContext(context.Background())
	return findings
}

func (a *analysis) lintFindingsContext(ctx context.Context) ([]lint.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.prog == nil || len(a.syntaxErrs) > 0 {
		return nil, nil
	}
	if a.lintCached {
		return a.lintCache, nil
	}
	start := ""
	if a.rules["main"] != nil {
		start = "main"
	}
	return lint.RunContext(ctx, a.g, start, lint.Options{})
}

// cacheLintContext commits a complete lint result; cancelled work retains no partial cache.
func (a *analysis) cacheLintContext(ctx context.Context) error {
	findings, err := a.lintFindingsContext(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.lintCache, a.lintCached = findings, true
	return nil
}

// lintDiagnosticHook is a controlled mapping barrier for protocol regressions.
// Production leaves it nil; tests reset it after all server workers have joined.
var lintDiagnosticHook func(context.Context, analysisSnapshot, int)

// preparePublicationContext builds the complete push payload in the snapshot
// worker. The actor only checks snapshot identity and writes this finished data.
func (a *analysis) preparePublicationContext(ctx context.Context, snapshot analysisSnapshot, withLint bool) error {
	diagnostics := append([]Diagnostic(nil), a.diags...)
	if withLint {
		if err := a.cacheLintContext(ctx); err != nil {
			return err
		}
		document := &document{uri: snapshot.URI, text: snapshot.Text, version: snapshot.Version, generation: snapshot.Generation, an: a}
		findings, err := document.lintDiagnosticsContext(ctx)
		if err != nil {
			return err
		}
		diagnostics = append(diagnostics, findings...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.publicationDiags = diagnostics
	return nil
}

func (d *document) lintDiagnostics() []Diagnostic {
	diagnostics, _ := d.lintDiagnosticsContext(context.Background())
	return diagnostics
}

func (d *document) lintDiagnosticsContext(ctx context.Context) ([]Diagnostic, error) {
	a := d.analysis()
	findings, err := a.lintFindingsContext(ctx)
	if err != nil {
		return nil, err
	}
	var out []Diagnostic
	for i, f := range findings {
		if i%256 == 0 {
			if lintDiagnosticHook != nil {
				lintDiagnosticHook(ctx, d.snapshot(), i)
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if !f.Pos.IsValid() {
			continue
		}
		severity := 4
		if f.Severity == lint.Error {
			severity = 1
		} else if f.Severity == lint.Warning {
			severity = 2
		}
		out = append(out, Diagnostic{Range: a.errorRange(f.Pos, false), Severity: severity, Source: "pego-lint", Message: f.Message, Code: f.Check,
			Data: &findingData{URI: d.uri, Version: d.version, Generation: d.generation, Key: fmt.Sprintf("%d:%s", i, f.Check)}})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) codeActionsControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Range        Range                  `json:"range"`
		Context      struct {
			Diagnostics []Diagnostic `json:"diagnostics"`
			Only        []string     `json:"only"`
		} `json:"context"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	out := []codeAction{}
	if !s.client.Experimental.Pego.LintDiagnostics {
		return out, nil
	}
	if len(p.Context.Only) > 0 {
		allowed := false
		for _, kind := range p.Context.Only {
			control.poll()
			if kind == "" || kind == "quickfix" {
				allowed = true
			}
		}
		if !allowed {
			return out, nil
		}
	}
	ctx := s.requestContext
	if ctx == nil {
		ctx = context.Background()
	}
	version := d.version
	current, lintErr := d.lintDiagnosticsContext(ctx)
	if lintErr != nil {
		return nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
	}
	for _, diagnostic := range current {
		control.poll()
		if ctx.Err() != nil {
			return nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
		}
		if !rangesOverlap(p.Range, diagnostic.Range) {
			continue
		}
		matched := false
		for _, supplied := range p.Context.Diagnostics {
			control.poll()
			if supplied.Data != nil && *supplied.Data == *diagnostic.Data && supplied.Range == diagnostic.Range && supplied.Code == diagnostic.Code && supplied.Message == diagnostic.Message && supplied.Source == diagnostic.Source {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if s.client.Experimental.Pego.ShowExplanation {
			explanation := ""
			for _, check := range lint.Checks {
				control.poll()
				if check.Name == diagnostic.Code {
					explanation = check.Summary
					break
				}
			}
			control.poll()
			out = append(out, codeAction{Title: "Explain " + diagnostic.Code, Kind: "quickfix", Diagnostics: []Diagnostic{diagnostic}, Command: &actionCommand{Title: "Explain lint finding", Command: "pego.showExplanation", Arguments: []any{diagnostic.Code, diagnostic.Message, explanation}}})
		}
		if s.client.Workspace.WorkspaceEdit.DocumentChanges {
			edit, ok := d.suppressionEditControlled(ctx, diagnostic, control)
			control.check()
			if ok {
				control.poll()
				out = append(out, codeAction{Title: "Suppress " + diagnostic.Code + " on this finding line", Kind: "quickfix", Diagnostics: []Diagnostic{diagnostic}, Edit: &actionEdit{DocumentChanges: []textDocumentEdit{{TextDocument: versionedTextDocumentIdentifier{URI: d.uri, Version: version}, Edits: []TextEdit{edit}}}}})
			}
		}
	}
	if d.version != version {
		return []codeAction{}, nil
	}
	return out, nil
}

func rangesOverlap(a, b Range) bool {
	before := func(a, b Position) bool { return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character }
	return !before(a.End, b.Start) && !before(b.End, a.Start)
}

// suppressionEdit is deliberately conservative: validate the edited grammar and
// verify that the targeted finding disappears. Human-readable Fix is never an edit.
func (d *document) suppressionEditControlled(ctx context.Context, diagnostic Diagnostic, control *renderControl) (TextEdit, bool) {
	control.poll()
	if ctx.Err() != nil {
		return TextEdit{}, false
	}
	if diagnostic.Code == lint.CheckDirective {
		return TextEdit{}, false
	}
	a := d.analysis()
	line := diagnostic.Range.Start.Line
	if line < 0 || line >= len(a.idx.lines) {
		return TextEdit{}, false
	}
	start := a.idx.lines[line]
	end := len(d.text)
	if line+1 < len(a.idx.lines) {
		end = a.idx.lines[line+1]
	}
	// Never splice a new directive into an existing comment or multiline token.
	for _, tok := range a.toks {
		control.poll()
		if tok.start < start && tok.end > start {
			return TextEdit{}, false
		}
	}
	if strings.Contains(d.text[start:end], "lint:ignore") {
		return TextEdit{}, false
	}
	eol := "\n"
	if strings.Contains(d.text, "\r\n") {
		eol = "\r\n"
	} else if strings.Contains(d.text, "\r") {
		eol = "\r"
	}
	edit := TextEdit{Range: Range{Start: Position{Line: line}, End: Position{Line: line}}, NewText: "// lint:ignore " + diagnostic.Code + eol}
	candidate, failure := analyzeContextSafely(ctx, d.text[:start]+edit.NewText+d.text[start:])
	if failure != nil || candidate.prog == nil || len(candidate.syntaxErrs) > 0 {
		return TextEdit{}, false
	}
	if ctx.Err() != nil {
		return TextEdit{}, false
	}
	findings, lintErr := candidate.lintFindingsContext(ctx)
	if lintErr != nil {
		return TextEdit{}, false
	}
	for i, finding := range findings {
		if i%256 == 0 {
			if ctx.Err() != nil {
				return TextEdit{}, false
			}
		}
		if finding.Check == lint.CheckDirective && finding.Pos.Line == line+1 {
			return TextEdit{}, false
		}
		if finding.Check == diagnostic.Code && finding.Message == diagnostic.Message && candidate.errorRange(finding.Pos, false).Start.Line == line+1 {
			return TextEdit{}, false
		}
	}
	// A directive before a declaration suppresses the whole rule. Do not offer
	// the narrower line-scoped title when the insertion would have that scope.
	for i, def := range a.defs {
		if i%256 == 0 {
			if ctx.Err() != nil {
				return TextEdit{}, false
			}
		}
		if def.kwStart >= start && def.kwStart < end {
			return TextEdit{}, false
		}
	}
	if ctx.Err() != nil {
		return TextEdit{}, false
	}
	return edit, true
}
