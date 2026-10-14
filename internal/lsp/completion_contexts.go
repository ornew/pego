package lsp

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// contextualItems recognizes unfinished syntax without requiring the containing
// declaration to have survived partial parsing.
func (a *analysis) contextualItems(prev int) ([]CompletionItem, bool) {
	var stack []int
	for i := 0; i <= prev; i++ {
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
	name := a.previousSignificantToken(open - 1)
	if a.isPunct(open, "(") {
		if a.isIdent(name, "recover") && a.isPunct(name-1, "#") && a.toks[name-1].end == a.toks[name].start && a.toks[name].end == a.toks[open].start {
			if a.previousSignificantToken(prev) == open {
				return []CompletionItem{{Label: "skip", Kind: completionKindProperty, Detail: "recovery parsing expression", InsertText: "skip=$1", InsertTextFormat: insertSnippet}}, true
			}
		}
		if a.isIdent(name, "level") && a.toks[name].end == a.toks[open].start {
			first := a.significantToken(open + 1)
			comma := a.significantToken(first + 1)
			if first > prev {
				return a.ruleItems(), true
			}
			if a.isPunct(comma, ",") && comma <= prev {
				var items []CompletionItem
				if d := a.rules[a.toks[first].Text]; d != nil {
					if op, ok := d.rule.Expr.(*grammar.OperatorsRule); ok {
						for _, level := range op.Levels {
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
	if name < 0 || a.toks[name].Kind != syntax.TokenIdent || !a.inValue(name) {
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
	last := a.previousSignificantToken(prev)
	if last != open && !a.isPunct(last, ",") {
		return nil, false
	}
	used := map[string]bool{}
	depth := 0
	for i := open + 1; i <= prev; i++ {
		if a.isPunct(i, "(") || a.isPunct(i, "{") {
			depth++
		}
		if a.isPunct(i, ")") || a.isPunct(i, "}") {
			depth--
		}
		if depth == 0 && a.toks[i].Kind == syntax.TokenIdent && a.isPunct(a.significantToken(i+1), ":") {
			used[a.toks[i].Text] = true
		}
	}
	var items []CompletionItem
	for _, f := range st.Fields {
		if !used[f.Name] {
			items = append(items, CompletionItem{Label: f.Name, Kind: completionKindField, Detail: grammar.FormatType(f.Type), InsertText: f.Name + ": $1", InsertTextFormat: insertSnippet})
		}
	}
	return items, true
}
