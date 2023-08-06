package calc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/pkg/parser"
	"github.com/ornew/pego/pkg/printer"
)

func TestParse(t *testing.T) {
	in := `1+2*(3-4)/5`
	// in := `1+2*3-4/(5+6*7-8/9)+10`
	fmt.Println(in)
	p, err := parser.NewParser(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	node, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	pp := printer.Printer{
		GetText:  p.GetText,
		Demangle: Demangle,
	}
	fmt.Println(pp.Print(node))
}
