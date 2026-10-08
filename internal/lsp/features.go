package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// --- Formatting ---

func (s *Server) formatting(params json.RawMessage) (any, *rpcError) {
	var p documentFormattingParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return formatEdits(d.analysis()), nil
}

// formatEdits returns the edits that format the document like pego fmt: none if it does not
// parse, because the formatter would drop the definitions with errors. The document keeps its line
// terminators: if its first line ends with "\r\n", so do the formatted lines.
func formatEdits(a *analysis) []TextEdit {
	if len(a.syntaxErrs) > 0 {
		return []TextEdit{}
	}
	out := grammar.Format(a.g)
	if i := strings.IndexByte(a.text, '\n'); i > 0 && a.text[i-1] == '\r' {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if out == a.text {
		return []TextEdit{}
	}
	return []TextEdit{minimalEdit(a.idx, a.text, out)}
}

// minimalEdit returns a single edit that turns old (indexed by idx) into new, replacing only the
// part between their common prefix and suffix, so that the editor keeps the cursor and the
// unchanged lines as they are. The edit starts and ends on character boundaries, and not inside a
// "\r\n".
func minimalEdit(idx *textIndex, old, new string) TextEdit {
	p := 0
	for p < len(old) && p < len(new) && old[p] == new[p] {
		p++
	}
	for p > 0 && (p < len(old) && !utf8.RuneStart(old[p]) || p < len(new) && !utf8.RuneStart(new[p])) {
		p--
	}
	if p > 0 && old[p-1] == '\r' {
		p--
	}
	n := 0
	for n < len(old)-p && n < len(new)-p && old[len(old)-1-n] == new[len(new)-1-n] {
		n++
	}
	for n > 0 && (!utf8.RuneStart(old[len(old)-n]) || !utf8.RuneStart(new[len(new)-n])) {
		n--
	}
	if n > 0 && old[len(old)-n] == '\n' && len(old)-n > 0 && old[len(old)-n-1] == '\r' {
		n--
	}
	return TextEdit{Range: idx.rangeOf(p, len(old)-n), NewText: new[p : len(new)-n]}
}

// --- Navigation ---

func (s *Server) definition(params json.RawMessage) (any, *rpcError) {
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	o, ok := a.occurrenceAt(off)
	if !ok {
		return nil, nil
	}
	def := a.lookup(o.kind, o.name)
	if o.def != nil {
		def = o.def
	}
	if def == nil {
		return nil, nil
	}
	return []Location{{URI: d.uri, Range: a.idx.rangeOf(def.nameStart, def.nameEnd)}}, nil
}

// occurrencesOf returns the occurrences of the name of o, in source order.
func (a *analysis) occurrencesOf(o occurrence) []occurrence {
	var out []occurrence
	for _, x := range a.occs {
		if x.kind == o.kind && x.name == o.name {
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) references(params json.RawMessage) (any, *rpcError) {
	var p referenceParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	o, ok := a.occurrenceAt(a.idx.offset(p.Position))
	if !ok {
		return nil, nil
	}
	locs := []Location{}
	for _, x := range a.occurrencesOf(o) {
		if x.def == nil || p.Context.IncludeDeclaration {
			locs = append(locs, Location{URI: d.uri, Range: a.idx.rangeOf(x.start, x.end)})
		}
	}
	return locs, nil
}

func (s *Server) documentHighlight(params json.RawMessage) (any, *rpcError) {
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	o, ok := a.occurrenceAt(off)
	if !ok {
		return nil, nil
	}
	hs := []documentHighlight{}
	for _, x := range a.occurrencesOf(o) {
		kind := highlightRead
		if x.def != nil {
			kind = highlightWrite
		}
		hs = append(hs, documentHighlight{Range: a.idx.rangeOf(x.start, x.end), Kind: kind})
	}
	return hs, nil
}

// --- Document symbols ---

func (s *Server) documentSymbol(params json.RawMessage) (any, *rpcError) {
	var p documentFormattingParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return d.analysis().symbols(), nil
}

func (a *analysis) symbols() []DocumentSymbol {
	syms := []DocumentSymbol{}
	for _, d := range a.defs {
		sym := DocumentSymbol{
			Name:           d.name,
			Range:          a.idx.rangeOf(d.start, d.end),
			SelectionRange: a.idx.rangeOf(d.nameStart, d.nameEnd),
		}
		if d.rule != nil {
			sym.Kind = symbolKindFunction
			if t, _, ok := a.ruleType(d); ok {
				sym.Detail = t
			}
		} else {
			sym.Kind, sym.Detail = typeSymbolKind(d.typ)
			if st, ok := d.typ.Spec.(*grammar.StructSpec); ok {
				sym.Children = a.fieldSymbols(d, st)
			}
		}
		syms = append(syms, sym)
	}
	return syms
}

func typeSymbolKind(td *grammar.TypeDef) (int, string) {
	switch spec := td.Spec.(type) {
	case *grammar.StructSpec:
		return symbolKindStruct, "struct"
	case *grammar.TerminalSpec:
		return symbolKindString, "terminal"
	case *grammar.AliasSpec:
		return symbolKindInterface, "= " + grammar.FormatType(spec.Type)
	}
	return symbolKindClass, ""
}

// fieldSymbols returns the symbols of the fields of the struct type d: each spans from its name to
// the last token before the next field (or the closing brace).
func (a *analysis) fieldSymbols(d *definition, st *grammar.StructSpec) []DocumentSymbol {
	var out []DocumentSymbol
	for k, f := range st.Fields {
		i, ok := a.tokenIndex(f.Pos)
		if !ok {
			continue
		}
		limit := d.end // the closing brace ends the definition
		if k+1 < len(st.Fields) {
			limit = a.idx.pegoOffset(st.Fields[k+1].Pos)
		}
		last := i
		for j := i + 1; j < len(a.toks) && a.toks[j].end < limit; j++ {
			if a.toks[j].Kind != syntax.TokenComment {
				last = j
			}
		}
		out = append(out, DocumentSymbol{
			Name:           f.Name,
			Detail:         grammar.FormatType(f.Type),
			Kind:           symbolKindField,
			Range:          a.idx.rangeOf(a.toks[i].start, a.toks[last].end),
			SelectionRange: a.idx.rangeOf(a.toks[i].start, a.toks[i].end),
		})
	}
	return out
}

// --- Hover ---

func (s *Server) hover(params json.RawMessage) (any, *rpcError) {
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	text, start, end, ok := a.hoverAt(off)
	if !ok {
		return nil, nil
	}
	r := a.idx.rangeOf(start, end)
	return hover{Contents: markupContent{Kind: "markdown", Value: text}, Range: &r}, nil
}

// hoverAt returns the hover text for the byte offset off and the span it is about.
func (a *analysis) hoverAt(off int) (text string, start, end int, ok bool) {
	if o, found := a.occurrenceAt(off); found {
		def := o.def
		if def == nil {
			def = a.lookup(o.kind, o.name)
		}
		switch {
		case def != nil && def.rule != nil:
			return a.ruleHover(def), o.start, o.end, true
		case def != nil:
			return typeHover(def), o.start, o.end, true
		case o.kind == kindType:
			if b := findBuiltin(builtinTypes, o.name); b != nil {
				return code("type "+b.name) + "\n" + b.doc, o.start, o.end, true
			}
			return code("type "+o.name) + "\nUndefined type.", o.start, o.end, true
		default:
			return code("def "+o.name) + "\nUndefined rule.", o.start, o.end, true
		}
	}
	i := a.tokenAt(off)
	if i < 0 || a.toks[i].Kind != syntax.TokenIdent {
		return "", 0, 0, false
	}
	t := a.toks[i]
	class, marked := a.sem[t.start]
	var b *builtin
	switch {
	case i > 0 && a.toks[i-1].Text == "#" && a.toks[i-1].end == t.start:
		b = findBuiltin(attributes, t.Text)
	case marked && class.typ == semFunction:
		b = findBuiltin(builtinFuncs, t.Text)
	case marked && class.typ == semProperty && class.mods&modDefaultLibrary != 0:
		b = findBuiltin(builtinMembers, t.Text)
	}
	if b == nil {
		return "", 0, 0, false
	}
	return code(b.signature) + "\n" + b.doc, t.start, t.end, true
}

func code(s string) string { return "```pego\n" + s + "\n```" }

func (a *analysis) ruleHover(d *definition) string {
	head := "def " + d.name
	var note string
	switch t, inferred, known := a.ruleType(d); {
	case !known:
		note = "The type is inferred when the grammar has no errors."
	case inferred:
		head += ": " + t
		note = "Inferred type."
	default:
		head += ": " + t
	}
	parts := []string{code(head)}
	if note != "" {
		parts = append(parts, "*"+note+"*")
	}
	if d.doc != "" {
		parts = append(parts, d.doc)
	}
	return strings.Join(parts, "\n\n")
}

func typeHover(d *definition) string {
	td := *d.typ
	td.Break = nil // without the comments before it
	src := strings.TrimSpace(grammar.Format(&grammar.Grammar{Statements: []grammar.Statement{&td}}))
	parts := []string{code(src)}
	if d.doc != "" {
		parts = append(parts, d.doc)
	}
	return strings.Join(parts, "\n\n")
}

// --- Rename ---

func (s *Server) prepareRename(params json.RawMessage) (any, *rpcError) {
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	o, rerr := a.renameTarget(off)
	if rerr != nil || o == nil {
		return nil, rerr
	}
	return prepareRenameResult{Range: a.idx.rangeOf(o.start, o.end), Placeholder: o.name}, nil
}

// renameTarget returns the occurrence to rename at off, nil if there is no name there, or an
// error that explains why the name cannot be renamed.
func (a *analysis) renameTarget(off int) (*occurrence, *rpcError) {
	o, ok := a.occurrenceAt(off)
	if !ok {
		return nil, nil
	}
	if len(a.syntaxErrs) > 0 {
		return nil, &rpcError{Code: codeRequestFailed, Message: "cannot rename while the file has syntax errors"}
	}
	if a.lookup(o.kind, o.name) == nil {
		if o.kind == kindType && findBuiltin(builtinTypes, o.name) != nil {
			return nil, &rpcError{Code: codeRequestFailed, Message: fmt.Sprintf("cannot rename the built-in type %s", o.name)}
		}
		return nil, &rpcError{Code: codeRequestFailed, Message: fmt.Sprintf("%s %s is not defined", o.kind, o.name)}
	}
	return &o, nil
}

func (s *Server) rename(params json.RawMessage) (any, *rpcError) {
	var p renameParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	o, err := a.renameTarget(a.idx.offset(p.Position))
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, nil
	}
	if err := a.checkNewName(o.kind, o.name, p.NewName); err != nil {
		return nil, err
	}
	edits := []TextEdit{}
	for _, x := range a.occurrencesOf(*o) {
		edits = append(edits, TextEdit{Range: a.idx.rangeOf(x.start, x.end), NewText: p.NewName})
	}
	return workspaceEdit{Changes: map[string][]TextEdit{d.uri: edits}}, nil
}

// checkNewName checks that the rule or type old can be renamed to name.
func (a *analysis) checkNewName(kind symKind, old, name string) *rpcError {
	fail := func(format string, args ...any) *rpcError {
		return &rpcError{Code: codeRequestFailed, Message: fmt.Sprintf(format, args...)}
	}
	if name == old {
		return nil
	}
	if !syntax.IsIdentifier(name) || name == "_" {
		return fail("%q is not a valid %s name", name, kind)
	}
	if kind == kindRule {
		if syntax.IsKeyword(name) {
			return fail("%s is a keyword", name)
		}
		if a.rules[name] != nil {
			return fail("rule %s is already defined", name)
		}
		return nil
	}
	if r, _ := utf8.DecodeRuneInString(name); !unicode.IsUpper(r) {
		return fail("type %s must start with an uppercase letter", name)
	}
	if findBuiltin(builtinTypes, name) != nil {
		return fail("type %s is reserved", name)
	}
	if a.types[name] != nil {
		return fail("type %s is already defined", name)
	}
	return nil
}
