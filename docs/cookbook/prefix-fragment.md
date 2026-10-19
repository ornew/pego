# Parse an Expression Embedded in Text

**Problem.** A template, a Markdown file or a string contains expressions between delimiters, such as `{{ price * qty }}`.
You do not want to write a grammar for the whole text, only for the expression, and you need to know where it ends so
that you can carry on scanning.

Scan the text for the opening delimiter yourself, and call `ParsePrefix` on what follows it. `ParsePrefix` runs one rule
on the beginning of its input, does not require the rule to reach the end, and reports how much it consumed. Put the
closing delimiter in the rule, so that the consumed length includes it. A syntax error is local to the fragment, so the
program adds the fragment's offset to report a position in the whole text.

```pego
// expr.pego
package expr

type Num = node[string]
type Name = node[string]
type Bin = { Op Op; Left Expr; Right Expr }
type Op = node[string]
type Expr ~ Num | Name | Bin

// A fragment is an expression followed by the closing delimiter.
rule fragment Expr = -ws e:sum -ws "}}" @error(message="expected an operator or '}}'") => $e

rule sum Expr = first:product rest:(-ws op:addop -ws p:product)* => foldl($first, $rest, (acc, i) => Bin{Op: i.$op, Left: acc, Right: i.$p})
rule product Expr = first:atom rest:(-ws op:mulop -ws a:atom)* => foldl($first, $rest, (acc, i) => Bin{Op: i.$op, Left: acc, Right: i.$a})
rule atom Expr = number / name / group
rule group Expr = "(" -ws e:sum -ws ")" => $e

rule addop Op = ("+" / "-") #atomic
rule mulop Op = ("*" / "/") #atomic
rule number Num = ([0-9]+ ("." [0-9]+)?) #atomic
rule name Name = ([a-z_]+) #atomic
rule ws = (&[ \t\r\n] .)*
```

```go
// main.go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ornew/pego"
)

//go:embed expr.pego
var grammar string

func eval(n *pego.Node, vars map[string]float64) (float64, error) {
	switch n.Type() {
	case "Num":
		return strconv.ParseFloat(n.Text(), 64)
	case "Name":
		name := n.Text()
		v, ok := vars[name]
		if !ok {
			return 0, fmt.Errorf("unknown name %q", name)
		}
		return v, nil
	}
	l, err := eval(n.Field("Left").(*pego.Node), vars)
	if err != nil {
		return 0, err
	}
	r, err := eval(n.Field("Right").(*pego.Node), vars)
	if err != nil {
		return 0, err
	}
	switch n.Field("Op").(*pego.Node).Text() {
	case "+":
		return l + r, nil
	case "-":
		return l - r, nil
	case "*":
		return l * r, nil
	}
	return l / r, nil
}

// lineCol converts an offset in text (ASCII here) to a 1-based line and column.
func lineCol(text string, off int) (int, int) {
	line, col := 1, 1
	for _, c := range text[:off] {
		if c == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}

// render replaces each {{ expression }} of the template by its value.
func render(p *pego.Parser, template string, vars map[string]float64) (string, error) {
	var out strings.Builder
	pos := 0
	for {
		i := strings.Index(template[pos:], "{{")
		if i < 0 {
			out.WriteString(template[pos:])
			return out.String(), nil
		}
		out.WriteString(template[pos : pos+i])
		base := pos + i + len("{{")
		res, err := p.ParsePrefix("fragment", template[base:])
		if err != nil {
			var se *pego.SyntaxError
			if errors.As(err, &se) {
				line, col := lineCol(template, base+se.Pos)
				return "", fmt.Errorf("%d:%d: %s", line, col, se.Message())
			}
			return "", err
		}
		v, err := eval(res.Node, vars)
		if err != nil {
			line, col := lineCol(template, base)
			return "", fmt.Errorf("%d:%d: %w", line, col, err)
		}
		out.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		pos = base + res.ConsumedBytes
	}
}

func main() {
	p, err := pego.CompileSource(grammar, "fragment")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	vars := map[string]float64{"price": 4.5, "qty": 3}
	for _, t := range []string{
		"Total: {{ price * qty }} (tax {{ (price * qty) / 10 }}).",
		"Line one\nTotal: {{ price * }} done",
		"Total: {{ price qty }} done",
		"Total: {{ cost }}",
	} {
		s, err := render(p, t, vars)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		fmt.Println(s)
	}
}
```

## Expected Output

```text
Total: 13.5 (tax 1.35).
error: 2:19: syntax error: expected "(", [0-9], [a-z_]
error: 1:17: expected an operator or '}}'
error: 1:10: unknown name "cost"
```

## How It Works

- `ParsePrefix("fragment", text)` parses the rule at the start of `text` and returns a `PrefixResult`. Its `Node` is the
  value of the rule, and `ConsumedBytes` is the number of bytes of `text` that the rule matched, including the closing
  `}}`. The scan continues at `base + ConsumedBytes`.
- `Parse` would fail on the text after the fragment. `ParsePrefix` does not check for the end of the input.
- The positions of nodes and errors are local to the string passed in. `base` converts them: here the text is ASCII, so a
  code point offset is a byte offset. For text with multibyte characters, convert a code point span to bytes first (see
  [Parsing a Prefix](../guide/prefix-parsing.md)).
- The label on the closing delimiter says what is expected after a complete expression (`price qty`). A missing operand
  (`price * }}`) has no label and gets the mechanical list of what could start one. An error in the evaluation, such as an
  unknown name, is reported at the start of the fragment.

## Variations and Limits

- Use `WithUnit(pego.Bytes)` when the surrounding text is processed in bytes, so that spans, errors and
  `ConsumedBytes` all agree.
- A delimiter that can appear inside the fragment, such as `}}` in a string literal, belongs in the grammar: the rule
  reads the string, so the scan never mistakes it for the end.
- A fragment grammar is an ordinary grammar: the same rule can be the start rule of a parser that reads a whole file of
  expressions.
