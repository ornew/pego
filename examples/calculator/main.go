// Command calculator parses an arithmetic expression with calc.zp and evaluates it.
//
//	go run ./examples/calculator "1 + 2 * (3 - 4) ^ 2"
package main

import (
	_ "embed"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/ornew/zeroparse"
)

//go:embed calc.zp
var grammar string

func main() {
	p, err := zeroparse.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	v, err := eval(p, strings.Join(os.Args[1:], " "))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(v)
}

func eval(p *zeroparse.Parser, src string) (float64, error) {
	n, err := p.Parse(src)
	if err != nil {
		return 0, err
	}
	return evalNode(n)
}

// evalNode evaluates the AST (Number, Binary, Unary) built by the actions of calc.zp.
func evalNode(n *zeroparse.Node) (float64, error) {
	switch n.Type() {
	case "Number":
		return strconv.ParseFloat(n.Text(), 64)
	case "Unary":
		x, err := evalNode(n.Field("X").(*zeroparse.Node))
		if err != nil {
			return 0, err
		}
		if n.Field("Op").(*zeroparse.Node).Text() == "-" {
			return -x, nil
		}
		return x, nil
	case "Binary":
		l, err := evalNode(n.Field("Left").(*zeroparse.Node))
		if err != nil {
			return 0, err
		}
		r, err := evalNode(n.Field("Right").(*zeroparse.Node))
		if err != nil {
			return 0, err
		}
		switch op := n.Field("Op").(*zeroparse.Node).Text(); op {
		case "+":
			return l + r, nil
		case "-":
			return l - r, nil
		case "*":
			return l * r, nil
		case "/":
			return l / r, nil
		case "%":
			return math.Mod(l, r), nil
		case "^":
			return math.Pow(l, r), nil
		}
	}
	return 0, fmt.Errorf("unexpected node %s", n.Type())
}
