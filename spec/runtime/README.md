# PEGO Parser and Runtime Specification

This specification defines what happens when a compiled grammar meets input: the
start rule, positions, results, syntax errors, the node and value model,
incremental documents and streaming, and the tools around a running parser:
tracing, compiled grammars, generated parsers, output schemas and the linter. The
grammars themselves are defined by the
[Grammar Language Specification](../language/README.md); the portable
instruction set that a grammar compiles to is defined by the
[Bytecode Specification](../bytecode/README.md).

> Behavior described as supported in this specification is implemented. Features
> proposed but not yet implemented are marked "(not implemented)".

## Contents

The chapters are in two groups, in reading order: each builds on the ones before it.

### Parsing and Results

Running a grammar on input and the results it produces, for whole texts, edited documents and streams.

| | Chapter | Defines |
|--:|:--|:--|
| 1 | [Parsing](parsing.md) | A parse as a whole: the start rule, input and positions, results, syntax errors, memoization, prefix parsing, limits and modes |
| 2 | [Nodes and Values](nodes.md) | The value and node model, source information, the Go `Node` type, traversal, and the text and JSON renderings |
| 3 | [Incremental Documents](documents.md) | Editing and reparsing text: versions, snapshots, interruption and change notifications |
| 4 | [Streaming](streaming.md) | Parsing a reader and delivering the elements of a `#stream` repetition as they match |

### Tools and Formats

Observing a parse, saving and generating parsers, describing their output and checking grammars.

| | Chapter | Defines |
|--:|:--|:--|
| 5 | [Tracing and Profiling](observability.md) | Rule tracing and profiling of the engine and of generated parsers |
| 6 | [Compiled Grammars](compiled-grammars.md) | Saving and loading `.pegoc` files, their modes and the operations each serves, versions and loading checks |
| 7 | [Generated Parsers](generated-parsers.md) | `pego gen`: the Go and TypeScript parsers, their APIs and what each supports |
| 8 | [Output Schemas and Structural Queries](schemas-and-queries.md) | Describing the output of a grammar and searching result nodes (experimental) |
| 9 | [Grammar Lint](lint.md) | The checks of the linter, severities and suppression directives |

The [Glossary](../glossary.md) lists the terms of the specifications, with the place
where each is defined.

## Conventions

The conventions of the [specifications](../README.md#conventions) apply.
