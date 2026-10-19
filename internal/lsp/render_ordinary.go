package lsp

import (
	"encoding/json"
	"regexp"

	"github.com/ornew/pego/grammar"
)

func (s *Server) documentSymbol(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) { return s.documentSymbolControlled(params, control) })
}
func (a *analysis) symbols() []DocumentSymbol {
	return a.symbolsControlled(nil)
}
func typeSymbolKind(td *grammar.TypeDef) (int, string) {
	return typeSymbolKindControlled(td, nil)
}
func (a *analysis) fieldSymbols(d *definition, st *grammar.TypeDef) []DocumentSymbol {
	return a.fieldSymbolsControlled(d, st, nil)
}
func (s *Server) hover(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) { return s.hoverControlled(params, control) })
}
func (a *analysis) hoverAt(off int) (text string, start, end int, ok bool) {
	return a.hoverAtControlled(off, nil)
}
func (a *analysis) ruleHover(d *definition) string {
	return a.ruleHoverControlled(d, nil)
}
func typeHover(d *definition) string {
	return typeHoverControlled(d, nil)
}
func (s *Server) completion(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) { return s.completionControlled(params, control) })
}
func (a *analysis) inside(t token, off int) bool {
	return a.insideControlled(t, off, nil)
}
func terminated(raw string, close byte, from int) bool {
	return terminatedControlled(raw, close, from, nil)
}
func (a *analysis) complete(off int) []CompletionItem {
	return a.completeControlled(off, nil)
}
func (a *analysis) attributeArguments(prev int) (value, found bool) {
	return a.attributeArgumentsControlled(prev, nil)
}
func (a *analysis) valueItems() []CompletionItem {
	return a.valueItemsControlled(nil)
}
func (a *analysis) ruleBodyStart(st, prev int) int {
	return a.ruleBodyStartControlled(st, prev, nil)
}
func (a *analysis) scanBody(eq, prev int) (value, operatorsRule bool) {
	return a.scanBodyControlled(eq, prev, nil)
}
func (a *analysis) inValue(prev int) bool {
	return a.inValueControlled(prev, nil)
}
func (a *analysis) captureItems(i int) []CompletionItem {
	return a.captureItemsControlled(i, nil)
}
func (a *analysis) declarations(kind symKind) []declared {
	return a.declarationsControlled(kind, nil)
}
func (a *analysis) memberItems() []CompletionItem {
	return a.memberItemsControlled(nil)
}
func (a *analysis) ruleItems() []CompletionItem {
	return a.ruleItemsControlled(nil)
}
func (a *analysis) typeItems(structsOnly bool) []CompletionItem {
	return a.typeItemsControlled(structsOnly, nil)
}
func sortedKeys(m map[string]bool) []string {
	return sortedKeysControlled(m, nil)
}
func (a *analysis) contextualItems(prev, off int) ([]CompletionItem, bool) {
	return a.contextualItemsControlled(prev, off, nil)
}
func (a *analysis) significantToken(i int) int {
	return a.significantTokenControlled(i, nil)
}
func (a *analysis) previousSignificantToken(i int) int {
	return a.previousSignificantTokenControlled(i, nil)
}
func (a *analysis) ruleType(d *definition) (typ string, inferred, known bool) {
	return a.ruleTypeControlled(d, nil)
}
func documentation(text string, formats []string) markupContent {
	return documentationControlled(text, formats, nil)
}
func plainDocumentation(text string) string {
	return plainDocumentationControlled(text, nil)
}
func plainMarkup(text string) string {
	return plainMarkupControlled(text, nil)
}
func stripUnderscoreMarkup(text string, pattern *regexp.Regexp) string {
	return stripUnderscoreMarkupControlled(text, pattern, nil)
}
