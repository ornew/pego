package syntax

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Fix rewrites source written in earlier PEGO syntax to the current syntax:
//
//   - the cut "--" becomes "#cut";
//   - a struct type "type T struct {...}" becomes the definition "type T = {...}";
//   - an alias "type T = U" becomes "type T ~ U";
//   - a positional capture "$n" becomes a named capture: the element it refers to is labeled,
//     reusing its name when it already has one, and "$0" becomes a list of the elements;
//   - a lambda parameter "$x" is written "x";
//   - a capture read as a member "x.y" is read "x.$y". Since field names start with an uppercase
//     letter, a member with another name reads a capture, except the node members startPos,
//     endPos and children, and the members expected and message of Missing.
//
// It edits only the rewritten tokens, so layout, comments and every other byte
// are kept, and it returns src unchanged when nothing needs rewriting. Source that does not lex
// is an error, and so is source with positional captures that does not parse. Lambda
// parameters and members are rewritten only in source that parses.
func Fix(src string) (string, error) {
	errs := &errorList{}
	toks := lex(src, errs)
	if len(errs.list) > 0 {
		return "", errs.list
	}
	out := fixTokens(src, toks)
	positional, captures := false, false
	for i, t := range toks {
		positional = positional || t.kind == tIndex
		captures = captures || t.kind == tCapture
		// x.y may read a capture.
		captures = captures || t.kind == tPunct && t.text == "." && i+1 < len(toks) && toks[i+1].kind == tIdent
	}
	if positional || captures {
		return fixParsed(out, positional)
	}
	return out, nil
}

// fixOffsets maps lexer positions in src to byte offsets.
type fixOffsets struct {
	// The lexer counts columns in code points, with each invalid byte as one; offsets maps a
	// code point index to its byte offset, with a final entry for the end of src.
	offsets []int
	lines   []int
}

func newFixOffsets(src string) *fixOffsets {
	var offsets []int
	for i := range src {
		offsets = append(offsets, i)
	}
	return &fixOffsets{append(offsets, len(src)), lineStarts(src)}
}

func (o *fixOffsets) at(p grammar.Pos) int { return o.offsets[o.lines[p.Line-1]+p.Col-1] }

// fixTokens applies the rewrites that need only the tokens.
func fixTokens(src string, toks []token) string {
	o := newFixOffsets(src)
	var edits []fixEdit
	// replace rewrites token t to text, keeping it a separate word so that it neither runs into
	// a neighboring token nor, for the cut, takes a following "(" as its arguments.
	replace := func(t token, text string) {
		start, end := o.at(t.pos), o.at(t.end)
		if r, _ := utf8.DecodeLastRuneInString(src[:start]); start > 0 && !unicode.IsSpace(r) {
			text = " " + text
		}
		if r, _ := utf8.DecodeRuneInString(src[end:]); end < len(src) && !unicode.IsSpace(r) {
			text += " "
		}
		edits = append(edits, fixEdit{start, end, text})
	}
	is := func(i int, kind tokenKind, text string) bool {
		return i < len(toks) && toks[i].kind == kind && toks[i].text == text
	}
	// depth counts open brackets, so that only a "type" at the top level, which starts a
	// declaration, is rewritten: struct fields may be named type or struct.
	depth := 0
	for i, t := range toks {
		if t.kind == tPunct {
			switch t.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth = max(depth-1, 0)
			}
		}
		switch {
		case is(i, tPunct, "--"):
			replace(t, "#cut")
		case depth == 0 && is(i, tIdent, "type") && i+2 < len(toks) && toks[i+1].kind == tIdent:
			switch {
			case is(i+2, tIdent, "struct"):
				replace(toks[i+2], "=")
			case is(i+2, tPunct, "=") && !is(i+3, tPunct, "{"):
				// "type T = {" is already a definition.
				replace(toks[i+2], "~")
			}
		}
	}
	if len(edits) == 0 {
		return src
	}
	return applyFixEdits(src, edits)
}

type fixEdit struct {
	start, end int // byte offsets
	text       string
}

func applyFixEdits(src string, edits []fixEdit) string {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
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

// AnyTypes returns the byte offsets in src of the references to the type any, which is
// deprecated, in the order of src. It returns nil for source that does not parse.
func AnyTypes(src string) []int {
	g, err := Parse(src)
	if err != nil {
		return nil
	}
	offsets := newFixOffsets(src)
	var out []int
	for _, r := range grammar.TypeRefs(g) {
		if r.Name == "any" && r.Pos.IsValid() {
			out = append(out, offsets.at(r.Pos))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
