package lsp

import (
	"encoding/json"

	"github.com/ornew/pego/internal/syntax"
)

// Semantic tokens tell rule names, type names, captures and fields apart, which a TextMate grammar
// cannot do reliably. Punctuation is left to the TextMate grammar.

type semTokenType int

// Semantic token types; the order is that of semanticTokenTypes.
const (
	semKeyword semTokenType = iota
	semType
	semFunction
	semVariable
	semParameter
	semProperty
	semString
	semNumber
	semRegexp
	semComment
	semDecorator
)

var semanticTokenTypes = []string{
	"keyword", "type", "function", "variable", "parameter", "property", "string", "number", "regexp", "comment",
	"decorator",
}

// Semantic token modifiers, as bits; the order is that of semanticTokenModifiers.
const (
	modDeclaration = 1 << iota
	modDefaultLibrary
)

var semanticTokenModifiers = []string{"declaration", "defaultLibrary"}

type semClass struct {
	typ  semTokenType
	mods int
}

func (s *Server) semanticTokens(params json.RawMessage) (any, *rpcError) {
	var p documentFormattingParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": d.analysis().semanticTokens()}, nil
}

// semanticTokens returns the semantic tokens of the document in the relative encoding of LSP:
// five integers per token (line delta, start delta, length, type, modifiers).
func (a *analysis) semanticTokens() []int {
	data := []int{}
	occ := 0
	prevLine, prevChar := 0, 0
	for i, t := range a.toks {
		c, ok := a.classify(i, &occ)
		if !ok {
			continue
		}
		start, end := a.idx.position(t.start), a.idx.position(t.end)
		if end.Line != start.Line {
			continue // tokens do not span lines, but a client could not show one that did
		}
		if start.Line != prevLine {
			prevChar = 0
		}
		data = append(data, start.Line-prevLine, start.Character-prevChar, end.Character-start.Character, int(c.typ), c.mods)
		prevLine, prevChar = start.Line, start.Character
	}
	return data
}

// classify returns the class of token i. occ is the index of the first occurrence that does not
// end before the token; classify advances it.
func (a *analysis) classify(i int, occ *int) (semClass, bool) {
	t := a.toks[i]
	switch t.Kind {
	case syntax.TokenComment:
		return semClass{semComment, 0}, true
	case syntax.TokenString:
		return semClass{semString, 0}, true
	case syntax.TokenInt:
		return semClass{semNumber, 0}, true
	case syntax.TokenCharClass:
		return semClass{semRegexp, 0}, true
	case syntax.TokenCapture, syntax.TokenIndex:
		return semClass{semParameter, 0}, true
	case syntax.TokenIdent:
	default:
		return semClass{}, false
	}
	for *occ < len(a.occs) && a.occs[*occ].end <= t.start {
		*occ++
	}
	if *occ < len(a.occs) && a.occs[*occ].start == t.start {
		o := a.occs[*occ]
		c := semClass{semFunction, 0}
		if o.kind == kindType {
			c.typ = semType
			if a.types[o.name] == nil && findBuiltin(builtinTypes, o.name) != nil {
				c.mods |= modDefaultLibrary
			}
		}
		if o.def != nil {
			c.mods |= modDeclaration
		}
		return c, true
	}
	if c, ok := a.sem[t.start]; ok {
		return c, true
	}
	switch t.Text {
	case "true", "false", "nil":
		return semClass{semKeyword, 0}, true
	case "left", "right", "none":
		if i > 0 && a.toks[i-1].Text == "infix" {
			return semClass{semKeyword, 0}, true
		}
		return semClass{}, false
	}
	if syntax.IsKeyword(t.Text) {
		return semClass{semKeyword, 0}, true
	}
	return semClass{}, false
}
