package lsp

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// contextualItems recognizes unfinished syntax without requiring the containing
// declaration to have survived partial parsing.
func (a *analysis) contextualItemsControlled(prev, off int, control *renderControl) ([]CompletionItem, bool) {
	control.poll()
	var stack []int
	for i := 0; i <= prev; i++ {
		control.poll()
		if a.isDefKeyword(i) {
			stack = nil
		}
		if a.isPunct(i, "(") || a.isPunct(i, "{") {
			stack = append(stack, i)
		}
		if (a.isPunct(i, ")") || a.isPunct(i, "}")) && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) == 0 {
		return nil, false
	}
	open := stack[len(stack)-1]
	name := a.previousSignificantTokenControlled(open-1, control)
	if a.isPunct(open, "(") {
		if a.isIdent(name, "recover") && a.isPunct(name-1, "#") && a.toks[name-1].end == a.toks[name].start && a.toks[name].end == a.toks[open].start {
			if a.previousSignificantTokenControlled(prev, control) == open {
				return []CompletionItem{{Label: "skip", Kind: completionKindProperty, Detail: "recovery parsing expression", InsertText: "skip=$1", InsertTextFormat: insertSnippet}}, true
			}
		}
		if a.isIdent(name, "level") && a.toks[name].end == a.toks[open].start {
			first := a.significantTokenControlled(open+1, control)
			comma := a.significantTokenControlled(first+1, control)
			if first > prev {
				return a.ruleItemsControlled(control), true
			}
			if a.isPunct(comma, ",") && comma <= prev {
				var items []CompletionItem
				if d := a.rules[a.toks[first].Text]; d != nil {
					if op, ok := d.rule.Expr.(*grammar.OperatorsRule); ok {
						for _, level := range op.Levels {
							control.poll()
							if level.Name != "" {
								items = append(items, CompletionItem{Label: level.Name, Kind: completionKindVariable, Detail: "binding level of " + d.name})
							}
						}
					}
				}
				return items, true
			}
		}
		return nil, false
	}
	if a.isIdent(name, "struct") {
		typeName := a.previousSignificantTokenControlled(name-1, control)
		keyword := a.previousSignificantTokenControlled(typeName-1, control)
		if typeName >= 0 && a.toks[typeName].Kind == syntax.TokenIdent && a.isIdent(keyword, "type") && a.isDefKeyword(keyword) {
			last := a.previousSignificantTokenControlled(prev, control)
			// A field name starts after the opening brace, a semicolon or a
			// physical newline. Open type groups are handled by the innermost
			// parenthesis above, so their continuation is not a new field.
			if last == open || a.isPunct(last, ";") || a.toks[last].End.Line < a.idx.position(off).Line+1 {
				return append(a.typeItemsControlled(false, control), CompletionItem{
					Label: "FieldName Type", Kind: completionKindSnippet, Detail: "struct field declaration",
					InsertText: "${1:FieldName} ${2:Type}", InsertTextFormat: insertSnippet,
				}), true
			}
			return nil, false // The existing field-type completion handles this position.
		}
	}
	if name < 0 || a.toks[name].Kind != syntax.TokenIdent || !a.inValueControlled(name, control) {
		return nil, false
	}
	d := a.types[a.toks[name].Text]
	if d == nil {
		return nil, false
	}
	st, ok := d.typ.Spec.(*grammar.StructSpec)
	if !ok {
		return nil, false
	}
	last := a.previousSignificantTokenControlled(prev, control)
	if last != open && !a.isPunct(last, ",") {
		return nil, false
	}
	used := map[string]bool{}
	depth := 0
	for i := open + 1; i <= prev; i++ {
		control.poll()
		if a.isPunct(i, "(") || a.isPunct(i, "{") {
			depth++
		}
		if a.isPunct(i, ")") || a.isPunct(i, "}") {
			depth--
		}
		if depth == 0 && a.toks[i].Kind == syntax.TokenIdent && a.isPunct(a.significantTokenControlled(i+1, control), ":") {
			used[a.toks[i].Text] = true
		}
	}
	var items []CompletionItem
	for _, f := range st.Fields {
		control.poll()
		if !used[f.Name] {
			items = append(items, CompletionItem{Label: f.Name, Kind: completionKindField, Detail: control.formatType(f.Type), InsertText: f.Name + ": $1", InsertTextFormat: insertSnippet})
		}
	}
	return items, true
}
