package printer

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/token"
)

type GetTextFunc func(token.Range) []byte

type DemangleFunc func(uint32) (string, bool)

type Printer struct {
	GetText  GetTextFunc
	Demangle DemangleFunc
}

func (p *Printer) PrintIndent(n *cst.Node, indent string) string {
	var b strings.Builder
	name, ok := p.Demangle(n.Code)
	if !ok {
		name = "_"
	}
	b.WriteString(indent)
	b.WriteString(fmt.Sprintf("+ %s #%d (%s): %q\n", name, n.Code, n.Range, p.GetText(n.Range)))
	indent += "  "
	for _, c := range n.Children {
		b.WriteString(p.PrintIndent(c, indent))
	}
	return b.String()
}

func (p *Printer) Print(n *cst.Node) string {
	return p.PrintIndent(n, "")
}
