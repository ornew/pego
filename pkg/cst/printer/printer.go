package printer

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/scanner"
)

type Printer struct {
	Source         scanner.Scanner
	DecodeNodeType func(int) string
	SourcePrint    bool
}

func (p Printer) PrintString(n *cst.Node) string {
	var b strings.Builder
	p.PrintIndent(&b, n, 0)
	return b.String()
}

func (p *Printer) PrintIndent(w io.Writer, n *cst.Node, i int) error {
	if p.DecodeNodeType == nil {
		fmt.Fprintf(w, "%d:", n.Type)
	} else {
		fmt.Fprintf(w, "%s:", p.DecodeNodeType(n.Type))
	}
	if p.SourcePrint {
		if ra, ok := p.Source.(io.ReaderAt); ok {
			size := n.End() - n.Pos()
			buf := make([]byte, size)
			rn, err := ra.ReadAt(buf, n.Pos().Int64())
			if err != nil && !errors.Is(err, io.EOF) {
				fmt.Fprintf(w, " (cannot read: %v)", err)
			} else {
				fmt.Fprintf(w, " %q", buf[:rn])
			}
		} else {
			fmt.Fprintf(w, " (not io.ReaderAt)")
		}
	}
	fmt.Fprintf(w, " [%d:%d]", n.Pos(), n.End())
	if len(n.Children) > 0 {
		fmt.Fprintf(w, " {\n")
		for _, c := range n.Children {
			for j := 0; j <= i; j++ {
				fmt.Fprintf(w, ". ")
			}
			p.PrintIndent(w, c, i+1)
			fmt.Fprintf(w, "\n")
		}
		for j := 0; j <= i-1; j++ {
			fmt.Fprintf(w, ". ")
		}
		fmt.Fprintf(w, "}")
	}
	return nil
}
