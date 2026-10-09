# The PEGO Language Specification

PEGO is a parser framework built around a grammar language that extends
Parsing Expression Grammars (PEG). In addition to describing syntax, a PEGO
grammar specifies, at the language level, how a typed abstract syntax tree is
built, how context-sensitive constructs are recognized, and how the parser
reports and recovers from syntax errors.

This document is the reference for the grammar language. It describes the
syntax and meaning of `.pego` grammar files and the results of parsing input
with them.

> Every feature described in this specification is implemented. The
> implementation status is tracked in
> [docs/development.md](../docs/development.md#language-feature-status). If a feature
> that is not yet implemented is added to this specification, its heading will
> be marked "(not implemented)".

## Contents

1. [Overview and Grammar Files](overview.md)
2. [Type System](types.md)
3. [Parsing Expressions](parser-expressions.md)
4. [Pratt Expressions](pratt.md)
5. [Predicates and Variables](predicates.md)
6. [Actions](actions.md)
7. [Type Checking](type-checking.md)
8. [Attributes](attributes.md)
9. [PEGO Bytecode Specification](bytecode.md)

## Conventions

The key words MUST, MUST NOT, SHOULD, SHOULD NOT and MAY are to be interpreted
as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119). They are
used only where a requirement is normative; most of this specification states
the behavior of the language in plain declarative sentences.

Grammar examples are written in ` ```pego ` code blocks. Syntax summaries use
angle brackets for placeholders (`<name>`), square brackets for optional parts
(`[: <type>]`) and `|` for alternatives.

Unless stated otherwise, an error described in this specification is reported
when the grammar is compiled, together with its line and column in the grammar
source.
