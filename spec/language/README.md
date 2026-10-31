# ZeroParse Grammar Language Specification

ZeroParse is a parser framework built around a grammar language that extends PEG
(Parsing Expression Grammar); ZeroParse is not plain PEG. In addition to describing
syntax, a ZeroParse grammar specifies how a typed abstract syntax tree is built, how
context-sensitive constructs are recognized, and how the parser reports and
recovers from syntax errors.

This specification is the reference for the syntax and meaning of `.zp`
grammar files: their characters and tokens, types, rule expressions, actions,
annotations and the checks that the compiler applies. How a grammar runs on
input, and the results it produces, are defined by the
[Parser and Runtime Specification](../runtime/README.md). Some source forms are
accepted into the source AST for formatting but are rejected by compilation; the
syntax chapter identifies these forms and does not imply that they execute.

> Behavior described as supported in this specification is implemented. Some
> source forms may be parsed and formatted while compilation or execution remains
> unsupported; those boundaries are stated in the relevant chapter. Features
> proposed but not yet implemented are marked "(not implemented)".

## Contents

The chapters are in three groups, in reading order: each builds on the ones before it.

### Language Basics

The ideas of the language, its characters and tokens, and the shape of a grammar file.

| | Chapter | Defines |
|--:|:--|:--|
| 1 | [Overview](overview.md) | The main ideas of the language and the chapter that defines each |
| 2 | [Lexical Structure](lexical.md) | Characters, white space, `//` and nested `/* ... */` comments, identifiers, keywords, literals, escape sequences, punctuation and adjacency |
| 3 | [Grammar Files](grammar-files.md) | The optional package clause, Type and Rule Declaration Statements, names, source metadata and grammar representations |

### Types and Expressions

What a rule matches and builds: types, expressions, operators, context and the checks that tie them together.

| | Chapter | Defines |
|--:|:--|:--|
| 4 | [Type System](types.md) | Built-in types, node type names, type declarations, struct and join types, names of types, assignability |
| 5 | [Rule Expressions](parser-expressions.md) | The expressions that match input, the values they build, captures, cut, rule calls and left recursion |
| 6 | [Operators Rule](operators-rule.md) | Operator expressions declared by precedence and associativity |
| 7 | [Predicates and Variables](predicates.md) | Predicates, variables and their scope, and context-sensitive parsing |
| 8 | [Actions](actions.md) | Building the value of a rule: captures, constructors, operators and built-in functions |
| 9 | [Type Checking](type-checking.md) | The type of each expression, type inference and the errors that the checker reports |

### Annotations and Syntax

The annotations that adjust rules and the self-hosted syntax of the language.

| | Chapter | Defines |
|--:|:--|:--|
| 10 | [Attributes](attributes.md) | Diagnostic annotations and argument syntax, including `@error` |
| 11 | [Pragmas](pragmas.md) | Behavioral and structural annotations: `#recover`, `#repeat`, `#stream`, `#atomic`, and `#cut` |
| 12 | [Grammar Syntax](syntax.md) | A self-hosted syntax projection tested against the reference parser |

The [Glossary](../glossary.md) lists the terms of the specifications, with the place
where each is defined.

The self-hosted grammar syntax chapter is a projection of the source parser, not
the public grammar AST contract. Its typed source tree is an implementation
representation; the public `grammar.Grammar` AST and its JSON representation
are defined in [Grammar Files](grammar-files.md#grammar-ast-source-metadata).

## Conventions

The conventions of the [specifications](../README.md#conventions) apply. Unless
stated otherwise, an error described in this specification is reported when the
grammar is compiled, together with its line and column in the grammar source.
