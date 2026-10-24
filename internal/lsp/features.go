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
	if s.requestContext == nil {
		return formatEdits(d.analysis()), nil
	}
	edits, formatErr := formatEditsContext(s.requestContext, d.analysis())
	if formatErr != nil {
		return nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
	}
	return edits, nil
}

// formatEdits returns the edits that format the document like pego fmt: none if it does not
// parse, because the formatter would drop the definitions with errors. The document keeps its line
// ends: the formatted lines end like its first line ("\n", "\r\n" or "\r").
func formatEdits(a *analysis) []TextEdit {
	if len(a.syntaxErrs) > 0 {
		return []TextEdit{}
	}
	out := grammar.Format(a.g)
	if nl := lineEnd(a.text); nl != "\n" {
		out = strings.ReplaceAll(out, "\n", nl)
	}
	if out == a.text {
		return []TextEdit{}
	}
	return []TextEdit{minimalEdit(a.idx, a.text, out)}
}

// lineEnd returns the line end of the first line of text: "\n", "\r\n" or "\r" ("\n" if there is
// only one line).
func lineEnd(text string) string {
	i := strings.IndexAny(text, "\r\n")
	switch {
	case i < 0 || text[i] == '\n':
		return "\n"
	case i+1 < len(text) && text[i+1] == '\n':
		return "\r\n"
	}
	return "\r"
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
func (a *analysis) occurrencesOfControlled(o occurrence, control *renderControl) []occurrence {
	control.poll()
	var out []occurrence
	for _, x := range a.occs {
		control.poll()
		if x.kind == o.kind && x.name == o.name {
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) referencesControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
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
	for _, x := range a.occurrencesOfControlled(o, control) {
		control.poll()
		if x.def == nil || p.Context.IncludeDeclaration {
			locs = append(locs, Location{URI: d.uri, Range: a.idx.rangeOf(x.start, x.end)})
		}
	}
	return locs, nil
}

func (s *Server) documentHighlightControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
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
	for _, x := range a.occurrencesOfControlled(o, control) {
		control.poll()
		kind := highlightRead
		if x.def != nil {
			kind = highlightWrite
		}
		hs = append(hs, documentHighlight{Range: a.idx.rangeOf(x.start, x.end), Kind: kind})
	}
	return hs, nil
}

// --- Document symbols ---

func (s *Server) documentSymbolControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
	var p documentFormattingParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	syms := d.analysis().symbolsControlled(control)
	if s.client.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport {
		return syms, nil
	}
	// A flat list for clients without hierarchical symbols, with the fields after their struct.
	flat := []symbolInformation{}
	var add func(syms []DocumentSymbol, container string)
	add = func(syms []DocumentSymbol, container string) {
		for _, sym := range syms {
			control.poll()
			flat = append(flat, symbolInformation{Name: sym.Name, Kind: sym.Kind,
				Location: Location{URI: d.uri, Range: sym.Range}, ContainerName: container})
			add(sym.Children, sym.Name)
		}
	}
	add(syms, "")
	return flat, nil
}

func (a *analysis) symbolsControlled(control *renderControl) []DocumentSymbol {
	control.poll()
	syms := []DocumentSymbol{}
	for _, d := range a.defs {
		control.poll()
		sym := DocumentSymbol{
			Name:           d.name,
			Range:          a.idx.rangeOf(d.start, d.end),
			SelectionRange: a.idx.rangeOf(d.nameStart, d.nameEnd),
		}
		if d.rule != nil {
			sym.Kind = symbolKindFunction
			if t, _, ok := a.ruleTypeControlled(d, control); ok {
				sym.Detail = t
			}
		} else {
			sym.Kind, sym.Detail = typeSymbolKindControlled(d.typ, control)
			if st, ok := d.typ.(*grammar.TypeDef); ok {
				sym.Children = a.fieldSymbolsControlled(d, st, control)
			}
		}
		syms = append(syms, sym)
	}
	return syms
}

func typeSymbolKindControlled(td grammar.TypeDecl, control *renderControl) (int, string) {
	control.poll()
	switch spec := td.(type) {
	case *grammar.TypeDef:
		return symbolKindStruct, "struct"
	case *grammar.TypeAlias:
		return symbolKindInterface, "~ " + control.formatType(spec.Type)
	}
	return symbolKindClass, ""
}

// fieldSymbols returns the symbols of the fields of the struct type d: each spans from its name to
// the last token before the next field (or the closing brace).
func (a *analysis) fieldSymbolsControlled(d *definition, st *grammar.TypeDef, control *renderControl) []DocumentSymbol {
	control.poll()
	var out []DocumentSymbol
	for k, f := range st.Fields() {
		control.poll()
		i, ok := a.tokenIndex(f.Diagnostic())
		if !ok {
			continue
		}
		limit := d.end // the closing brace ends the definition
		if k+1 < len(st.Fields()) {
			limit = a.idx.pegoOffset(st.Fields()[k+1].Diagnostic())
		}
		last := i
		for j := i + 1; j < len(a.toks) && a.toks[j].end < limit; j++ {
			control.poll()
			if a.toks[j].Kind != syntax.TokenComment {
				last = j
			}
		}
		out = append(out, DocumentSymbol{
			Name:           f.Name,
			Detail:         control.formatType(f.Type),
			Kind:           symbolKindField,
			Range:          a.idx.rangeOf(a.toks[i].start, a.toks[last].end),
			SelectionRange: a.idx.rangeOf(a.toks[i].start, a.toks[i].end),
		})
	}
	return out
}

// --- Hover ---

func (s *Server) hoverControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	a := d.analysis()
	text, start, end, ok := a.hoverAtControlled(off, control)
	if !ok {
		return nil, nil
	}
	r := a.idx.rangeOf(start, end)
	return hover{Contents: documentationControlled(text, s.client.TextDocument.Hover.ContentFormat, control), Range: &r}, nil
}

// hoverAt returns the hover text for the byte offset off and the span it is about.
func (a *analysis) hoverAtControlled(off int, control *renderControl) (text string, start, end int, ok bool) {
	control.poll()
	if o, found := a.occurrenceAt(off); found {
		def := o.def
		if def == nil {
			def = a.lookup(o.kind, o.name)
		}
		switch {
		case def != nil && def.rule != nil:
			return a.ruleHoverControlled(def, control), o.start, o.end, true
		case def != nil:
			return typeHoverControlled(def, control), o.start, o.end, true
		case o.kind == kindType:
			if b := findBuiltin(builtinTypes, o.name); b != nil {
				return code("type "+b.name) + "\n" + b.doc, o.start, o.end, true
			}
			return code("type "+o.name) + "\nUndefined type.", o.start, o.end, true
		default:
			return code("rule "+o.name) + "\nUndefined rule.", o.start, o.end, true
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
	case i > 0 && a.toks[i-1].Text == "@" && a.toks[i-1].end == t.start:
		b = findBuiltin(attributes, t.Text)
	case i > 0 && a.toks[i-1].Text == "#" && a.toks[i-1].end == t.start:
		b = findBuiltin(pragmas, t.Text)
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

func (a *analysis) ruleHoverControlled(d *definition, control *renderControl) string {
	control.poll()
	head := "rule " + d.name
	var note string
	switch t, inferred, known := a.ruleTypeControlled(d, control); {
	case !known && d.rule.Type == nil && strings.HasPrefix(d.name, "_") && a.prog != nil:
		// A hidden rule without an action has no type of its own.
		note = "Hidden rule: it makes no node of its own."
	case !known:
		note = "The type is inferred when the grammar has no errors."
	case inferred:
		head += " " + t
		note = "Inferred type."
	default:
		head += " " + t
	}
	if pr, _ := grammar.RuleOperators(d.rule.Expr); pr != nil {
		head += " = #operators { ... }"
	}
	parts := []string{code(head)}
	if note != "" {
		parts = append(parts, "*"+note+"*")
	}
	if d.doc != "" {
		parts = append(parts, d.doc)
	}
	return control.join(parts, "\n\n")
}

func typeHoverControlled(d *definition, control *renderControl) string {
	control.poll()
	// The declaration without the comments before it.
	var td grammar.Decl
	switch t := d.typ.(type) {
	case *grammar.TypeDef:
		v := *t
		v.Break = nil
		v.LeadingComments, v.TrailingComments, v.LeadingDetachedComments, v.HeaderComments = "", nil, nil, nil
		td = &v
	case *grammar.TypeAlias:
		v := *t
		v.Break = nil
		v.LeadingComments, v.TrailingComments, v.LeadingDetachedComments, v.HeaderComments = "", nil, nil, nil
		td = &v
	}
	src := control.trimSpace(control.formatGrammar(&grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{td}}}))
	parts := []string{code(src)}
	if d.doc != "" {
		parts = append(parts, d.doc)
	}
	return control.join(parts, "\n\n")
}

// --- Rename ---

func (s *Server) prepareRenameControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
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

func (s *Server) renameControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
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
	if err := a.checkNewNameControlled(o.kind, o.name, p.NewName, control); err != nil {
		return nil, err
	}
	edits := []TextEdit{}
	for _, x := range a.occurrencesOfControlled(*o, control) {
		control.poll()
		edits = append(edits, TextEdit{Range: a.idx.rangeOf(x.start, x.end), NewText: p.NewName})
	}
	return workspaceEdit{Changes: map[string][]TextEdit{d.uri: edits}}, nil
}

// checkNewName checks that the rule or type old can be renamed to name.
func (a *analysis) checkNewNameControlled(kind symKind, old, name string, control *renderControl) *rpcError {
	control.poll()
	fail := func(format string, args ...any) *rpcError {
		return &rpcError{Code: codeRequestFailed, Message: fmt.Sprintf(format, args...)}
	}
	if name == old {
		return nil
	}
	if kind == kindRule && !control.isRuleName(name) || kind != kindRule && !control.isIdentifier(name) || name == "_" {
		return fail("%q is not a valid %s name", name, kind)
	}
	if kind == kindRule {
		// A leading underscore hides a rule: it makes no node of its own and is no entry point.
		// A rename must not change that.
		if hidden := strings.HasPrefix(old, "_"); hidden != strings.HasPrefix(name, "_") {
			if hidden {
				return fail("rule %s is hidden; the name %s would make it a visible rule", old, name)
			}
			return fail("rule %s is visible; the name %s would hide it", old, name)
		}
		if a.rules[name] != nil {
			return fail("rule %s is already defined", name)
		}
		return a.checkUnusedControlled(kind, name, control)
	}
	if r, _ := utf8.DecodeRuneInString(name); !unicode.IsUpper(r) {
		return fail("type %s must start with an uppercase letter", name)
	}
	if findBuiltin(builtinTypes, name) != nil || formerTypeNames[name] {
		return fail("type %s is reserved", name)
	}
	if a.types[name] != nil {
		return fail("type %s is already defined", name)
	}
	return a.checkUnusedControlled(kind, name, control)
}

// checkUnused checks that name is not used in the namespace kind: renaming to the name of an
// undefined rule or type would make its references refer to the renamed definition.
func (a *analysis) checkUnusedControlled(kind symKind, name string, control *renderControl) *rpcError {
	control.poll()
	for _, o := range a.occs {
		control.poll()
		if o.kind == kind && o.name == name {
			return &rpcError{Code: codeRequestFailed, Message: fmt.Sprintf("%s %s is used (but not defined) in the file", kind, name)}
		}
	}
	return nil
}
