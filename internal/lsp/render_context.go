package lsp

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

// A render control belongs to one immutable-snapshot request. It is never
// stored on the shared analysis or returned with a response.
type renderControl struct {
	ctx   context.Context
	steps uint32
}
type renderCancellation struct{ err error }

func renderRequest(ctx context.Context, action func(*renderControl) (any, *rpcError)) (result any, rerr *rpcError) {
	if ctx == nil {
		return action(nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
	}
	if ctx.Done() == nil {
		return action(nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(renderCancellation); ok {
				result, rerr = nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
			} else {
				panic(failure)
			}
		}
	}()
	c := &renderControl{ctx: ctx}
	result, rerr = action(c)
	c.check()
	return
}

// Keep the ordinary nil-control guard inlineable, including in token loops.
func (c *renderControl) poll() {
	if c != nil {
		c.step()
	}
}
func (c *renderControl) step() {
	c.steps++
	if c.steps%256 == 0 {
		c.check()
	}
}
func (c *renderControl) check() {
	if c != nil {
		if err := c.ctx.Err(); err != nil {
			panic(renderCancellation{err})
		}
	}
}

func (c *renderControl) formatType(t grammar.TypeExpr) string {
	if c == nil {
		return grammar.FormatType(t)
	}
	out, err := grammar.FormatTypeContext(c.ctx, t)
	if err != nil {
		panic(renderCancellation{err})
	}
	return out
}
func (c *renderControl) formatGrammar(g *grammar.Grammar) string {
	if c == nil {
		return grammar.Format(g)
	}
	out, err := grammar.FormatContext(c.ctx, g)
	if err != nil {
		panic(renderCancellation{err})
	}
	return out
}
func (c *renderControl) ruleType(p *engine.Program, name string) (string, bool) {
	if c == nil {
		return p.RuleType(name)
	}
	return p.RuleTypeWithPoll(name, c.poll)
}
func (c *renderControl) isIdentifier(s string) bool {
	if c == nil {
		return syntax.IsIdentifier(s)
	}
	if s == "" {
		return false
	}
	for i := range len(s) {
		c.poll()
		b := s[i]
		letter := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
		if !letter && (i == 0 || b != '_' && (b < '0' || b > '9')) {
			return false
		}
	}
	return true
}
func (c *renderControl) sortKeys(keys []string) {
	if c == nil {
		sort.Strings(keys)
		return
	}
	slices.SortFunc(keys, func(a, b string) int { c.poll(); return strings.Compare(a, b) })
}

// All documentation delimiters are single ASCII bytes. Preserve strings.Split
// exactly, including empty and trailing parts, while bounding each scan.
func (c *renderControl) split(s, sep string) []string {
	if c == nil {
		return strings.Split(s, sep)
	}
	if len(sep) != 1 {
		panic("unsupported documentation separator")
	}
	var parts []string
	start := 0
	for at := 0; at < len(s); {
		c.check()
		end := min(at+4096, len(s))
		for at < end {
			n := strings.IndexByte(s[at:end], sep[0])
			if n < 0 {
				at = end
				break
			}
			at += n
			parts = append(parts, s[start:at])
			at++
			start = at
		}
	}
	return append(parts, s[start:])
}
func (c *renderControl) writeString(b *strings.Builder, s string) {
	if c == nil || len(s) <= 4096 {
		b.WriteString(s)
		return
	}
	for len(s) > 0 {
		c.check()
		n := min(4096, len(s))
		b.WriteString(s[:n])
		s = s[n:]
	}
}
func (c *renderControl) join(parts []string, sep string) string {
	if c == nil {
		return strings.Join(parts, sep)
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	maxInt := int(^uint(0) >> 1)
	if len(sep) > 0 && len(parts)-1 > maxInt/len(sep) {
		panic("strings: Join output length overflow")
	}
	n := len(sep) * (len(parts) - 1)
	for _, part := range parts {
		c.poll()
		if len(part) > maxInt-n {
			panic("strings: Join output length overflow")
		}
		n += len(part)
	}
	var out strings.Builder
	out.Grow(n)
	for i, part := range parts {
		c.poll()
		if i > 0 {
			c.writeString(&out, sep)
		}
		c.writeString(&out, part)
	}
	return out.String()
}
func (c *renderControl) trimSpace(s string) string {
	if c == nil {
		return strings.TrimSpace(s)
	}
	start, end := 0, len(s)
	for start < end {
		c.poll()
		r, n := utf8.DecodeRuneInString(s[start:end])
		if !unicode.IsSpace(r) {
			break
		}
		start += n
	}
	for start < end {
		c.poll()
		r, n := utf8.DecodeLastRuneInString(s[start:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= n
	}
	return s[start:end]
}

// Standard-library regexp calls remain individual uncancellable operations.
// Check immediately around them and within the subsequent explicit walks.
func (c *renderControl) replaceDocumentation(pattern *regexp.Regexp, text, replacement string) string {
	c.check()
	out := pattern.ReplaceAllString(text, replacement)
	c.check()
	return out
}
func (c *renderControl) documentationMatches(pattern *regexp.Regexp, text string) [][]int {
	c.check()
	out := pattern.FindAllStringSubmatchIndex(text, -1)
	c.check()
	return out
}
