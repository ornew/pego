package syntax

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Fix rewrites source written in earlier PEGO syntax to the current syntax: the cut "--"
// becomes "#cut". It edits only the rewritten tokens, so layout, comments and every other byte
// are kept, and it returns src unchanged when nothing needs rewriting. Source that does not lex
// is an error.
func Fix(src string) (string, error) {
	errs := &errorList{}
	toks := lex(src, errs)
	if len(errs.list) > 0 {
		return "", errs.list
	}
	// The lexer counts columns in code points, with each invalid byte as one; offsets maps a
	// code point index to its byte offset, with a final entry for the end of src.
	var offsets []int
	for i := range src {
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(src))
	lines := lineStarts(src)
	index := func(p grammar.Pos) int { return lines[p.Line-1] + p.Col - 1 }
	var edits []fixEdit
	for _, t := range toks {
		if t.kind != tPunct || t.text != "--" {
			continue
		}
		start, end := offsets[index(t.pos)], offsets[index(t.end)]
		text := "#cut"
		// Keep the cut a separate word, so that it neither takes a following "(" as its
		// arguments nor runs into a neighboring token.
		if r, _ := utf8.DecodeLastRuneInString(src[:start]); start > 0 && !unicode.IsSpace(r) {
			text = " " + text
		}
		if r, _ := utf8.DecodeRuneInString(src[end:]); end < len(src) && !unicode.IsSpace(r) {
			text += " "
		}
		edits = append(edits, fixEdit{start, end, text})
	}
	if len(edits) == 0 {
		return src, nil
	}
	return applyFixEdits(src, edits), nil
}

type fixEdit struct {
	start, end int // byte offsets
	text       string
}

func applyFixEdits(src string, edits []fixEdit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	last := 0
	for _, e := range edits {
		if e.start < last {
			panic(fmt.Sprintf("overlapping fix edits at %d", e.start))
		}
		b.WriteString(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(src[last:])
	return b.String()
}

// lineStarts returns the code point index at which each line of src starts, with the lexer's
// notion of a line break: a line feed, or a carriage return that no line feed follows.
func lineStarts(src string) []int {
	starts := []int{0}
	n := 0
	for i, r := range src {
		n++
		if r == '\n' || r == '\r' && (i+1 >= len(src) || src[i+1] != '\n') {
			starts = append(starts, n)
		}
	}
	return starts
}
