package lsp

import (
	"encoding/json"
	"sort"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// Completion works on tokens rather than on the grammar: the definition being written usually
// does not parse yet, and the parser drops a definition with an error. The context of the cursor
// (a type, a parsing expression, an action, an attribute, ...) is found from the tokens before it,
// and the names offered are those that def and type declare anywhere in the file.

func (s *Server) completion(params json.RawMessage) (any, *rpcError) {
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	items := d.analysis().complete(off)
	if !s.client.TextDocument.Completion.CompletionItem.SnippetSupport {
		// Without snippets, insert the label.
		for i := range items {
			if items[i].InsertTextFormat == insertSnippet {
				items[i].InsertText, items[i].InsertTextFormat = "", 0
			}
		}
	}
	return completionList{Items: items}, nil
}

func (a *analysis) isPunct(i int, text string) bool {
	return i >= 0 && i < len(a.toks) && a.toks[i].Kind == syntax.TokenPunct && a.toks[i].Text == text
}

func (a *analysis) isIdent(i int, text string) bool {
	return i >= 0 && i < len(a.toks) && a.toks[i].Kind == syntax.TokenIdent && a.toks[i].Text == text
}

// isDefKeyword reports whether token i is def or type.
func (a *analysis) isDefKeyword(i int) bool { return a.isIdent(i, "def") || a.isIdent(i, "type") }

// inside reports whether the byte offset off is inside token t (a comment, a string or a character
// class), where nothing is completed.
func (a *analysis) inside(t token, off int) bool {
	switch t.Kind {
	case syntax.TokenComment:
		// A comment extends to the end of the line, past trailing white space.
		for k := t.start; k < off; k++ {
			if a.text[k] == '\n' || a.text[k] == '\r' {
				return false
			}
		}
		return off > t.start
	case syntax.TokenString:
		return off > t.start && (off < t.end || !terminated(a.text[t.start:t.end], '"', 1))
	case syntax.TokenCharClass:
		return off > t.start && (off < t.end || !terminated(a.text[t.start:t.end], ')', 2))
	}
	return false
}

// terminated reports whether the string or character class raw, whose content starts at index
// from, ends with its closing character close.
func terminated(raw string, close byte, from int) bool {
	for i := from; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++
		case close:
			return i == len(raw)-1
		}
	}
	return false
}

// complete returns the completion items at the byte offset off.
func (a *analysis) complete(off int) []CompletionItem {
	items := []CompletionItem{}
	// i is the last token that starts before off.
	i := sort.Search(len(a.toks), func(i int) bool { return a.toks[i].start >= off }) - 1
	if i >= 0 && a.inside(a.toks[i], off) {
		return items
	}
	// prev is the token before the word being typed, if any. capture reports that the word is a
	// capture reference ($name), or that "$" is directly before the cursor or the word.
	prev, capture := i, false
	if i >= 0 && off <= a.toks[i].end {
		switch a.toks[i].Kind {
		case syntax.TokenCapture:
			prev, capture = i-1, true
		case syntax.TokenIdent:
			prev = i - 1
		}
	}
	// The punctuation directly before the word (or the cursor).
	adjacent := ""
	if prev >= 0 && a.toks[prev].Kind == syntax.TokenPunct &&
		(prev == i && a.toks[prev].end == off || prev == i-1 && a.toks[prev].end == a.toks[i].start) {
		adjacent = a.toks[prev].Text
	}
	switch adjacent {
	case "#":
		return attributeItems()
	case "$":
		capture = true
	}
	// "$" and "." are trigger characters. In a value expression they start a capture reference
	// and a field access; elsewhere they are the end-of-line anchor and any character, after which
	// a completion accepted with Enter would only break the expression.
	if (capture || adjacent == ".") && !a.inValue(prev) {
		return items
	}
	if capture {
		return a.captureItems(prev)
	}
	// st is the keyword of the definition the cursor is in.
	st := prev
	for st >= 0 && !a.isDefKeyword(st) {
		st--
	}
	if st < 0 {
		return keywordItems(append([]string{"package"}, statementKeywords...))
	}
	if prev == st {
		return items // the name of a new definition
	}
	pt := a.toks[prev]
	if a.isIdent(st, "type") {
		switch {
		case prev == st+1:
			return keywordItems(typeKeywords)
		case pt.Text == "{" || pt.Text == ",":
			return items // a field name
		}
		return append(a.typeItems(false), keywordItems(statementKeywords)...)
	}
	// A rule definition: its type comes before "=".
	eq := a.ruleEquals(st, prev)
	if eq < 0 {
		switch pt.Text {
		case ":", "|", "[]", "*", "(":
			return a.typeItems(false)
		}
		return items
	}
	value, pratt := a.scanBody(eq, prev)
	if value {
		switch pt.Text {
		case "new":
			return a.typeItems(true)
		case ".":
			return a.memberItems()
		}
		items = append(items, builtinFuncItems()...)
		items = append(items, keywordItems(termKeywords)...)
		for _, v := range sortedKeys(a.vars) {
			items = append(items, CompletionItem{Label: v, Kind: completionKindVariable, Detail: "variable"})
		}
		return items
	}
	if pt.Text == "infix" {
		return keywordItems([]string{"left", "right", "none"})
	}
	items = append(items, a.ruleItems()...)
	if prev == eq {
		items = append(items, keywordItems(bodyKeywords)...)
	}
	if pratt {
		items = append(items, keywordItems(prattKeywords)...)
	}
	return append(items, keywordItems(statementKeywords)...)
}

// ruleEquals returns the index of the "=" of the rule definition whose def is token st, if it
// comes before token prev, or -1.
func (a *analysis) ruleEquals(st, prev int) int {
	for j := st + 1; j <= prev; j++ {
		if a.isPunct(j, "=") {
			return j
		}
	}
	return -1
}

// scanBody scans the tokens of a rule body from its "=" (token eq) to token prev, and reports
// whether the cursor after prev is in a value expression (an action after "->", or a predicate
// in [...]) and whether the body is a pratt expression.
func (a *analysis) scanBody(eq, prev int) (value, pratt bool) {
	term, depth := false, 0
	pratt = a.isIdent(eq+1, "pratt")
	for j := eq + 1; j <= prev; j++ {
		switch t := a.toks[j]; {
		case a.isPunct(j, "->"):
			term = true
		case a.isPunct(j, "["):
			depth++
		case a.isPunct(j, "]"):
			depth = max(0, depth-1)
		case pratt && t.Kind == syntax.TokenIdent:
			switch t.Text {
			case "skip", "operand", "level", "prefix", "postfix", "infix":
				term = false // the next line of the pratt expression
			}
		}
	}
	return term || depth > 0, pratt
}

// inValue reports whether the cursor after token prev is in a value expression of a rule body.
func (a *analysis) inValue(prev int) bool {
	st := prev
	for st >= 0 && !a.isDefKeyword(st) {
		st--
	}
	if st < 0 || !a.isIdent(st, "def") {
		return false
	}
	eq := a.ruleEquals(st, prev)
	if eq < 0 {
		return false
	}
	value, _ := a.scanBody(eq, prev)
	return value
}

// captureItems returns the capture labels and lambda parameters of the definition that token i
// is in, for completion after "$".
func (a *analysis) captureItems(i int) []CompletionItem {
	items := []CompletionItem{}
	first := i
	for first >= 0 && !a.isDefKeyword(first) {
		first--
	}
	if first < 0 || !a.isIdent(first, "def") {
		return items
	}
	seen := map[string]bool{}
	add := func(name, detail string) {
		if !seen[name] {
			seen[name] = true
			items = append(items, CompletionItem{Label: name, Kind: completionKindVariable, Detail: detail})
		}
	}
	for j := first + 2; j < len(a.toks) && !a.isDefKeyword(j); j++ {
		t := a.toks[j]
		switch {
		case t.Kind == syntax.TokenIdent && a.isPunct(j+1, ":") && a.toks[j+1].start == t.end &&
			!a.isPunct(j-1, "{") && !a.isPunct(j-1, ","):
			add(t.Text, "capture") // label:expr (not a field of new T{...})
		case a.isPunct(j, "=>") && a.isPunct(j-1, ")"):
			// The parameters of a lambda: (a, b) =>.
			for k := j - 2; k > first && !a.isPunct(k, "("); k -= 2 {
				if a.toks[k].Kind == syntax.TokenIdent {
					add(a.toks[k].Text, "parameter")
				}
			}
		}
	}
	return items
}

// declared is a name that a def or type statement declares.
type declared struct {
	name string
	// spec is the token after the name of a type: struct, terminal or =.
	spec string
}

// declarations returns the names declared by def (kind rule) or type (kind type) statements, in
// source order and without duplicates, including those of definitions with syntax errors.
func (a *analysis) declarations(kind symKind) []declared {
	kw := "def"
	if kind == kindType {
		kw = "type"
	}
	var out []declared
	seen := map[string]bool{}
	for i := 0; i+1 < len(a.toks); i++ {
		n := a.toks[i+1]
		if !a.isIdent(i, kw) || n.Kind != syntax.TokenIdent || syntax.IsKeyword(n.Text) || seen[n.Text] {
			continue
		}
		d := declared{name: n.Text}
		if a.isIdent(i+2, "struct") || a.isIdent(i+2, "terminal") || a.isPunct(i+2, "=") {
			d.spec = a.toks[i+2].Text
		}
		seen[d.name] = true
		out = append(out, d)
	}
	return out
}

func keywordItems(words []string) []CompletionItem {
	var items []CompletionItem
	for _, w := range words {
		items = append(items, CompletionItem{Label: w, Kind: completionKindKeyword, SortText: "9" + w})
	}
	return items
}

func attributeItems() []CompletionItem {
	var items []CompletionItem
	for _, b := range attributes {
		items = append(items, CompletionItem{Label: b.name, Kind: completionKindProperty, Detail: b.signature,
			Documentation: &markupContent{Kind: "markdown", Value: b.doc},
			InsertText:    b.snippet, InsertTextFormat: insertSnippet})
	}
	return items
}

func builtinFuncItems() []CompletionItem {
	var items []CompletionItem
	for _, b := range builtinFuncs {
		items = append(items, CompletionItem{Label: b.name, Kind: completionKindFunction, Detail: b.signature,
			Documentation: &markupContent{Kind: "markdown", Value: b.doc},
			InsertText:    b.snippet, InsertTextFormat: insertSnippet})
	}
	return items
}

// memberItems returns the fields every node has and the fields of the struct types that parse.
func (a *analysis) memberItems() []CompletionItem {
	var items []CompletionItem
	for _, b := range builtinMembers {
		items = append(items, CompletionItem{Label: b.name, Kind: completionKindField, Detail: b.signature,
			Documentation: &markupContent{Kind: "markdown", Value: b.doc}, SortText: "9" + b.name})
	}
	seen := map[string]bool{}
	for _, d := range a.defs {
		if d.typ == nil {
			continue
		}
		st, ok := d.typ.Spec.(*grammar.StructSpec)
		if !ok {
			continue
		}
		for _, f := range st.Fields {
			if !seen[f.Name] {
				seen[f.Name] = true
				items = append(items, CompletionItem{Label: f.Name, Kind: completionKindField,
					Detail: d.name + "." + f.Name + " " + grammar.FormatType(f.Type)})
			}
		}
	}
	return items
}

func (a *analysis) ruleItems() []CompletionItem {
	var items []CompletionItem
	for _, n := range a.declarations(kindRule) {
		it := CompletionItem{Label: n.name, Kind: completionKindFunction}
		if d := a.rules[n.name]; d != nil {
			if t, _, ok := a.ruleType(d); ok {
				it.Detail = t
			}
			if d.doc != "" {
				it.Documentation = &markupContent{Kind: "markdown", Value: d.doc}
			}
		}
		items = append(items, it)
	}
	return items
}

// typeItems returns the types declared in the file and the built-in types; with structsOnly, only
// the struct types (for new).
func (a *analysis) typeItems(structsOnly bool) []CompletionItem {
	var items []CompletionItem
	for _, n := range a.declarations(kindType) {
		if structsOnly && n.spec != "struct" {
			continue
		}
		it := CompletionItem{Label: n.name, Kind: completionKindClass, Detail: n.spec}
		if n.spec == "struct" {
			it.Kind = completionKindStruct
		}
		if d := a.types[n.name]; d != nil {
			_, it.Detail = typeSymbolKind(d.typ)
			if d.doc != "" {
				it.Documentation = &markupContent{Kind: "markdown", Value: d.doc}
			}
		}
		items = append(items, it)
	}
	if structsOnly {
		return items
	}
	for _, b := range builtinTypes {
		items = append(items, CompletionItem{Label: b.name, Kind: completionKindClass, Detail: "built-in type",
			Documentation: &markupContent{Kind: "markdown", Value: b.doc}, SortText: "9" + b.name})
	}
	return items
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
