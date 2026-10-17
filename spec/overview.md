# Overview

This chapter introduces the main ideas of the language and shows a complete
grammar. The chapters that follow define every part precisely; each part named
here links to its chapter.

## What a Grammar Describes

A grammar file contains an optional package clause followed by Type and Rule
Definition Statements. Type definitions use Type Expressions; ordinary Rule
Definition Statements combine a Rule Expression with an optional Value Action
containing an Action Expression. An Operators Rule is a specialized Rule
Definition Statement. Rule Expressions use PEG choice and repetition, along with
PEGO-specific constructs. A parse starts at a **start rule** and has to match
the whole input (see [Parsing](parsing.md)).

PEGO extends PEG (Parsing Expression Grammar), but it is not plain PEG: the
language adds typed values, actions, contextual recognition, and error recovery:

| A grammar says | By means of | Chapter |
|:--|:--|:--|
| How the text is structured | Rules, sequences, ordered choice, repetition, lookahead, [left recursion](parser-expressions.md#left-recursion) | [Rule Expressions](parser-expressions.md) |
| How expressions with operators are parsed | Operators Rule declarations: operators grouped by precedence and associativity | [Operators Rule](operators-rule.md) |
| What the tree looks like | A predictable concrete syntax tree by default; `#atomic` and `-` to shape it; captures `label:a` | [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree) |
| What the types of the tree are | `type` definitions: struct, union and terminal types, checked when the grammar is compiled | [Type System](types.md), [Type Checking](type-checking.md) |
| How the value of a rule is built | Value Actions `=> expr` inside rule bodies build typed nodes from captures | [Actions](actions.md) |
| What depends on context | Predicate Actions `{expr}` that read and define variables | [Predicates and Variables](predicates.md) |
| How a parse reports and survives errors | `@error` messages, `#recover` | [Attributes](attributes.md), [Pragmas](pragmas.md), [Syntax errors](parsing.md#syntax-errors) |
| What may be handed over early | `#stream` repetitions | [Pragmas](pragmas.md#stream) |

A grammar file is written in the language that [Lexical Structure](lexical.md)
and [Grammar Files](grammar-files.md) define; [Grammar Syntax](syntax.md)
collects its grammar in one place and the [Glossary](glossary.md) defines the
terms. The portable [bytecode](bytecode.md) that a grammar compiles to, and the
virtual machine that runs it, are specified for those who port the runtime.

## Example: A Calculator

```pego
package calc

// --- Types ---
type Number terminal // a terminal type: the value is the matched text
type OpType terminal

type Op = {
    Left  Node   // left operand
    Op    OpType // operator
    Right Node   // right operand
}

// Node is either an Op or a Number.
type Node ~ Op | Number

// --- Rules ---

// Expression (addition and subtraction).
rule expr Node = (
    // Capture the first term as l, and zero or more (operator, term) pairs as rest.
    l:term rest:(op:term_binary_op r:term)*
    // Fold the captured values from the left to build the tree.
) => foldl($l, $rest, (acc, i) => Op{Left: $acc, Op: $i.op, Right: $i.r})

// Term (multiplication and division).
rule term Node = (
    l:factor rest:(op:factor_binary_op r:factor)*
) => foldl($l, $rest, (acc, i) => Op{Left: $acc, Op: $i.op, Right: $i.r})

rule term_binary_op OpType = ("+" / "-") #atomic
rule factor_binary_op OpType = ("*" / "/") #atomic

// Factor (a number or a parenthesized expression).
rule factor Node = group / number

rule group Node = "(" e:expr ")" => $e

// A number is one or more digits.
rule number Number = [0-9]+ #atomic

// The whole input must be an expression.
rule main = ^^ expr $$
```

For the input `1+2*3`, the value of `expr` is
`Op{Left: Number"1", Op: OpType"+", Right: Op{Left: Number"2", Op: OpType"*", Right: Number"3"}}`.
The value of `main` is a `Seq` node whose only child is that value, because the
anchors `^^` and `$$` have no value. With the grammar saved as `calc.pego`,
`pego parse -g calc.pego -f sexpr -i '1+2*3'` prints the tree in one line:

```
(Seq (Op Left=Number"1"@number Op=OpType"+"@term_binary_op Right=(Op Left=Number"2"@number Op=OpType"*"@factor_binary_op Right=Number"3"@number)))@main
```

## Reading the Example

- `type Number terminal` defines a terminal type: a rule of that type produces a
  node that holds the matched text (see [Terminal types](types.md#terminal-types)).
  `Op` is a struct type with named fields, and `Node` is a union of two types.
- A rule such as `term` captures its parts with `label:expression` and builds the
  node in an action. `foldl` folds the repetition into a left-associative tree
  (see [Built-in functions](actions.md#operators-and-functions)).
- `(...) #atomic` matches an expression and keeps only its text, as one terminal (see
  [Atomic](parser-expressions.md#atomic)).
- `^^` and `$$` anchor the beginning and the end of the input. A rule without an
  action, such as `main`, produces a `Seq` node of the values of its elements
  that have a value (see
  [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree)).
