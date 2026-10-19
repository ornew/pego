# The PEGO Language Specification

PEGO is a parser framework built around a grammar language that extends PEG
(Parsing Expression Grammar); PEGO is not plain PEG. In addition to describing
syntax, a PEGO grammar specifies how a typed abstract syntax tree is built, how
context-sensitive constructs are recognized, and how the parser reports and
recovers from syntax errors.

This document is the reference for supported grammar-language behavior. It
describes the syntax and meaning of `.pego` grammar files, the results of parsing
input with them, and the contracts of the runtimes that execute them. Some source
forms are accepted into the source AST for formatting but are rejected by
compilation; the syntax chapter identifies these forms and does not imply that
they execute. The specification is self-contained: every chapter links only to
other chapters of the specification, apart from the publications of the
standards that it cites (RFC 2119, for the key words of the conventions).

> Behavior described as supported in this specification is implemented. Some
> source forms may be parsed and formatted while compilation or execution remains
> unsupported; those boundaries are stated in the relevant chapter. Features
> proposed but not yet implemented are marked "(not implemented)".

## Contents

The chapters are in three parts, in reading order: each builds on the ones before it.

### Part 1: The Language

| | Chapter | Defines |
|--:|:--|:--|
| 1 | [Overview](overview.md) | The main ideas of the language and the chapter that defines each |
| 2 | [Lexical Structure](lexical.md) | Characters, white space, `//` and nested `/* ... */` comments, identifiers, keywords, literals, escape sequences, punctuation and adjacency |
| 3 | [Grammar Files](grammar-files.md) | The optional package clause, Type and Rule Definition Statements, names, source metadata and grammar representations |
| 4 | [Type System](types.md) | Built-in types, node type names, struct and union types, text structs, names of types, assignability |
| 5 | [Rule Expressions](parser-expressions.md) | The expressions that match input, the values they build, captures, cut, rule calls and left recursion |
| 6 | [Operators Rule](operators-rule.md) | Operator expressions declared by precedence and associativity |
| 7 | [Predicates and Variables](predicates.md) | Predicates, variables and their scope, and context-sensitive parsing |
| 8 | [Actions](actions.md) | Building the value of a rule: captures, constructors, operators and built-in functions |
| 9 | [Type Checking](type-checking.md) | The type of each expression, type inference and the errors that the checker reports |
| 10 | [Attributes](attributes.md) | Diagnostic annotations and argument syntax, including `@error` |
| 11 | [Pragmas](pragmas.md) | Behavioral and structural annotations: `#recover`, `#repeat`, `#stream`, `#atomic`, and `#cut` |
| 12 | [Grammar Syntax](syntax.md) | A self-hosted syntax projection tested against the reference parser |

### Part 2: Parsing and Runtime Contracts

| | Chapter | Defines |
|--:|:--|:--|
| 13 | [Parsing](parsing.md) | A parse as a whole: the start rule, input and positions, results, syntax errors, memoization, prefix parsing, limits and modes |
| 14 | [Nodes and Values](nodes.md) | The value and node model, source information, the Go `Node` type, traversal, and the text and JSON renderings |
| 15 | [Incremental Documents](documents.md) | Editing and reparsing text: versions, snapshots, interruption and change notifications |
| 16 | [Streaming](streaming.md) | Parsing a reader and delivering the elements of a `#stream` repetition as they match |
| 17 | [Observability](observability.md) | Rule tracing and profiling of the engine and of generated parsers |
| 18 | [Compiled Grammars](compiled-grammars.md) | Saving and loading `.pegoc` files, with and without the AST, versions and loading checks |
| 19 | [Generated Parsers](generated-parsers.md) | `pego gen`: the Go and TypeScript parsers, their APIs and what each supports |
| 20 | [Output Schemas and Structural Queries](schemas-and-queries.md) | Describing the output of a grammar and searching result nodes (experimental) |
| 21 | [Grammar Lint](lint.md) | The checks of the linter, severities and suppression directives |

### Part 3: The Portable Virtual Machine

The portable bytecode that a grammar compiles to and the virtual machine that
runs it are specified separately, for those who port the runtime.

| | Chapter | Defines |
|--:|:--|:--|
| 22 | [PEGO Bytecode Specification](bytecode.md) | The module (tables and instructions), the VM state, the instruction set, rule calls, left recursion, Operators Rule parsing, expression code and the `.pegoc` file format |

The [Glossary](glossary.md) lists the terms of this specification, with the place
where each is defined.

The self-hosted grammar syntax chapter is a projection of the source parser, not
the public grammar AST contract. Its typed source tree is an implementation
representation; the public `grammar.Grammar` AST and its JSON representation
are defined in [Grammar Files](grammar-files.md#grammar-ast-source-metadata).

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
