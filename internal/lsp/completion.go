package lsp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// Completion works on tokens rather than on the grammar: the definition being written usually
// does not parse yet, and the parser drops a definition with an error. The context of the cursor
// (a type, a parsing expression, an action, an attribute, ...) is found from the tokens before it,
// and the names offered are those that rule and type declare anywhere in the file.

func (s *Server) completion(params json.RawMessage) (any, *rpcError) {
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	items := d.analysis().complete(off)
	for i := range items {
		if items[i].Documentation != nil {
			doc := documentation(items[i].Documentation.Value, s.client.TextDocument.Completion.CompletionItem.DocumentationFormat)
			items[i].Documentation = &doc
		}
	}
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

// isDefKeyword reports whether token i is rule or type.
func (a *analysis) isDefKeyword(i int) bool {
	return a.declarationStart(i) && (a.isIdent(i, "rule") || a.isIdent(i, "type"))
}

func (a *analysis) declarationStart(i int) bool {
	return i >= 0 && i < len(a.declarationStarts) && a.declarationStarts[i]
}

// indexDeclarationStarts scans once; completion and semantic lookup must not
// rescan the entire token prefix for every potential contextual keyword.
func (a *analysis) indexDeclarationStarts() {
	a.declarationStarts = make([]bool, len(a.toks))
	depth, previous := 0, -1
	for i, t := range a.toks {
		if t.Kind == syntax.TokenComment {
			continue
		}
		boundary := previous < 0 || a.isPunct(previous, ";") || a.toks[previous].End.Line < t.Pos.Line
		if boundary && len(a.syntaxErrs) > 0 && (a.isIdent(i, "rule") || a.isIdent(i, "type")) {
			next := a.significantToken(i + 1)
			if next < len(a.toks) && a.toks[next].Kind == syntax.TokenIdent {
				depth = 0
			}
		}
		a.declarationStarts[i] = boundary && depth == 0
		if t.Kind == syntax.TokenPunct {
			switch t.Text {
			case "(", "{":
				depth++
			case ")", "}":
				depth = max(0, depth-1)
			}
		}
		previous = i
	}
}

// inside reports whether the byte offset off is inside token t (a comment, a string or a character
// class), where nothing is completed.
func (a *analysis) inside(t token, off int) bool {
	switch t.Kind {
	case syntax.TokenComment:
		if strings.HasPrefix(t.Text, "/*") {
			return off > t.start && (off < t.end || !strings.HasSuffix(t.Text, "*/"))
		}
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
		return off > t.start && (off < t.end || !terminated(a.text[t.start:t.end], ']', 1))
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
	if items, found := a.contextualItems(prev); found {
		return items
	}
	// The punctuation directly before the word (or the cursor).
	adjacent := ""
	if prev >= 0 && a.toks[prev].Kind == syntax.TokenPunct &&
		(prev == i && a.toks[prev].end == off || prev == i-1 && a.toks[prev].end == a.toks[i].start) {
		adjacent = a.toks[prev].Text
	}
	switch adjacent {
	case "@":
		return directiveItems(attributes)
	case "#":
		return directiveItems(pragmas)
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
	if value, args := a.attributeArguments(prev); args {
		if !value {
			return a.ruleItems()
		}
		if adjacent == "." {
			return a.memberItems()
		}
		return a.valueItems()
	}
	// st is the keyword of the definition the cursor is in.
	prev = a.previousSignificantToken(prev)
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
		case pt.Text == "{" || pt.Text == ";":
			return items // a field name
		}
		return append(a.typeItems(false), keywordItems(statementKeywords)...)
	}
	// A rule definition's optional type comes before its equals sign.
	eq := a.ruleBodyStart(st, prev)
	if eq < 0 {
		if prev == st+1 {
			return a.typeItems(false)
		}
		switch pt.Text {
		case ":", "|", "[]", "*", "(":
			return a.typeItems(false)
		}
		return items
	}
	value, operatorsRule := a.scanBody(eq, prev)
	if value {
		switch pt.Text {
		case ".":
			return a.memberItems()
		}
		items = append(items, builtinFuncItems()...)
		items = append(items, a.typeItems(true)...)
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
	if operatorsRule {
		items = append(items, keywordItems(operatorsRuleKeywords)...)
	}
	return append(items, keywordItems(statementKeywords)...)
}

// attributeArguments finds the innermost open attribute argument list, including
// nested groups. Recovery skip arguments contain PEG expressions; other arguments
// contain value expressions. Comments do not change either context.
func (a *analysis) attributeArguments(prev int) (value, found bool) {
	type context struct {
		attribute, argument string
		pragma              bool
	}
	var stack []context
	for j := 0; j <= prev; j++ {
		if a.isDefKeyword(j) {
			stack = nil
		}
		if a.isPunct(j, "(") {
			c := context{}
			name := a.previousSignificantToken(j - 1)
			if name >= 0 {
				at := a.previousSignificantToken(name - 1)
				if at >= 0 && (a.isPunct(at, "@") || a.isPunct(at, "#")) && a.toks[name].Kind == syntax.TokenIdent &&
					a.toks[at].end == a.toks[name].start && a.toks[name].end == a.toks[j].start {
					c.attribute = a.toks[name].Text
					c.pragma = a.isPunct(at, "#")
				}
			}
			stack = append(stack, c)
		} else if a.isPunct(j, ")") && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		} else if a.isPunct(j, "=") && len(stack) > 0 && stack[len(stack)-1].attribute != "" {
			name := a.previousSignificantToken(j - 1)
			if name >= 0 {
				stack[len(stack)-1].argument = a.toks[name].Text
			}
		}
	}
	for j := len(stack) - 1; j >= 0; j-- {
		if stack[j].attribute != "" {
			return !stack[j].pragma || stack[j].attribute != "recover" || stack[j].argument != "skip", true
		}
	}
	return false, false
}

func (a *analysis) valueItems() []CompletionItem {
	items := append(builtinFuncItems(), a.typeItems(true)...)
	items = append(items, keywordItems(termKeywords)...)
	for _, v := range sortedKeys(a.vars) {
		items = append(items, CompletionItem{Label: v, Kind: completionKindVariable, Detail: "variable"})
	}
	return items
}

// ruleBodyStart returns the equals sign of the rule whose keyword is token st, if it
// comes before token prev, or -1.
func (a *analysis) ruleBodyStart(st, prev int) int {
	for j := st + 1; j <= prev; j++ {
		if a.isPunct(j, "=") {
			return j
		}
	}
	return -1
}

// scanBody scans the tokens of a rule body from its "=" (token eq) to token prev, and reports
// whether the cursor after prev is in a value expression (an action after "=>", or a predicate
// in {term}) and whether the declaration is an Operators Rule.
func (a *analysis) scanBody(eq, prev int) (value, operatorsRule bool) {
	first := a.significantToken(eq + 1)
	operatorsRule = a.isIdent(first, "operators") && a.isPunct(a.significantToken(first+1), "{")
	// Each PEG group has its own action context. Closing an inline group or
	// a predicate restores the enclosing parsing-expression context.
	type frame struct {
		close string
		value bool
		list  bool
	}
	stack := []frame{{}}
	for j := eq + 1; j <= prev; j++ {
		t := a.toks[j]
		top := &stack[len(stack)-1]
		previous := a.previousSignificantToken(j - 1)
		boundary := previous < 0 || a.isPunct(previous, "{") || a.isPunct(previous, ";") || a.toks[previous].End.Line < t.Pos.Line
		if top.list && boundary {
			top.value = false
		}
		if t.Kind == syntax.TokenPunct {
			switch t.Text {
			case "=>":
				top.value = true
			case "(":
				stack = append(stack, frame{close: ")", value: top.value})
			case "{":
				list := operatorsRule && (previous == first || top.list && (a.isIdent(previous, "level") || a.isIdent(a.previousSignificantToken(previous-1), "level")))
				stack = append(stack, frame{close: "}", value: !list, list: list})
			case "[":
				stack = append(stack, frame{close: "]", value: top.value})
			case ")", "}", "]":
				if len(stack) > 1 && top.close == t.Text {
					stack = stack[:len(stack)-1]
				}
			case ";":
				if stack[len(stack)-1].list {
					stack[len(stack)-1].value = false
				}
			}
		} else if top.list && boundary && t.Kind == syntax.TokenIdent {
			switch t.Text {
			case "skip", "operand", "level", "prefix", "postfix", "infix":
				stack[len(stack)-1].value = false
			}
		}
	}
	return stack[len(stack)-1].value, operatorsRule
}

// inValue reports whether the cursor after token prev is in a value expression of a rule body.
func (a *analysis) inValue(prev int) bool {
	if value, args := a.attributeArguments(prev); args {
		return value
	}
	st := prev
	for st >= 0 && !a.isDefKeyword(st) {
		st--
	}
	if st < 0 || !a.isIdent(st, "rule") {
		return false
	}
	eq := a.ruleBodyStart(st, prev)
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
	if first < 0 || !a.isIdent(first, "rule") {
		return items
	}
	seen := map[string]bool{}
	add := func(name, detail string) {
		if syntax.IsIdentifier(name) && !seen[name] {
			seen[name] = true
			items = append(items, CompletionItem{Label: name, Kind: completionKindVariable, Detail: detail})
		}
	}
	for j := first + 2; j < len(a.toks) && !a.isDefKeyword(j); j++ {
		t := a.toks[j]
		switch {
		case t.Kind == syntax.TokenIdent && a.isPunct(j+1, ":") && a.toks[j+1].start == t.end &&
			!(a.isPunct(j-1, "{") && a.inValue(j-1)) && !a.isPunct(j-1, ","):
			add(t.Text, "capture") // label:expr (not a constructor field T{...})
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

// declared is a name that a rule or type statement declares.
type declared struct {
	name string
	// spec is the token after the name of a type: struct, terminal or =.
	spec string
}

// declarations returns the names declared by rule (kind rule) or type (kind type) statements, in
// source order and without duplicates, including those of definitions with syntax errors.
func (a *analysis) declarations(kind symKind) []declared {
	kw := "rule"
	if kind == kindType {
		kw = "type"
	}
	var out []declared
	seen := map[string]bool{}
	for i := 0; i+1 < len(a.toks); i++ {
		next := a.significantToken(i + 1)
		if next >= len(a.toks) {
			continue
		}
		n := a.toks[next]
		if !a.isIdent(i, kw) || !a.declarationStart(i) || n.Kind != syntax.TokenIdent || !syntax.IsIdentifier(n.Text) || n.Pos.Line != a.toks[i].End.Line || seen[n.Text] {
			continue
		}
		d := declared{name: n.Text}
		spec := a.significantToken(next + 1)
		if a.isIdent(spec, "struct") || a.isIdent(spec, "terminal") || a.isPunct(spec, "=") {
			d.spec = a.toks[spec].Text
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

func directiveItems(list []builtin) []CompletionItem {
	var items []CompletionItem
	for _, b := range list {
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
// the struct types (for constructors).
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
