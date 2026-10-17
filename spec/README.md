# The PEGO Language Specification

PEGO is a parser framework built around a grammar language that extends PEG
(Parsing Expression Grammar); PEGO is not plain PEG. In addition to describing
syntax, a PEGO grammar specifies how a typed abstract syntax tree is built, how
context-sensitive constructs are recognized, and how the parser reports and
recovers from syntax errors.

This document is the reference for supported grammar-language behavior. It
describes the syntax and meaning of `.pego` grammar files and the results of
parsing input with them. Some source forms are accepted into the source AST for
formatting but are rejected by compilation; the syntax chapter identifies these
forms and does not imply that they execute. For how to use the language, see the
[guides](../docs/guide/README.md); for a first tour, see the
[tutorial](../docs/tutorial/getting-started.md).

> Behavior described as supported in this specification is implemented. Some
> source forms may be parsed and formatted while compilation or execution remains
> unsupported; those boundaries are stated in the relevant chapter. The
> implementation status is tracked in
> [docs/development.md](../docs/development.md#language-feature-status). Features
> proposed but not yet implemented are marked "(not implemented)".

## Contents

The chapters are in reading order: each builds on the ones before it.

| | Chapter | Defines |
|--:|:--|:--|
| 1 | [Overview](overview.md) | The main ideas of the language and a complete example grammar |
| 2 | [Lexical Structure](lexical.md) | Characters, white space, `//` and nested `/* ... */` comments, identifiers, keywords, literals, escape sequences, punctuation and adjacency |
| 3 | [Grammar Files](grammar-files.md) | The optional package clause, Type and Rule Definition Statements, names, source metadata and grammar representations |
| 4 | [Type System](types.md) | Built-in and reserved types, struct, union and terminal types, names of types, assignability |
| 5 | [Rule Expressions](parser-expressions.md) | The expressions that match input, the concrete syntax tree they build, captures, cut, rule calls and left recursion |
| 6 | [Operators Rule](operators-rule.md) | Operator expressions declared by precedence and associativity |
| 7 | [Predicates and Variables](predicates.md) | Predicates, variables and their scope, and context-sensitive parsing |
| 8 | [Actions](actions.md) | Building the value of a rule: captures, constructors, operators and built-in functions |
| 9 | [Type Checking](type-checking.md) | The type of each expression, type inference and the errors that the checker reports |
| 10 | [Attributes](attributes.md) | Diagnostic annotations and argument syntax, including `@error` |
| 11 | [Pragmas](pragmas.md) | Behavioral and structural annotations: `#recover`, `#repeat`, `#stream`, `#atomic`, and `#cut` |
| 12 | [Parsing](parsing.md) | A parse as a whole: the start rule, input and positions, results and nodes, syntax errors, memoization, limits and modes |
| 13 | [Grammar Syntax](syntax.md) | A self-hosted syntax projection tested against the reference parser |
| 14 | [Glossary](glossary.md) | The terms of this specification, with the place where each is defined |

The self-hosted grammar syntax chapter is a projection of the source parser, not
the public grammar AST contract. Its typed source tree is an implementation
representation; the public `grammar.Grammar` AST and its JSON representation
are defined in [Grammar Files](grammar-files.md#grammar-ast-source-metadata).

The portable bytecode that a grammar compiles to and the virtual machine that
runs it are specified separately, for those who port the runtime:

| Document | Defines |
|:--|:--|
| [PEGO Bytecode Specification](bytecode.md) | The module (tables and instructions), the VM state, the instruction set, rule calls, left recursion, Operators Rule parsing, expression code and the `.pegoc` file format |

## Conventions

The key words MUST, MUST NOT, SHOULD, SHOULD NOT and MAY are to be interpreted
as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119). They are
used only where a requirement is normative; most of this specification states
the behavior of the language in plain declarative sentences.

Grammar examples are written in ` ```pego ` code blocks. Output of the `pego`
command that an example shows is the output of running that command. Syntax summaries use
angle brackets for placeholders (`<name>`), square brackets for optional parts
(`[<type>]`) and `|` for alternatives.

Unless stated otherwise, an error described in this specification is reported
when the grammar is compiled, together with its line and column in the grammar
source.
