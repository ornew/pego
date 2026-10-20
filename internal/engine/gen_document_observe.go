package engine

import "fmt"

// generatedDocumentParseGuard begins a generated Document.ParseAST with a
// result of type typ.
func generatedDocumentParseGuard(typ string) string {
	return fmt.Sprintf(" if err := d.BeginParse(); err != nil { var zero %s; return zero, err }\n defer d.EndParse()\n", typ)
}
