# Overview

This chapter introduces the main ideas of the language and shows a complete
grammar. The chapters that follow define every part precisely; each part named
here links to its chapter.

## What a grammar describes

A **grammar** is a set of **rules**. Each rule has a name and a **parsing
expression** that matches input: literals, character classes, sequences,
ordered choices, repetitions, lookahead and calls of other rules, as in any
Parsing Expression Grammar. A parse starts at a **start rule** and has to match
the whole input (see [Parsing](parsing.md)).

PEGO extends PEG so that a grammar also says what the parse produces and how it
fails:

| A grammar says | By means of | Chapter |
|:--|:--|:--|
| How the text is structured | Rules, sequences, ordered choice, repetition, lookahead, [left recursion](parser-expressions.md#left-recursion) | [Parsing Expressions](parser-expressions.md) |
| How expressions with operators are parsed | `pratt` rules: operators declared by precedence and associativity | [Pratt Expressions](pratt.md) |
| What the tree looks like | A predictable concrete syntax tree by default; `@` and `-` to shape it; captures `label:a` | [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree) |
| What the types of the tree are | `type` definitions: struct, union and terminal types, checked when the grammar is compiled | [Type System](types.md), [Type Checking](type-checking.md) |
| How the value of a rule is built | Actions `-> expr` that build typed nodes from the captures | [Actions](actions.md) |
| What depends on context | Predicates `[expr]` that read and define variables | [Predicates and Variables](predicates.md) |
| How a parse reports and survives errors | `#error` messages, `#recover` | [Attributes](attributes.md), [Syntax errors](parsing.md#syntax-errors) |
| What may be handed over early | `#stream` repetitions | [Attributes](attributes.md#stream) |

A grammar file is written in the language that [Lexical Structure](lexical.md)
and [Grammar Files](grammar-files.md) define; [Syntax Summary](syntax.md)
collects its grammar in one place and the [Glossary](glossary.md) defines the
terms. The portable [bytecode](bytecode.md) that a grammar compiles to, and the
virtual machine that runs it, are specified for those who port the runtime.

## Example: a calculator

```pego
package calc

// --- Types ---
type Number terminal // a terminal type: the value is the matched text
type OpType terminal

type Op struct {
    Left  Node   // left operand
    Op    OpType // operator
    Right Node   // right operand
}

// A Node is either an Op or a Number.
type Node = Op | Number

// --- Rules ---

// Expression (addition and subtraction).
def expr: Node =
    // Capture the first term as l, and zero or more (operator, term) pairs as rest.
    l:term rest:(op:term_binary_op r:term)*
    // Fold the captured values from the left to build the tree.
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})

// Term (multiplication and division).
def term: Node =
    l:factor rest:(op:factor_binary_op r:factor)*
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})

def term_binary_op: OpType = @("+" / "-")
def factor_binary_op: OpType = @("*" / "/")

// Factor (a number or a parenthesized expression).
def factor: Node = group / number

def group: Node = "(" e:expr ")" -> $e

// A number is one or more digits.
def number: Number = @((?0-9)+)

// The whole input must be an expression.
def main = ^^ expr $$
```

For the input `1+2*3`, the value of `expr` is
`Op{Left: Number"1", Op: OpType"+", Right: Op{Left: Number"2", Op: OpType"*", Right: Number"3"}}`.
The value of `main` is a `Seq` node whose only child is that value, because the
anchors `^^` and `$$` have no value. With the grammar saved as `calc.pego`,
`pego parse -g calc.pego -f sexpr -i '1+2*3'` prints the tree in one line:

```
(Seq (Op Left=Number"1"@number Op=OpType"+"@term_binary_op Right=(Op Left=Number"2"@number Op=OpType"*"@factor_binary_op Right=Number"3"@number)))@main
```

## Reading the example

- `type Number terminal` defines a terminal type: a rule of that type produces a
  node that holds the matched text (see [Terminal types](types.md#terminal-types)).
  `Op` is a struct type with named fields, and `Node` is a union of two types.
- A rule such as `term` captures its parts with `label:expression` and builds the
  node in an action. `foldl` folds the repetition into a left-associative tree
  (see [Built-in functions](actions.md#built-in-functions)).
- `@(...)` matches an expression and keeps only its text, as one terminal (see
  [Atomic](parser-expressions.md#atomic)).
- `^^` and `$$` anchor the beginning and the end of the input. A rule without an
  action, such as `main`, produces a `Seq` node of the values of its elements
  that have a value (see
  [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree)).
