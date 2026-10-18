# PEGO Tutorial

A series of lessons that builds one project, a parser for a small configuration language, from a first grammar to a
shipped, tested and debugged parser. Each lesson ends with a working result and starts from the files of the lessons
before it, so read them in order the first time.

Every lesson has the same shape: what you will build, what you need first, the steps with verified output, and where to
go next. The [guides](../guide/README.md) explain each feature in depth and the [cookbook](../cookbook/README.md)
solves single tasks.

| Lesson | You will learn |
|:--|:--|
| [Getting Started With PEGO](getting-started.md) | Installing `pego`, writing a first grammar, parsing from the command line and from Go |
| [Parsing Structured Input](structured-input.md) | Rules, ordered choice, repetition and lookahead, for a configuration file format |
| [Building Typed Trees](typed-trees.md) | Naming and shaping the tree, struct and terminal types, reading it in Go |
| [Parsing Expressions](expressions.md) | Operators and precedence with the Operators Rule, building a calculator |
| [Reporting Errors](errors.md) | Reading syntax errors, writing clear messages, recovering and collecting several errors |
| [Parsing Context-Sensitive Input](context.md) | Predicates and variables for matching tags and limiting lengths |
| [Testing and Linting a Grammar](testing-and-linting.md) | Fixture suites, the linter and the formatter |
| [Shipping a Parser](shipping.md) | Generating Go code, compiled grammars and backends |
| [Handling Big and Live Input](big-and-live-input.md) | Streaming input, reparsing after edits |
| [Working With the Tooling](tooling.md) | Explaining, tracing and profiling a grammar, sampling inputs, the editor and the playground |

## Requirements

Go 1.27.1 or later. The lessons use the `pego` command, which [Getting Started With
PEGO](getting-started.md#install) shows how to install.
