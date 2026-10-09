# Compile a filter language into a Go predicate

**Problem.** Users of your tool write filters such as `age > 30 and name ~ "a*"` in a query box or a configuration
file. You want a `func(Record) bool` that is cheap to call on every record.

Parse the filter once and turn the tree into Go closures. The grammar gives the keywords, the precedence of `or`, `and`
and `not`, and comparisons that do not chain.

```pego
// filter.pego
type Num terminal
type Str terminal
type Bool terminal
type Field terminal
type Binary struct { Left Expr, Op Match, Right Expr }
type Not struct { X Expr }
type Expr = Num | Str | Bool | Field | Binary | Not

def main: Expr = ws e:expr ws $$ -> $e

def expr: Expr = pratt {
    skip    ws
    operand num
    operand str
    operand bool
    operand field
    operand "(" e:expr ws ")" -> $e
    level { infix left "or" !wordchar                 -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left "and" !wordchar                -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { prefix "not" !wordchar                    -> new Not{X: $rhs} }
    level { infix none "=" / "!=" / "<=" / ">=" / "<" / ">" / "~"
                                                      -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

def num: Num = "-"? (?0-9)+ ("." (?0-9)+)?
def str: Str = "\"" ("\\" . / (?^"\\))* "\""
def bool: Bool = ("true" / "false") !wordchar
def field: Field = !keyword (?a-z_) wordchar*
def keyword = ("and" / "or" / "not") !wordchar
def wordchar = (?a-z0-9_)
def ws = (? \t\r\n)*
```

```go
// main.go
package main

import (
	"cmp"
	_ "embed"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"

	"github.com/ornew/pego"
)

//go:embed filter.pego
var grammar string

// Record is what a filter looks at.
type Record map[string]any

// value computes the value of a subexpression for a record.
type value func(Record) any

// compile turns a tree into closures, so that the tree is walked once, not once per record.
func compile(n *pego.Node) value {
	switch n.Type() {
	case "Num":
		v, _ := strconv.ParseFloat(n.Text, 64)
		return func(Record) any { return v }
	case "Str":
		s, _ := strconv.Unquote(n.Text)
		return func(Record) any { return s }
	case "Bool":
		b := n.Text == "true"
		return func(Record) any { return b }
	case "Field":
		name := n.Text
		return func(r Record) any { return r[name] }
	case "Not":
		x := compile(n.Field("X").(*pego.Node))
		return func(r Record) any { return x(r) != true }
	}
	// Binary
	op := n.Field("Op").(*pego.Node).Text
	l := compile(n.Field("Left").(*pego.Node))
	r := compile(n.Field("Right").(*pego.Node))
	switch op {
	case "and":
		return func(rec Record) any { return l(rec) == true && r(rec) == true }
	case "or":
		return func(rec Record) any { return l(rec) == true || r(rec) == true }
	}
	return func(rec Record) any { return compare(op, l(rec), r(rec)) }
}

// order compares two numbers or two strings.
func order(a, b any) (int, bool) {
	switch x := a.(type) {
	case int:
		a = float64(x)
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y), true
		}
	}
	if y, ok := b.(int); ok {
		b = float64(y)
	}
	x, ok1 := a.(float64)
	y, ok2 := b.(float64)
	return cmp.Compare(x, y), ok1 && ok2
}

func compare(op string, a, b any) bool {
	if op == "~" { // a glob pattern, such as "a*"
		s, ok1 := a.(string)
		pattern, ok2 := b.(string)
		m, _ := path.Match(pattern, s)
		return ok1 && ok2 && m
	}
	c, ok := order(a, b)
	if !ok { // booleans and values of different types
		return op == "=" && a != nil && a == b || op == "!=" && a != b
	}
	switch op {
	case "=":
		return c == 0
	case "!=":
		return c != 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	}
	return c >= 0 // ">="
}

// Compile parses a filter and returns a predicate.
func Compile(p *pego.Parser, src string) (func(Record) bool, error) {
	tree, err := p.Parse(src)
	if err != nil {
		return nil, err
	}
	v := compile(tree)
	return func(r Record) bool { return v(r) == true }, nil
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	people := []Record{
		{"name": "Ann", "age": 34, "admin": true},
		{"name": "Bob", "age": 25},
		{"name": "alice", "age": 41},
		{"name": "Carl", "age": 52, "admin": false},
	}
	for _, q := range []string{
		`age > 30 and name ~ "[Aa]*"`,
		`not admin = true or age < 30`,
		`(name = "Bob" or name = "Carl") and age >= 25`,
		`age > 30 and`,
	} {
		match, err := Compile(p, q)
		if err != nil {
			fmt.Printf("%-48s error: %v\n", q, err)
			continue
		}
		var names []string
		for _, r := range people {
			if match(r) {
				names = append(names, r["name"].(string))
			}
		}
		fmt.Printf("%-48s %v\n", q, names)
	}
}
```

```text
age > 30 and name ~ "[Aa]*"                      [Ann alice]
not admin = true or age < 30                     [Bob alice Carl]
(name = "Bob" or name = "Carl") and age >= 25    [Bob Carl]
age > 30 and                                     error: 1:13: syntax error: expected "(", "-", "\"", "false", "not", "true", (?0-9), (?a-z_)
```

## How it works

- **Word operators.** `"and" !wordchar` is the operator `and` only when no letter or digit follows, so a field named
  `android` is not read as `and` followed by `roid`. The `!wordchar` is a lookahead: it consumes nothing, so `$op` is
  just `and`. `field` excludes the keywords with `!keyword`, so `and` cannot be a field name.
- **Precedence from the table.** Loosest first: `or`, `and`, `not`, and the comparisons. `not admin = true` is
  `not (admin = true)`, and `a = b = c` is rejected, because `infix none` does not chain.
- **Closures.** `compile` walks the tree once and returns a function for each node; the predicate then calls closures
  and never looks at the tree again. The `switch` on `n.Type()` runs when the filter is compiled, not for each record.
- **Missing fields.** A record without the field gives `nil`, and every comparison with `nil` is false, except `!=`.
  Decide that in `compare`; it is Go code, not grammar.
- **Reject bad filters early.** Everything the grammar can check is checked by `Compile`: a filter with a syntax
  error never becomes a predicate. Checks that need your schema (does the field exist? is `age` a number?) belong in
  `compile`, where you can return an error with the position of the node (`n.Start`).

## Variations

- **More operators**: `in` (a list operand), `contains`, arithmetic on operands, `is null`. Each is a level or an
  operand of the table.
- **Another target**: the same tree can be compiled to SQL, to a query for another system, or to a call of your own
  evaluator. Walk it once and write the output; [Evaluate arithmetic with variables and
  functions](calculator.md) walks it each time instead.
- **Messages for users.** Replace the expected list with a short message using `#error`, and show a caret under the
  position: [Report an error with its source line and a caret](error-carets.md).
- **Ready-made.** For the Common Expression Language (CEL), which is a complete expression language for policies and
  filters, use [parsers/cel](../../parsers/cel/).

See [Expressions](../guide/expressions.md) and [Pratt expressions](../../spec/pratt.md).
