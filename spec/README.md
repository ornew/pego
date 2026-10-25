# The PEGO Specifications

PEGO is a parser framework built around a grammar language that extends PEG
(Parsing Expression Grammar). Its behavior is defined by several specifications,
each with its own contents and chapter numbering. Together they describe the
syntax and meaning of `.pego` grammar files, the results of parsing input with
them, the contracts of the runtimes that execute them, and the internal
representations that the engine builds from a grammar. A specification may link
to another where it relies on it; apart from those links and the publications of
the standards that they cite (RFC 2119, for the key words of the conventions),
they link to nothing else.

## Specifications

| Specification | Defines |
|:--|:--|
| [Grammar Language](language/README.md) | The syntax and meaning of `.pego` grammar files: lexical structure, types, rule expressions, Operators Rules, predicates, actions, attributes, pragmas and the checks the compiler applies |
| [Parser and Runtime](runtime/README.md) | Parsing input with a grammar: positions, results, errors, the node model, Documents, streaming, tracing and profiling, compiled grammars, generated parsers, output schemas and the linter |
| [Core IR](core-ir/README.md) | The intermediate representation of a checked grammar: its modules, nodes, terms, effects, invariants and text form |
| [Bytecode](bytecode/README.md) | The portable bytecode that a grammar compiles to, the virtual machine that runs it and the `.pegoc` file format |

The [Glossary](glossary.md) lists the terms of all the specifications, with the place where each is defined.

## Conventions

The key words MUST, MUST NOT, SHOULD, SHOULD NOT and MAY are to be interpreted
as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119). They are
used only where a requirement is normative; most of the specifications state
the behavior of the language in plain declarative sentences.

Grammar examples are written in ` ```pego ` code blocks. Output of the `pego`
command that an example shows is the output of running that command. Syntax summaries use
angle brackets for placeholders (`<name>`), square brackets for optional parts
(`[<type>]`) and `|` for alternatives.
