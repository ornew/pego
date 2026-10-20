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

func (s *Server) completionControlled(params json.RawMessage, control *renderControl) (any, *rpcError) {
	control.poll()
	d, off, err := s.position(params)
	if err != nil {
		return nil, err
	}
	items := d.analysis().completeControlled(off, control)
	for i := range items {
		control.poll()
		if items[i].Documentation != nil {
			doc := documentationControlled(items[i].Documentation.Value, s.client.TextDocument.Completion.CompletionItem.DocumentationFormat, control)
			items[i].Documentation = &doc
		}
	}
	if !s.client.TextDocument.Completion.CompletionItem.SnippetSupport {
		// Without snippets, insert the label.
		for i := range items {
			control.poll()
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

// isDefKeyword reports whether token i is rule, type or var.
func (a *analysis) isDefKeyword(i int) bool {
	return a.declarationStart(i) && (a.isIdent(i, "rule") || a.isIdent(i, "type") || a.isIdent(i, "var"))
}

func (a *analysis) declarationStart(i int) bool {
	return i >= 0 && i < len(a.declarationStarts) && a.declarationStarts[i]
}

// indexDeclarationStarts scans once; completion and semantic lookup must not
// rescan the entire token prefix for every potential contextual keyword.
func (a *analysis) indexDeclarationStartsControlled(control *renderControl) {
	control.poll()
	a.declarationStarts = make([]bool, len(a.toks))
	depth, previous := 0, -1
	for i, t := range a.toks {
		control.poll()
		if t.Kind == syntax.TokenComment {
			continue
		}
		boundary := previous < 0 || a.isPunct(previous, ";") || a.toks[previous].End.Line < t.Pos.Line
		if boundary && len(a.syntaxErrs) > 0 && (a.isIdent(i, "rule") || a.isIdent(i, "type") || a.isIdent(i, "var")) {
			next := a.significantTokenControlled(i+1, control)
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
func (a *analysis) insideControlled(t token, off int, control *renderControl) bool {
	control.poll()
	switch t.Kind {
	case syntax.TokenComment:
		if strings.HasPrefix(t.Text, "/*") {
			return off > t.start && (off < t.end || !strings.HasSuffix(t.Text, "*/"))
		}
		// A comment extends to the end of the line, past trailing white space.
		for k := t.start; k < off; k++ {
			control.poll()
			if a.text[k] == '\n' || a.text[k] == '\r' {
				return false
			}
		}
		return off > t.start
	case syntax.TokenString:
		return off > t.start && (off < t.end || !terminatedControlled(a.text[t.start:t.end], '"', 1, control))
	case syntax.TokenCharClass:
		return off > t.start && (off < t.end || !terminatedControlled(a.text[t.start:t.end], ']', 1, control))
	}
	return false
}

// terminated reports whether the string or character class raw, whose content starts at index
// from, ends with its closing character close.
func terminatedControlled(raw string, close byte, from int, control *renderControl) bool {
	control.poll()
	for i := from; i < len(raw); i++ {
		control.poll()
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
func (a *analysis) completeControlled(off int, control *renderControl) []CompletionItem {
	control.poll()
	items := []CompletionItem{}
	// i is the last token that starts before off.
	i := sort.Search(len(a.toks), func(i int) bool { return a.toks[i].start >= off }) - 1
	if i >= 0 && a.insideControlled(a.toks[i], off, control) {
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
	if items, found := a.contextualItemsControlled(prev, off, control); found {
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
	if (capture || adjacent == ".") && !a.inValueControlled(prev, control) {
		return items
	}
	if capture {
		return a.captureItemsControlled(prev, control)
	}
	if value, args := a.attributeArgumentsControlled(prev, control); args {
		if !value {
			return a.ruleItemsControlled(control)
		}
		if adjacent == "." {
			return append(a.memberItemsControlled(control), a.captureMemberItemsControlled(prev, control)...)
		}
		return a.valueItemsControlled(control)
	}
	// st is the keyword of the definition the cursor is in.
	prev = a.previousSignificantTokenControlled(prev, control)
	st := prev
	for st >= 0 && !a.isDefKeyword(st) {
		control.poll()
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
			return items // only = or ~ follows the name
		case prev == st+2 && pt.Text == "=":
			return items // only a struct body follows; aliases are written with ~
		case pt.Text == "{" || pt.Text == ";":
			return items // a field name
		}
		return append(a.typeItemsControlled(false, control), keywordItems(statementKeywords)...)
	}
	// A rule definition's optional type comes before its equals sign.
	eq := a.ruleBodyStartControlled(st, prev, control)
	if eq < 0 {
		if prev == st+1 {
			return a.typeItemsControlled(false, control)
		}
		switch pt.Text {
		case ":", "|", "[]", "*", "(":
			return a.typeItemsControlled(false, control)
		}
		return items
	}
	value, operatorsRule := a.scanBodyControlled(eq, prev, control)
	if value {
		switch pt.Text {
		case ".":
			return append(a.memberItemsControlled(control), a.captureMemberItemsControlled(prev, control)...)
		}
		items = append(items, builtinFuncItems()...)
		items = append(items, a.typeItemsControlled(true, control)...)
		items = append(items, keywordItems(termKeywords)...)
		// A parameter hides a variable of the same name.
		params := a.paramItemsControlled(prev, control)
		hidden := map[string]bool{}
		for _, p := range params {
			hidden[p.Label] = true
		}
		for _, v := range sortedKeysControlled(a.vars, control) {
			control.poll()
			if !hidden[v] {
				items = append(items, CompletionItem{Label: v, Kind: completionKindVariable, Detail: "variable"})
			}
		}
		return append(items, params...)
	}
	if pt.Text == "infix" {
		return keywordItems([]string{"left", "right", "none"})
	}
	items = append(items, a.ruleItemsControlled(control)...)
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
func (a *analysis) attributeArgumentsControlled(prev int, control *renderControl) (value, found bool) {
	control.poll()
	type context struct {
		attribute, argument string
		pragma              bool
	}
	var stack []context
	for j := 0; j <= prev; j++ {
		control.poll()
		if a.isDefKeyword(j) {
			stack = nil
		}
		if a.isPunct(j, "(") {
			c := context{}
			name := a.previousSignificantTokenControlled(j-1, control)
			if name >= 0 {
				at := a.previousSignificantTokenControlled(name-1, control)
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
			name := a.previousSignificantTokenControlled(j-1, control)
			if name >= 0 {
				stack[len(stack)-1].argument = a.toks[name].Text
			}
		}
	}
	for j := len(stack) - 1; j >= 0; j-- {
		control.poll()
		if stack[j].attribute != "" {
			return !stack[j].pragma || stack[j].attribute != "recover" || stack[j].argument != "skip", true
		}
	}
	return false, false
}

func (a *analysis) valueItemsControlled(control *renderControl) []CompletionItem {
	control.poll()
	items := append(builtinFuncItems(), a.typeItemsControlled(true, control)...)
	items = append(items, keywordItems(termKeywords)...)
	for _, v := range sortedKeysControlled(a.vars, control) {
		control.poll()
		items = append(items, CompletionItem{Label: v, Kind: completionKindVariable, Detail: "variable"})
	}
	return items
}

// ruleBodyStart returns the equals sign of the rule whose keyword is token st, if it
// comes before token prev, or -1.
func (a *analysis) ruleBodyStartControlled(st, prev int, control *renderControl) int {
	control.poll()
	for j := st + 1; j <= prev; j++ {
		control.poll()
		if a.isPunct(j, "=") {
			return j
		}
	}
	return -1
}

// scanBody scans the tokens of a rule body from its "=" (token eq) to token prev, and reports
// whether the cursor after prev is in a value expression (an action after "=>", or a predicate
// in {term}) and whether the declaration is an Operators Rule.
func (a *analysis) scanBodyControlled(eq, prev int, control *renderControl) (value, operatorsRule bool) {
	control.poll()
	first := a.significantTokenControlled(eq+1, control)
	operatorsRule = a.isIdent(first, "operators") && a.isPunct(a.significantTokenControlled(first+1, control), "{")
	// Each PEG group has its own action context. Closing an inline group or
	// a predicate restores the enclosing parsing-expression context.
	type frame struct {
		close string
		value bool
		list  bool
	}
	stack := []frame{{}}
	for j := eq + 1; j <= prev; j++ {
		control.poll()
		t := a.toks[j]
		top := &stack[len(stack)-1]
		previous := a.previousSignificantTokenControlled(j-1, control)
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
				list := operatorsRule && (previous == first || top.list && (a.isIdent(previous, "level") || a.isIdent(a.previousSignificantTokenControlled(previous-1, control), "level")))
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
func (a *analysis) inValueControlled(prev int, control *renderControl) bool {
	control.poll()
	if value, args := a.attributeArgumentsControlled(prev, control); args {
		return value
	}
	st := prev
	for st >= 0 && !a.isDefKeyword(st) {
		control.poll()
		st--
	}
	if st < 0 || !a.isIdent(st, "rule") {
		return false
	}
	eq := a.ruleBodyStartControlled(st, prev, control)
	if eq < 0 {
		return false
	}
	value, _ := a.scanBodyControlled(eq, prev, control)
	return value
}

// captureItems returns the capture labels of the definition that token i is in, for completion
// after "$".
func (a *analysis) captureItemsControlled(i int, control *renderControl) []CompletionItem {
	control.poll()
	items := []CompletionItem{}
	first := i
	for first >= 0 && !a.isDefKeyword(first) {
		control.poll()
		first--
	}
	if first < 0 || !a.isIdent(first, "rule") {
		return items
	}
	seen := map[string]bool{}
	add := func(name, detail string) {
		if control.isIdentifier(name) && !seen[name] {
			seen[name] = true
			items = append(items, CompletionItem{Label: name, Kind: completionKindVariable, Detail: detail})
		}
	}
	for j := first + 2; j < len(a.toks) && !a.isDefKeyword(j); j++ {
		control.poll()
		if a.isLabelControlled(j, control) {
			add(a.toks[j].Text, "capture")
		}
	}
	return items
}

// captureMemberItems returns the captures that the receiver before the "." at token dot may
// have, for completion as the capture reads x.$y. A capture $x has the captures of the rule it
// labels; another receiver, such as a lambda parameter over a repetition, those inside the
// labelled groups of the definition and those of the rules repeated directly after a label. Typing a name after
// the "." still finds them.
func (a *analysis) captureMemberItemsControlled(dot int, control *renderControl) []CompletionItem {
	control.poll()
	items := []CompletionItem{}
	first := dot
	for first >= 0 && !a.isDefKeyword(first) {
		control.poll()
		first--
	}
	if first < 0 || !a.isIdent(first, "rule") {
		return items
	}
	seen := map[string]bool{}
	add := func(found []CompletionItem) {
		for _, it := range found {
			control.poll()
			if !seen[it.Label] {
				seen[it.Label] = true
				it.InsertText = "$" + it.Label
				it.Label = it.InsertText
				items = append(items, it)
			}
		}
	}
	ruleCaptures := func(i int) {
		if a.isPunct(a.significantTokenControlled(i+1, control), "(") {
			return // a call of a parameterized rule, whose arguments may hold captures
		}
		if d := a.rules[a.toks[i].Text]; d != nil {
			if kw, ok := a.tokAt[d.kwStart]; ok {
				add(a.captureItemsControlled(kw, control))
			}
		}
	}
	recv := a.previousSignificantTokenControlled(dot-1, control)
	capture := recv >= 0 && a.toks[recv].Kind == syntax.TokenCapture
	for j := first + 2; j < len(a.toks) && !a.isDefKeyword(j); j++ {
		control.poll()
		if !a.isLabelControlled(j, control) {
			continue
		}
		target := a.significantTokenControlled(j+2, control)
		if target >= len(a.toks) {
			break
		}
		switch {
		case a.toks[target].Kind == syntax.TokenIdent:
			if capture && strings.TrimPrefix(a.toks[recv].Text, "$") == a.toks[j].Text || !capture && a.repeatedControlled(target, control) {
				ruleCaptures(target)
			}
		case !capture && a.isPunct(target, "("):
			for k, depth := target+1, 1; k < len(a.toks) && depth > 0 && !a.isDefKeyword(k); k++ {
				control.poll()
				switch {
				case a.isPunct(k, "("):
					depth++
				case a.isPunct(k, ")"):
					depth--
				case a.isLabelControlled(k, control):
					add([]CompletionItem{{Label: a.toks[k].Text, Kind: completionKindVariable, Detail: "capture"}})
				}
			}
		}
	}
	return items
}

// repeated reports whether the rule called at token i is repeated, as the elements a lambda
// over the capture of the repetition reads.
func (a *analysis) repeatedControlled(i int, control *renderControl) bool {
	next := a.significantTokenControlled(i+1, control)
	return a.isPunct(next, "*") || a.isPunct(next, "+") || a.isPunct(next, "#") && a.isIdent(next+1, "repeat")
}

// isLabel reports whether token i is the label of a capture, label:expr, and not a field of a
// constructor T{...} or a later one after ",".
func (a *analysis) isLabelControlled(i int, control *renderControl) bool {
	t := a.toks[i]
	return t.Kind == syntax.TokenIdent && a.isPunct(i+1, ":") && a.toks[i+1].start == t.end &&
		!(a.isPunct(i-1, "{") && a.inValueControlled(i-1, control)) && !a.isPunct(i-1, ",")
}

// paramItems returns the parameters of the lambdas, (a, b) =>, whose bodies token i is in: a body
// runs from its => to the "," or closing bracket of the brackets the lambda is in.
func (a *analysis) paramItemsControlled(i int, control *renderControl) []CompletionItem {
	control.poll()
	items := []CompletionItem{}
	first := i
	for first >= 0 && !a.isDefKeyword(first) {
		control.poll()
		first--
	}
	seen := map[string]bool{}
	for j := first + 1; j <= i && j < len(a.toks); j++ {
		control.poll()
		if !a.isPunct(j, "=>") || !a.isPunct(j-1, ")") || !a.inLambdaBody(j, i, control) {
			continue
		}
		// The parameter list alternates names and commas, and a lambda is always a call argument,
		// after "(" or ",": "x:(a b) =>" is a rule body and its action.
		open := j - 2
		var names []string
		for ; open > first && a.toks[open].Kind == syntax.TokenIdent; open -= 2 {
			control.poll()
			names = append(names, a.toks[open].Text)
			if !a.isPunct(open-1, ",") {
				open--
				break
			}
		}
		if open == j-2 && a.isPunct(open, "(") {
			names = nil // ()
		} else if open <= first || !a.isPunct(open, "(") || len(names) == 0 {
			continue
		}
		if !a.isPunct(open-1, "(") && !a.isPunct(open-1, ",") {
			continue
		}
		for _, n := range names {
			if control.isIdentifier(n) && !seen[n] {
				seen[n] = true
				items = append(items, CompletionItem{Label: n, Kind: completionKindVariable, Detail: "parameter"})
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
func (a *analysis) declarationsControlled(kind symKind, control *renderControl) []declared {
	control.poll()
	kw := "rule"
	if kind == kindType {
		kw = "type"
	}
	var out []declared
	seen := map[string]bool{}
	for i := 0; i+1 < len(a.toks); i++ {
		control.poll()
		next := a.significantTokenControlled(i+1, control)
		if next >= len(a.toks) {
			continue
		}
		n := a.toks[next]
		if !a.isIdent(i, kw) || !a.declarationStart(i) || n.Kind != syntax.TokenIdent || !control.isIdentifier(n.Text) || n.Pos.Line != a.toks[i].End.Line || seen[n.Text] {
			continue
		}
		d := declared{name: n.Text}
		spec := a.significantTokenControlled(next+1, control)
		switch {
		case a.isPunct(spec, "=") && a.isPunct(a.significantTokenControlled(spec+1, control), "{"):
			d.spec = "struct"
		case a.isIdent(spec, "terminal") || a.isPunct(spec, "~"):
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
func (a *analysis) memberItemsControlled(control *renderControl) []CompletionItem {
	control.poll()
	var items []CompletionItem
	for _, b := range builtinMembers {
		control.poll()
		items = append(items, CompletionItem{Label: b.name, Kind: completionKindField, Detail: b.signature,
			Documentation: &markupContent{Kind: "markdown", Value: b.doc}, SortText: "9" + b.name})
	}
	seen := map[string]bool{}
	for _, d := range a.defs {
		control.poll()
		if d.typ == nil {
			continue
		}
		st, ok := d.typ.(*grammar.TypeDef)
		if !ok {
			continue
		}
		for _, f := range st.Fields() {
			control.poll()
			if !seen[f.Name] {
				seen[f.Name] = true
				items = append(items, CompletionItem{Label: f.Name, Kind: completionKindField,
					Detail: d.name + "." + f.Name + " " + control.formatType(f.Type)})
			}
		}
	}
	return items
}

func (a *analysis) ruleItemsControlled(control *renderControl) []CompletionItem {
	control.poll()
	var items []CompletionItem
	for _, n := range a.declarationsControlled(kindRule, control) {
		control.poll()
		it := CompletionItem{Label: n.name, Kind: completionKindFunction}
		if d := a.rules[n.name]; d != nil {
			if t, _, ok := a.ruleTypeControlled(d, control); ok {
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
func (a *analysis) typeItemsControlled(structsOnly bool, control *renderControl) []CompletionItem {
	control.poll()
	var items []CompletionItem
	for _, n := range a.declarationsControlled(kindType, control) {
		control.poll()
		if structsOnly && n.spec != "struct" {
			continue
		}
		it := CompletionItem{Label: n.name, Kind: completionKindClass, Detail: n.spec}
		if n.spec == "struct" {
			it.Kind = completionKindStruct
		}
		if d := a.types[n.name]; d != nil {
			_, it.Detail = typeSymbolKindControlled(d.typ, control)
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
		control.poll()
		items = append(items, CompletionItem{Label: b.name, Kind: completionKindClass, Detail: "built-in type",
			Documentation: &markupContent{Kind: "markdown", Value: b.doc}, SortText: "9" + b.name})
	}
	return items
}

func sortedKeysControlled(m map[string]bool, control *renderControl) []string {
	control.poll()
	keys := make([]string, 0, len(m))
	for k := range m {
		control.poll()
		keys = append(keys, k)
	}
	control.sortKeys(keys)
	return keys
}

// inLambdaBody reports whether token i is in the body of the lambda whose => is token arrow.
func (a *analysis) inLambdaBody(arrow, i int, control *renderControl) bool {
	depth := 0
	for k := arrow + 1; k < i && k < len(a.toks); k++ {
		control.poll()
		switch {
		case a.isPunct(k, "("), a.isPunct(k, "["), a.isPunct(k, "{"):
			depth++
		case a.isPunct(k, ")"), a.isPunct(k, "]"), a.isPunct(k, "}"):
			if depth == 0 {
				return false
			}
			depth--
		case a.isPunct(k, ",") && depth == 0:
			return false
		}
	}
	return true
}
