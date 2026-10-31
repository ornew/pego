# Overview

This chapter introduces the main ideas of the language. The chapters that follow
define every part precisely; each part named here links to its chapter.

## What a Grammar Describes

A grammar file contains an optional package clause followed by Type and Rule
Declaration Statements. Type definitions use Type Expressions; ordinary Rule
Declaration Statements combine a Rule Expression with an optional Value Action
containing an Action Expression. An Operators Rule is a specialized Rule
Declaration Statement. Rule Expressions use PEG choice and repetition, along with
ZeroParse-specific constructs. A parse starts at a **start rule** and has to match
the whole input (see [Parsing](../runtime/parsing.md)).

ZeroParse extends PEG (Parsing Expression Grammar), but it is not plain PEG: the
language adds typed values, actions, contextual recognition, and error recovery:

| A grammar says | By means of | Chapter |
|:--|:--|:--|
| How the text is structured | Rules, sequences, ordered choice, repetition, lookahead, [left recursion](parser-expressions.md#left-recursion) | [Rule Expressions](parser-expressions.md) |
| How expressions with operators are parsed | Operators Rule declarations: operators grouped by precedence and associativity | [Operators Rule](operators-rule.md) |
| What the result looks like | Values that follow the shape of the rule by default: strings, tuples for sequences, lists for repetitions; `#atomic` and `-` to shape them; captures `label:a` to name elements | [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree), [Nodes and Values](../runtime/nodes.md) |
| What the types of the values are | `type` definitions: struct and join types, checked when the grammar is compiled | [Type System](types.md), [Type Checking](type-checking.md) |
| How the value of a rule is built | Value Actions `=> expr` inside rule bodies build structs, lists and other values from captures | [Actions](actions.md) |
| What depends on context | Predicate Actions `{expr}` that read and define variables | [Predicates and Variables](predicates.md) |
| How a parse reports and survives errors | `@error` messages, `#recover` | [Attributes](attributes.md), [Pragmas](pragmas.md), [Syntax errors](../runtime/parsing.md#syntax-errors) |
| What may be handed over early | `#stream` repetitions | [Pragmas](pragmas.md#stream) |

A grammar file is written in the language that [Lexical Structure](lexical.md)
and [Grammar Files](grammar-files.md) define; [Grammar Syntax](syntax.md)
collects its grammar in one place and the [Glossary](../glossary.md) defines the
terms. The portable [bytecode](../bytecode/README.md) that a grammar compiles to, and the
virtual machine that runs it, are specified for those who port the runtime.
