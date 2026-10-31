package grammar

import (
	"context"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Format converts g to ZeroParse source code.
//
// Comments and line breaks recorded in the AST (see LineBreak) are kept, so
// formatting parsed source preserves its comments. Struct field types and
// trailing comments on consecutive lines are aligned. A grammar without such
// layout information (for example, one decoded from JSON) is printed with a
// blank line between definitions and one item per line in blocks.
func Format(g *Grammar) string { return formatGrammar(g, nil) }

// FormatType returns t in ZeroParse syntax.
func FormatType(t TypeExpr) string { return formatType(t, nil) }

// FormatContext is Format with cooperative cancellation during AST traversal,
// layout processing and output construction. Cancellation returns an empty
// string and ctx.Err(), never partial formatted source. The caller must not
// mutate g during formatting. Individual allocations, concatenations and
// comparisons may finish before cancellation is observed.
func FormatContext(ctx context.Context, g *Grammar) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ctx.Done() == nil {
		return Format(g), nil
	}
	defer recoverMetadata(&result, &err)
	c := metadataControl{ctx: ctx}
	result = formatGrammar(g, &c)
	c.check()
	return result, nil
}

// FormatExprContext is FormatExpr with the same cancellation contract as
// FormatContext. The caller must not mutate e during formatting.
func FormatExprContext(ctx context.Context, e Expr) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ctx.Done() == nil {
		return FormatExpr(e), nil
	}
	defer recoverMetadata(&result, &err)
	c := metadataControl{ctx: ctx}
	result = formatExpr(e, precChoice, &c)
	c.check()
	return result, nil
}

// FormatTermContext is FormatTerm with the same cancellation contract as
// FormatContext. The caller must not mutate t during formatting.
func FormatTermContext(ctx context.Context, t Term) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ctx.Done() == nil {
		return FormatTerm(t), nil
	}
	defer recoverMetadata(&result, &err)
	c := metadataControl{ctx: ctx}
	result = formatTerm(t, 0, &c)
	c.check()
	return result, nil
}

// FormatTypeContext is FormatType with the same cancellation contract as
// FormatContext. The caller must not mutate t during formatting.
func FormatTypeContext(ctx context.Context, t TypeExpr) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ctx.Done() == nil {
		return FormatType(t), nil
	}
	defer recoverMetadata(&result, &err)
	c := metadataControl{ctx: ctx}
	result = formatType(t, &c)
	c.check()
	return result, nil
}

func expressionLayout(e Expr) bool { return expressionLayoutControlled(e, nil) }

func sortFormatKeys(keys []string, c *metadataControl) {
	if c == nil {
		sort.Strings(keys)
		return
	}
	slices.SortFunc(keys, func(a, b string) int { pollFormat(c); return strings.Compare(a, b) })
}

// Large copies have their own byte checkpoints; small fragments share the AST
// traversal's counter instead of calling Context.Err for every output token.
func writeFormatString(b *strings.Builder, s string, c *metadataControl) {
	if c == nil || len(s) <= 4096 {
		b.WriteString(s)
		return
	}
	for len(s) > 0 {
		c.check()
		n := min(len(s), 4096)
		b.WriteString(s[:n])
		s = s[n:]
	}
}

func joinFormatParts(parts []string, sep string, c *metadataControl) string {
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
	if len(sep) != 0 && len(parts)-1 > maxInt/len(sep) {
		panic("strings: Join output length overflow")
	}
	n := len(sep) * (len(parts) - 1)
	for _, part := range parts {
		pollFormat(c)
		if len(part) > maxInt-n {
			panic("strings: Join output length overflow")
		}
		n += len(part)
	}
	var out strings.Builder
	out.Grow(n)
	for i, part := range parts {
		pollFormat(c)
		if i > 0 {
			writeFormatString(&out, sep, c)
		}
		writeFormatString(&out, part, c)
	}
	return out.String()
}

func formatRuneCount(s string, c *metadataControl) int {
	if c == nil {
		return utf8.RuneCountInString(s)
	}
	total := 0
	for at := 0; at < len(s); {
		limit := 256 - int(c.steps%256)
		n := 0
		for n < limit && at < len(s) {
			_, size := utf8.DecodeRuneInString(s[at:])
			at += size
			n++
		}
		total += n
		c.steps += uint32(n)
		if c.steps%256 == 0 {
			c.check()
		}
	}
	return total
}

func hasFormatNewline(s string, c *metadataControl) bool {
	if c == nil {
		return strings.ContainsAny(s, "\r\n")
	}
	for len(s) > 4096 {
		c.check()
		if strings.ContainsAny(s[:4096], "\r\n") {
			return true
		}
		s = s[4096:]
	}
	return strings.ContainsAny(s, "\r\n")
}

func trimFormatRight(s, cutset string, c *metadataControl) string {
	if c == nil || cutset != " " {
		return strings.TrimRight(s, cutset)
	}
	n := len(s)
	for n > 0 && s[n-1] == ' ' {
		n--
		if (len(s)-n)%4096 == 0 {
			c.check()
		}
	}
	return s[:n]
}

func replaceFormatNewlines(s, old, new string, c *metadataControl) string {
	if c == nil {
		return strings.ReplaceAll(s, old, new)
	}
	if old != "\n" {
		c.check()
		out := strings.ReplaceAll(s, old, new)
		c.check()
		return out
	}
	if old == new {
		return s
	}
	count := 0
	for at := 0; at < len(s); at += 4096 {
		c.check()
		count += strings.Count(s[at:min(at+4096, len(s))], "\n")
	}
	if count == 0 {
		return s
	}
	var out strings.Builder
	out.Grow(len(s) + count*(len(new)-1))
	for at := 0; at < len(s); at += 4096 {
		c.check()
		part := s[at:min(at+4096, len(s))]
		for {
			i := strings.IndexByte(part, '\n')
			if i < 0 {
				writeFormatString(&out, part, c)
				break
			}
			writeFormatString(&out, part[:i], c)
			writeFormatString(&out, new, c)
			part = part[i+1:]
		}
	}
	return out.String()
}

// Keep the nil-control guard small enough to inline at ordinary formatter
// call sites; the shared metadata counter remains out of that fast path.
func pollFormat(c *metadataControl) {
	if c != nil {
		c.poll()
	}
}

// Check between the copies made while recursive wrappers return, as well as
// while descending into them. Evaluating s first leaves ordinary panic values
// untouched and keeps the nil-control guard eligible for inlining.
func finishFormat(s string, c *metadataControl) string {
	if c != nil {
		c.poll()
	}
	return s
}
