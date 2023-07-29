package main

import (
	"fmt"
	"os"
	"strings"

	calc "github.com/ornew/pego/examples/calc"
	"github.com/ornew/pego/pkg/parser"
	"github.com/ornew/pego/pkg/printer"
)

func run() error {
	in := `1+2*(3-4)/5`
	// in := `1+2*3-4/(5+6*7-8/9)+10`
	fmt.Println(in)
	p, err := parser.NewParser(strings.NewReader(in))
	if err != nil {
		return err
	}
	node, err := calc.Parse(p)
	if err != nil {
		return err
	}
	pp := printer.Printer{
		GetText:  p.GetText,
		Demangle: calc.Demangle,
	}
	fmt.Println(pp.Print(node))
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
