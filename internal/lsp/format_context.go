package lsp

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/ornew/zeroparse/grammar"
)

// Formatting has two potentially large phases: AST printing and deriving a
// minimal edit in the client's original newline convention. Neither publishes
// partial output when the request is cancelled.
func formatEditsContext(ctx context.Context, a *analysis) ([]TextEdit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return formatEdits(a), nil
	}
	if len(a.syntaxErrs) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []TextEdit{}, nil
	}
	out, err := grammar.FormatContext(ctx, a.g)
	if err != nil {
		return nil, err
	}
	nl, err := lineEndContext(ctx, a.text)
	if err != nil {
		return nil, err
	}
	if nl != "\n" {
		out, err = formatNewlinesContext(ctx, out, nl)
		if err != nil {
			return nil, err
		}
	}
	edit, err := minimalEditContext(ctx, a.idx, a.text, out)
	if err != nil {
		return nil, err
	}
	if edit.Range.Start == edit.Range.End && edit.NewText == "" {
		return []TextEdit{}, nil
	}
	return []TextEdit{edit}, nil
}

func lineEndContext(ctx context.Context, text string) (string, error) {
	for at := 0; at < len(text); at += 4096 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if i := strings.IndexAny(text[at:min(at+4096, len(text))], "\r\n"); i >= 0 {
			at += i
			if text[at] == '\n' {
				return "\n", nil
			}
			if at+1 < len(text) && text[at+1] == '\n' {
				return "\r\n", nil
			}
			return "\r", nil
		}
	}
	return "\n", nil
}

func formatNewlinesContext(ctx context.Context, text, nl string) (string, error) {
	count := 0
	for at := 0; at < len(text); at += 4096 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count += strings.Count(text[at:min(at+4096, len(text))], "\n")
	}
	if count == 0 {
		return text, nil
	}
	var out strings.Builder
	out.Grow(len(text) + count*(len(nl)-1))
	for at := 0; at < len(text); at += 4096 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// Each replacement is bounded to one chunk, including a CRLF that
		// happens to cross a chunk boundary in the original source.
		out.WriteString(strings.ReplaceAll(text[at:min(at+4096, len(text))], "\n", nl))
	}
	return out.String(), nil
}

func minimalEditContext(ctx context.Context, idx *textIndex, old, new string) (TextEdit, error) {
	p, limit := 0, min(len(old), len(new))
	for p < limit {
		if err := ctx.Err(); err != nil {
			return TextEdit{}, err
		}
		end := min(p+4096, limit)
		if old[p:end] == new[p:end] {
			p = end
			continue
		}
		for p < end && old[p] == new[p] {
			p++
		}
		break
	}
	for p > 0 && (p < len(old) && !utf8.RuneStart(old[p]) || p < len(new) && !utf8.RuneStart(new[p])) {
		p--
		if p%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return TextEdit{}, err
			}
		}
	}
	if p > 0 && old[p-1] == '\r' {
		p--
	}
	n, limit := 0, min(len(old)-p, len(new)-p)
	for n < limit {
		if err := ctx.Err(); err != nil {
			return TextEdit{}, err
		}
		end := min(n+4096, limit)
		if old[len(old)-end:len(old)-n] == new[len(new)-end:len(new)-n] {
			n = end
			continue
		}
		for n < end && old[len(old)-1-n] == new[len(new)-1-n] {
			n++
		}
		break
	}
	for n > 0 && (!utf8.RuneStart(old[len(old)-n]) || !utf8.RuneStart(new[len(new)-n])) {
		n--
		if n%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return TextEdit{}, err
			}
		}
	}
	if n > 0 && old[len(old)-n] == '\n' && len(old)-n > 0 && old[len(old)-n-1] == '\r' {
		n--
	}
	if err := ctx.Err(); err != nil {
		return TextEdit{}, err
	}
	return TextEdit{Range: idx.rangeOf(p, len(old)-n), NewText: new[p : len(new)-n]}, nil
}
