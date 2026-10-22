package lsp

import (
	"context"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/lint"
)

// A lint fix is offered for a finding only when lint.Rewrites proves its kind of change, and then
// only if the edited grammar passes the same checks as a suppression and two more: no other
// finding appears or disappears, and no rule changes its type or its status as a start rule.
// Positions inside the messages of the findings shift with an edit, so they are not compared.

// lintFix is a proven change for one finding.
type lintFix struct {
	title string
	edit  TextEdit
}

// lintFixer finds the fix of a diagnostic of one document. It computes the findings and their
// rewrites once.
type lintFixer struct {
	d        *document
	ctx      context.Context
	findings []lint.Finding
	rewrites []lint.Rewrite
}

// fixer returns the fixer of d, or nil if the document has no findings to fix.
func (d *document) fixer(ctx context.Context) *lintFixer {
	a := d.analysis()
	findings, err := a.lintFindingsContext(ctx)
	if err != nil || len(findings) == 0 || a.prog == nil {
		return nil
	}
	rewrites, err := lint.Rewrites(ctx, a.g, a.prog.DefaultStart(), findings)
	if err != nil {
		return nil
	}
	return &lintFixer{d: d, ctx: ctx, findings: findings, rewrites: rewrites}
}

// fix returns the proven fix of the diagnostic, if it has one.
func (x *lintFixer) fix(diagnostic Diagnostic, control *renderControl) (lintFix, bool) {
	if x == nil || diagnostic.Data == nil {
		return lintFix{}, false
	}
	i, ok := findingIndex(diagnostic.Data.Key)
	if !ok || i >= len(x.findings) || !x.rewrites[i].IsValid() || x.findings[i].Check != diagnostic.Code {
		return lintFix{}, false
	}
	return x.d.verifiedFixControlled(x.ctx, x.findings, i, x.rewrites[i], control)
}

// findingIndex parses the "index:check" key of a lint diagnostic.
func findingIndex(key string) (int, bool) {
	index, _, ok := strings.Cut(key, ":")
	i, err := strconv.Atoi(index)
	return i, ok && err == nil && i >= 0
}

var positionText = regexp.MustCompile(`\d+:\d+`)

// findingKeys counts the findings by check, rule and message, without the positions in the message
// (and, for an unused rule, without the message).
func findingKeys(findings []lint.Finding, skip int) map[string]int {
	keys := map[string]int{}
	for i, f := range findings {
		if i == skip {
			continue
		}
		message := positionText.ReplaceAllString(f.Message, "")
		if f.Check == lint.CheckUnreachableRule {
			// The message names the rules that call the rule, and removing one of them changes it.
			message = ""
		}
		keys[f.Check+"\x00"+f.Rule+"\x00"+message]++
	}
	return keys
}

func (d *document) verifiedFixControlled(ctx context.Context, findings []lint.Finding, i int, rw lint.Rewrite, control *renderControl) (lintFix, bool) {
	a := d.analysis()
	f := findings[i]
	start, end := a.idx.pegoOffset(rw.Start), a.idx.pegoOffset(rw.End)
	if start < 0 || start >= end || end > len(d.text) {
		return lintFix{}, false
	}
	var title string
	removed := ""
	switch f.Check {
	case lint.CheckCharClass:
		title = "Merge the overlapping ranges of " + d.text[start:end]
	case lint.CheckUselessLookahead:
		title = "Remove the lookahead that always succeeds"
		start, end = lookaheadSpan(d.text, start, end)
	case lint.CheckUnreachableRule:
		// Nothing in the grammar of the language server itself is removed on a suggestion.
		if path.Base(d.uri) == "pego.pego" {
			return lintFix{}, false
		}
		title, removed = "Remove the unused rule "+f.Rule, f.Rule
		start, end = ruleSpan(d.text, start, end)
	default:
		return lintFix{}, false
	}
	candidateText := d.text[:start] + rw.NewText + d.text[end:]
	candidate, err := analyzeContextSafely(ctx, candidateText)
	if err != nil || candidate.prog == nil || len(candidate.syntaxErrs) > 0 {
		return lintFix{}, false
	}
	after, err := candidate.lintFindingsContext(ctx)
	if err != nil || !maps.Equal(findingKeys(findings, i), findingKeys(after, -1)) {
		return lintFix{}, false
	}
	before := a.prog
	if !slices.Equal(before.Starts(), candidate.prog.Starts()) || before.DefaultStart() != candidate.prog.DefaultStart() {
		return lintFix{}, false
	}
	for _, rd := range a.g.Rules() {
		control.poll()
		if rd.Name == removed {
			continue
		}
		was, wasOK := before.RuleType(rd.Name)
		now, nowOK := candidate.prog.RuleType(rd.Name)
		if wasOK != nowOK || was != now {
			return lintFix{}, false
		}
	}
	if err := ctx.Err(); err != nil {
		return lintFix{}, false
	}
	return lintFix{title: title, edit: TextEdit{Range: a.idx.rangeOf(start, end), NewText: rw.NewText}}, true
}

// lookaheadSpan widens the span of a removed lookahead over the blanks after it, when blanks or the
// start of the line come before it, so that no double space is left.
func lookaheadSpan(text string, start, end int) (int, int) {
	before := start == 0 || strings.ContainsRune(" \t\r\n", rune(text[start-1]))
	if before {
		for end < len(text) && (text[end] == ' ' || text[end] == '\t') {
			end++
		}
	}
	return start, end
}

// ruleSpan widens the span of a removed rule to whole lines when only blanks or a comment share
// them, and over the comment lines directly above it, which document it. A comment line that holds
// a file-wide directive stays.
func ruleSpan(text string, start, end int) (int, int) {
	lineStart := strings.LastIndexByte(text[:start], '\n') + 1
	if strings.TrimSpace(text[lineStart:start]) != "" {
		return start, end
	}
	rest := end
	lineEnd := len(text)
	if i := strings.IndexByte(text[end:], '\n'); i >= 0 {
		lineEnd = end + i + 1
		rest = end + i
	} else {
		rest = len(text)
	}
	tail := strings.TrimSpace(text[end:rest])
	if tail != "" && !strings.HasPrefix(tail, "//") {
		return start, end
	}
	start, end = lineStart, lineEnd
	for start > 0 {
		prevStart := strings.LastIndexByte(text[:start-1], '\n') + 1
		line := strings.TrimSpace(text[prevStart : start-1])
		if !strings.HasPrefix(line, "//") || strings.Contains(line, "lint:file-ignore") {
			break
		}
		start = prevStart
	}
	return start, end
}
