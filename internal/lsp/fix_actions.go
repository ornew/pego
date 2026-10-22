package lsp

import (
	"context"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/engine"
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

// fixerFor returns the fixer of the finding at index i alone, computing only its rewrite.
func (d *document) fixerFor(ctx context.Context, i int) *lintFixer {
	a := d.analysis()
	findings, err := a.lintFindingsContext(ctx)
	if err != nil || i < 0 || i >= len(findings) || a.prog == nil {
		return nil
	}
	one, err := lint.Rewrites(ctx, a.g, a.prog.DefaultStart(), findings[i:i+1])
	if err != nil {
		return nil
	}
	rewrites := make([]lint.Rewrite, len(findings))
	rewrites[i] = one[0]
	return &lintFixer{d: d, ctx: ctx, findings: findings, rewrites: rewrites}
}

// hasRewrite reports whether lint.Rewrites proves a rewrite for the finding at index i. It does
// not verify the edited document.
func (d *document) hasRewrite(ctx context.Context, i int) bool {
	x := d.fixerFor(ctx, i)
	return x != nil && x.rewrites[i].IsValid()
}

// fixTitle returns the title of the fix that a finding can have, without computing the fix. A
// title does not depend on the rewrite, so it is the same before and after the fix is resolved.
func (d *document) fixTitle(f lint.Finding) (string, bool) {
	switch f.Check {
	case lint.CheckCharClass:
		return "Merge the overlapping ranges of this character class", true
	case lint.CheckUselessLookahead:
		return "Remove the lookahead that always succeeds", true
	case lint.CheckUnreachableRule:
		return "Remove the unused rule " + f.Rule, path.Base(d.uri) != "pego.pego"
	}
	return "", false
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
// (and, for an unused rule, without the message). The finding at index skip and the findings of the
// rule gone, which is deleted, are left out.
func findingKeys(findings []lint.Finding, skip int, gone string) map[string]int {
	keys := map[string]int{}
	for i, f := range findings {
		if i == skip || gone != "" && f.Rule == gone {
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
	// Nothing in the grammar of the language server itself is removed on a suggestion.
	title, ok := d.fixTitle(f)
	if !ok {
		return lintFix{}, false
	}
	removed := ""
	switch f.Check {
	case lint.CheckUselessLookahead:
		start, end = lookaheadSpan(d.text, start, end)
	case lint.CheckUnreachableRule:
		removed = f.Rule
		start, end = ruleSpan(d.text, start, end)
	}
	candidateText := d.text[:start] + rw.NewText + d.text[end:]
	candidate, err := analyzeContextSafely(ctx, candidateText)
	if err != nil || candidate.prog == nil || len(candidate.syntaxErrs) > 0 {
		return lintFix{}, false
	}
	after, err := candidate.lintFindingsContext(ctx)
	if err != nil || !maps.Equal(findingKeys(findings, i, removed), findingKeys(after, -1, "")) {
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
		// The type of a rule can hide a change inside it, such as a discard that moved to
		// another element, so the shape of every subexpression must agree too.
		wasShape, wasOK := before.ShapeSignature(rd.Name, control.poll)
		nowShape, nowOK := candidate.prog.ShapeSignature(rd.Name, control.poll)
		if wasOK != nowOK || wasShape != nowShape {
			return lintFix{}, false
		}
	}
	// The rules of the inline actions are not declared rules: compare them in the lowered grammars.
	if !d.inlineActionsAgree(ctx, candidate, removed, control) {
		return lintFix{}, false
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

// ruleSpan widens the span of a removed rule so that nothing of its definition is left behind:
//   - a block comment that shares the line before the rule documents it and goes with it;
//   - a ";" that separates it from the next definition on the line, or from the previous one, goes
//     with it;
//   - when only blanks or a comment share its lines, the whole lines go, and so do the comment
//     lines directly above it (line comments and block comments), which document it.
//
// A comment that holds a file-wide directive stays.
func ruleSpan(text string, start, end int) (int, int) {
	lineStart := strings.LastIndexByte(text[:start], '\n') + 1
	before := text[lineStart:start]
	if c := blockCommentBefore(before); c >= 0 {
		if strings.TrimSpace(before[:c]) != "" {
			// A comment that stays comes first: drop what follows it on the line, and the blank.
			return lineStart + len(strings.TrimRight(before[:c], " \t")), end
		}
		start, before = lineStart+c, ""
	}
	if strings.TrimSpace(before) != "" {
		// Another definition comes first on the line: take the separator between them.
		kept := strings.TrimRight(before, " \t")
		if strings.HasSuffix(kept, ";") {
			start = lineStart + len(strings.TrimRight(kept[:len(kept)-1], " \t"))
		} else if lineRest := text[end:]; strings.TrimSpace(lineRest[:strings.IndexByte(lineRest+"\n", '\n')]) == "" {
			// Nothing follows the rule, and what precedes it stays: leave no blank after it.
			start = lineStart + len(kept)
		}
		return start, end
	}
	lineEnd, rest := len(text), len(text)
	if i := strings.IndexByte(text[end:], '\n'); i >= 0 {
		lineEnd, rest = end+i+1, end+i
	}
	tail := strings.TrimSpace(text[end:rest])
	separated := strings.HasPrefix(tail, ";")
	if separated {
		tail = strings.TrimSpace(tail[1:])
	}
	if tail != "" && !strings.HasPrefix(tail, "//") {
		if separated {
			// Another definition follows on the line: take the separator before it.
			after := end
			for after < len(text) && strings.IndexByte(" \t", text[after]) >= 0 {
				after++
			}
			after++ // the ";"
			for after < len(text) && strings.IndexByte(" \t", text[after]) >= 0 {
				after++
			}
			return start, after
		}
		return start, end
	}
	start, end = lineStart, lineEnd
	for start > 0 {
		prevStart := strings.LastIndexByte(text[:start-1], '\n') + 1
		first := commentLinesAbove(text, prevStart, start-1)
		if first < 0 {
			break
		}
		start = first
	}
	return start, end
}

// blockComments splits line into the block comments it consists of, as their start indexes, or
// returns nil if it holds anything else (or nothing).
func blockComments(line string) []int {
	var starts []int
	for i := 0; ; {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
		if i == len(line) {
			return starts
		}
		if !strings.HasPrefix(line[i:], "/*") {
			return nil
		}
		end := strings.Index(line[i+2:], "*/")
		if end < 0 {
			return nil
		}
		starts = append(starts, i)
		i += 2 + end + 2
	}
}

// blockCommentBefore returns the index in line at which the block comments that document a rule
// start, when line holds only block comments and blanks: all of them, or those after the last one
// that holds a file-wide directive, which stays. It returns -1 if there are none.
func blockCommentBefore(line string) int {
	starts := blockComments(line)
	first := -1
	for k, at := range starts {
		end := len(line)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		if strings.Contains(line[at:end], "lint:file-ignore") {
			first = -1
		} else if first < 0 {
			first = at
		}
	}
	return first
}

// commentLinesAbove returns the start of the comment that fills the lines of text up to end
// (exclusive of the line break), where the last line starts at lastStart: a line comment, a block
// comment on one line, or a block comment over several lines. It returns -1 if those lines are
// not such a comment or hold a file-wide directive.
func commentLinesAbove(text string, lastStart, end int) int {
	line := strings.TrimSpace(text[lastStart:end])
	switch {
	case strings.HasPrefix(line, "//"):
		if strings.Contains(line, "lint:file-ignore") {
			return -1
		}
		return lastStart
	case blockComments(line) != nil:
		// Block comments only, on one line. One that holds a file-wide directive stays, with
		// those before it.
		if strings.Contains(line, "lint:file-ignore") {
			return -1
		}
		return lastStart
	case strings.HasSuffix(line, "*/") && strings.Count(line, "*/") == 1:
		first := lastStart
		for {
			t := strings.TrimSpace(text[first:end])
			if strings.HasPrefix(t, "/*") {
				if strings.Contains(t, "lint:file-ignore") || strings.Count(t, "*/") != 1 {
					return -1
				}
				return first
			}
			if first == 0 {
				return -1
			}
			first = strings.LastIndexByte(text[:first-1], '\n') + 1
			if strings.Contains(text[first:lastStart], "*/") {
				return -1
			}
		}
	}
	return -1
}

// inlineActionsAgree reports whether each inline action keeps the type and the shapes of its value
// in the edited document. An inline action is a rule of its own that the compiler generates, so it
// is not among the declared rules compared above; lowering both grammars gives it a hidden rule of a
// name that does not depend on the edit (the holder and its number), and the lowered grammars are
// compared like the declared ones. The inline actions of removed are gone and are not compared.
func (d *document) inlineActionsAgree(ctx context.Context, candidate *analysis, removed string, control *renderControl) bool {
	before, names, err := engine.LowerInlineActions(d.analysis().g)
	if err != nil {
		return false
	}
	after, afterNames, err := engine.LowerInlineActions(candidate.g)
	if err != nil {
		return false
	}
	if names == nil || afterNames == nil {
		return names == nil && afterNames == nil
	}
	wasProg, err := compileContextSafely(ctx, before)
	if err != nil {
		return false
	}
	nowProg, err := compileContextSafely(ctx, after)
	if err != nil {
		return false
	}
	for _, rd := range before.Rules() {
		control.poll()
		holder, generated := names.Holder(rd.Name)
		if !generated || holder == removed {
			continue
		}
		was, wasOK := wasProg.RuleType(rd.Name)
		now, nowOK := nowProg.RuleType(rd.Name)
		wasShape, wasShapeOK := wasProg.ShapeSignature(rd.Name, control.poll)
		nowShape, nowShapeOK := nowProg.ShapeSignature(rd.Name, control.poll)
		if wasOK != nowOK || was != now || wasShapeOK != nowShapeOK || wasShape != nowShape {
			return false
		}
	}
	return true
}
