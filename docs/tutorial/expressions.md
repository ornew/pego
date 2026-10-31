# Parsing Expressions

Arithmetic is where PEG grammars usually get awkward: operator precedence and associativity normally require one rule
per level. This lesson builds a calculator grammar with the two tools ZeroParse offers, an Operators Rule and left recursion.

## What You Will Build

A grammar that parses `1 + 2 * 3 - 4` into a tree that respects precedence and associativity, written two ways.

## Before You Start

Complete [Building Typed Trees](typed-trees.md). This lesson does not use the configuration files.

## An Operators Rule

Start with an Operators Rule. Save as `calc.zp`:

```zp
type Number = node[string]
type Binary = { Left Expr; Op node[string]; Right Expr }
type Expr ~ Number | Binary

@start
rule main Expr = e:expr $$ => $e

rule expr Expr = #operators {
    operand number
    level additive { infix left "+" / "-" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

rule number Number = ([0-9]+) #atomic
```

An Operators Rule Declaration Statement defines an expression by listing its
parts:

- `operand` is something that can stand on its own, here a number.
- `level name { ... }` declares operators that share a precedence. `infix left "+" / "-"` is a binary operator whose
  alternatives are `+` and `-`, grouping to the left. The action receives `$lhs` and `$rhs`, the operands, and `$op`,
  the operator text.

```bash
zp parse -g calc.zp -f sexpr -i '1+2-3'
```

```text
Binary{Left: Binary{Left: Number("1"), Op: "+", Right: Number("2")}, Op: "-", Right: Number("3")}
```

`1+2-3` is `(1+2)-3`, as left associativity demands. But whitespace is not handled yet:

```bash
zp parse -g calc.zp -f sexpr -i '1 + 2'
```

```text
zp: 1:2: syntax error: expected end of input, "+", "-", [0-9]
```

Add a `skip` item, which ZeroParse inserts before every operand and operator, and fill out the language: more levels,
parentheses, a prefix operator and a right-associative one. **Levels declared earlier bind more loosely**, so the order
of the `level` blocks is the precedence table, from loosest to tightest:

```zp
type Number = node[string]
type Binary = { Left Expr; Op node[string]; Right Expr }
type Unary = { Op node[string]; X Expr }
type Expr ~ Number | Binary | Unary

@start
rule main Expr = e:expr ws $$ => $e

rule expr Expr = #operators {
    skip ws
    operand number
    operand "(" e:expr ws ")" => $e
    level additive { infix left "+" / "-" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level multiplicative { infix left "*" / "/" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level prefix { prefix "-" => Unary{Op: $op, X: $rhs} }
    level power { infix right "^" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

rule number Number = ([0-9]+ ("." [0-9]+)?) #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g calc.zp -f sexpr -i '1+2*3'
```

```text
Binary{Left: Number("1"), Op: "+", Right: Binary{Left: Number("2"), Op: "*", Right: Number("3")}}
```

Multiplication binds tighter than addition. Some more inputs:

| Input | Result |
|:--|:--|
| `1 - 2 - 3` | `Binary{Left: Binary{Left: Number("1"), Op: "-", Right: Number("2")}, Op: "-", Right: Number("3")}` |
| `2^3^2` | `Binary{Left: Number("2"), Op: "^", Right: Binary{Left: Number("3"), Op: "^", Right: Number("2")}}` |
| `-2^2` | `Unary{Op: "-", X: Binary{Left: Number("2"), Op: "^", Right: Number("2")}}` |
| `(1+2)*3` | `Binary{Left: Binary{Left: Number("1"), Op: "+", Right: Number("2")}, Op: "*", Right: Number("3")}` |
| `1.5*2` | `Binary{Left: Number("1.5"), Op: "*", Right: Number("2")}` |

`1 - 2 - 3` groups to the left, `2^3^2` is `2^(3^2)` because `^` is `infix right`, and `-2^2` is `-(2^2)` because `^`
is declared after (so binds tighter than) the prefix `-`. A parenthesized operand returns `$e`, the inner expression,
so no node is created for the parentheses.

The `skip` item is a convenience of Operators Rule syntax only. It is not inserted *inside* an operator or operand, which is
why the parenthesized operand writes `ws` before `")"` itself, and why `main` has a trailing `ws`.

### The Same Language With Left Recursion

A rule that starts by calling itself is **left recursive**, and ZeroParse accepts it. Precedence then comes from having one
rule per level, each defined in terms of the next tighter one:

```zp
type Number = node[string]
type Binary = { Left Expr; Op node[string]; Right Expr }
type Expr ~ Number | Binary

@start
rule main Expr = ws e:expr ws $$ => $e

rule expr Expr = sum / product
rule sum Expr = l:expr ws op:("+" / "-") #atomic ws r:product => Binary{Left: $l, Op: $op, Right: $r}

rule product Expr = mul / atom
rule mul Expr = l:product ws op:("*" / "/") #atomic ws r:atom => Binary{Left: $l, Op: $op, Right: $r}

rule atom Expr = number / group
rule group Expr = "(" ws e:expr ws ")" => $e
rule number Number = ([0-9]+) #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g calc-lr.zp -f sexpr -i '1 + 2 * 3 - 4'
```

```text
Binary{Left: Binary{Left: Number("1"), Op: "+", Right: Binary{Left: Number("2"), Op: "*", Right: Number("3")}}, Op: "-", Right: "4"}
```

This produces the same trees as the Operators Rule version, with one rule per level instead of one line per level. Operators Rule
expressions scale better as the language grows (the [Go](../../parsers/golang/) and
[Python](../../parsers/python/) parsers use them for their full operator tables), and they support features such as
level-restricted calls and mixed prefix, infix and postfix operators. The guide to
[Operators Rule](../guide/operators-rule.md) compares the two approaches and covers every option;
[examples/calculator](../../examples/calculator/) contains both versions of a complete calculator with an evaluator.

## Next Steps

[Reporting Errors](errors.md) shows what the parser says when input is wrong. The guide
[Operators Rule](../guide/operators-rule.md) covers every operator form, and the [Calculator
recipe](../cookbook/calculator.md) adds an evaluator.
