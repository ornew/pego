# Evaluate arithmetic with variables and functions

**Problem.** A program lets users type formulas such as `max(x, 2) * 3 ^ 2 - -y / 4`, with the usual precedence, and
evaluates them for many values of `x` and `y`.

Parse the formula once into a typed tree, then evaluate the tree as often as you like. A Pratt expression gives the
precedence and associativity in a table; the evaluator is a `switch` on the node type.

```pego
// calc.pego
type Num terminal
type Name terminal
type Bin struct { Left Expr, Op Match, Right Expr }
type Neg struct { X Expr }
type Call struct { Fn Name, Args []Expr }
type Expr = Num | Name | Bin | Neg | Call

def main: Expr = e:expr ws $$ -> $e

def expr: Expr = pratt {
    skip    ws
    operand num
    operand call
    operand name
    operand "(" e:expr ws ")" -> $e
    level { infix left  "+" / "-"       -> new Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left  "*" / "/" / "%" -> new Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level { prefix      "-"             -> new Neg{X: $rhs} }
    level { infix right "^"             -> new Bin{Left: $lhs, Op: $op, Right: $rhs} }
}

def call: Call = f:name ws "(" ws as:args? ws ")" -> new Call{Fn: $f, Args: concat($as)}
def args = first:expr rest:(ws "," ws e:expr)* -> concat(list($first), map($rest, (r) => $r.e))

def num: Num = (?0-9)+ ("." (?0-9)+)?
def name: Name = (?a-z_)+
def ws = (? \t\r\n)*
```

```go
// main.go
package main

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"strconv"

	"github.com/ornew/pego"
)

//go:embed calc.pego
var grammar string

// funcs are the functions an expression can call, with their number of arguments.
var funcs = map[string]struct {
	arity int
	f     func(a []float64) float64
}{
	"sqrt": {1, func(a []float64) float64 { return math.Sqrt(a[0]) }},
	"max":  {2, func(a []float64) float64 { return math.Max(a[0], a[1]) }},
	"min":  {2, func(a []float64) float64 { return math.Min(a[0], a[1]) }},
}

// eval evaluates a tree of the grammar with the given values of the variables.
func eval(n *pego.Node, vars map[string]float64) (float64, error) {
	switch n.Type() {
	case "Num":
		return strconv.ParseFloat(n.Text, 64)
	case "Name":
		v, ok := vars[n.Text]
		if !ok {
			return 0, fmt.Errorf("undefined variable %q", n.Text)
		}
		return v, nil
	case "Neg":
		x, err := eval(n.Field("X").(*pego.Node), vars)
		return -x, err
	case "Bin":
		l, err := eval(n.Field("Left").(*pego.Node), vars)
		if err != nil {
			return 0, err
		}
		r, err := eval(n.Field("Right").(*pego.Node), vars)
		if err != nil {
			return 0, err
		}
		switch op := n.Field("Op").(*pego.Node).Text; op {
		case "+":
			return l + r, nil
		case "-":
			return l - r, nil
		case "*":
			return l * r, nil
		case "/":
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return l / r, nil
		case "%":
			return math.Mod(l, r), nil
		default: // "^"
			return math.Pow(l, r), nil
		}
	case "Call":
		name := n.Field("Fn").(*pego.Node).Text
		fn, ok := funcs[name]
		if !ok {
			return 0, fmt.Errorf("undefined function %q", name)
		}
		argNodes := n.Field("Args").(*pego.Node).Children
		if len(argNodes) != fn.arity {
			return 0, fmt.Errorf("%s takes %d arguments, got %d", name, fn.arity, len(argNodes))
		}
		args := make([]float64, len(argNodes))
		for i, a := range argNodes {
			v, err := eval(a, vars)
			if err != nil {
				return 0, err
			}
			args[i] = v
		}
		return fn.f(args), nil
	}
	return 0, fmt.Errorf("unexpected %s node", n.Type())
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	// Parse once, evaluate many times.
	tree, err := p.Parse("max(x, 2) * 3 ^ 2 ^ 2 - -y / 4")
	if err != nil {
		log.Fatal(err)
	}
	for _, x := range []float64{1, 5, 10} {
		v, err := eval(tree, map[string]float64{"x": x, "y": 2})
		fmt.Println("x =", x, "->", v, err)
	}

	for _, src := range []string{"1 +", "sqrt(1, 2)", "z * 2", "1 / (2 - 2)", "2 ^ 3 ^ 2", "(1 + 2) * 3 % 4"} {
		tree, err := p.Parse(src)
		if err != nil {
			fmt.Printf("%-16s syntax error: %v\n", src, err)
			continue
		}
		v, err := eval(tree, nil)
		fmt.Printf("%-16s %v %v\n", src, v, err)
	}
}
```

```text
x = 1 -> 162.5 <nil>
x = 5 -> 405.5 <nil>
x = 10 -> 810.5 <nil>
1 +              syntax error: 1:4: syntax error: expected "(", "-", (?0-9), (?a-z_)
sqrt(1, 2)       0 sqrt takes 1 arguments, got 2
z * 2            0 undefined variable "z"
1 / (2 - 2)      0 division by zero
2 ^ 3 ^ 2        512 <nil>
(1 + 2) * 3 % 4  1 <nil>
```

## How it works

- **The table is the precedence.** `level` blocks run from the loosest to the tightest, so `+ -` bind loosest, then `* / %`,
  then the prefix minus, then `^`. `infix right` makes `^` associative to the right (`2 ^ 3 ^ 2` is `2 ^ 9`, 512), and
  `infix left` the others (`(1 + 2) * 3 % 4` is `9 % 4`). `-y / 4` is `(-y) / 4`, because the prefix minus binds tighter
  than `/` and looser than `^`.
- **`skip ws`** lets white space appear between the tokens of the expression, so the operators and operands need no
  `ws` of their own. A rule called from an operand, such as `call`, handles its own white space.
- **Order of operands.** `call` comes before `name`: both start with a name, and a Pratt expression tries the operands
  in order, like a choice.
- **Typed nodes.** `Bin` has an operator and two `Expr`s, and the checker rejects a rule that builds one wrongly, when the
  grammar compiles. `Expr` is a union, so `eval` can switch on `n.Type()` and reach each field with `Field`.
- **Parse once.** The cost of parsing is paid once for `tree`; each evaluation only walks it. If you compile the
  formula into closures instead, as [Compile a filter language into a Go predicate](filter-language.md) does, each
  evaluation does not even dispatch on the node type.
- **Errors.** A formula that does not parse is a `*pego.SyntaxError` with its position. Errors that depend on the
  values (undefined variable, division by zero) are found while evaluating; wrong arity and undefined functions could
  also be found in a pass over the tree before any evaluation.

## Variations

- **Integer arithmetic** (or `big.Int`): change the `Num` conversion and the operators in `eval`.
- **Comparison and logic**: add levels, loosest first. The [filter language](filter-language.md) does.
- **Assignments and statements**: put the Pratt expression inside a rule of a statement grammar; the minilang example
  of [examples/](../../examples/README.md) has expressions in a programming language.
- **A grammar without `pratt`**: a rule per precedence level, or a left-recursive rule per level, gives the same trees;
  [examples/calculator](../../examples/calculator/) has both.

See [Expressions](../guide/expressions.md) for operator tables, associativity and level-restricted calls, and
[Pratt expressions](../../spec/pratt.md) for the rules.
