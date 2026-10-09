# PEGO Cookbook

Short recipes for concrete problems. Each one is a complete solution: the problem in a sentence or two, the grammar and
the Go program, the output they print, and a few lines on how it works and what to change. They differ from the other
documents in how they are organized:

- The [tutorial](../tutorial/getting-started.md) teaches the language from the first rule on.
- The [guides](../guide/README.md) explain one feature at a time, in depth.
- The cookbook starts from a task (read a configuration file, report three errors at once, reparse on every keystroke)
  and combines the features that the task needs.

## Running a recipe

Every program is a `main` package that you can run as it is. Put the files in a directory (a block whose first line is
`// name.go` or `// name.pego` is a file of that name, without that line), make it a module that uses PEGO, and run it:

```bash
mkdir recipe && cd recipe
go mod init example.com/recipe
go get github.com/ornew/pego
go run .
```

The recipes need Go 1.27 or later and nothing else, except where a recipe says otherwise (the ones that generate code
install the `pego` tool with `go get -tool`, and the one about ready-made parsers adds a module). The grammars are
embedded in the programs with `go:embed`, so the `.pego` files are real files that `pego fmt`, `pego parse` and the
editor support work with. Every output was produced by running the code shown, with Go 1.27.1 on an Apple M3 Max;
times differ from machine to machine.

## Read data

| Recipe | What it shows |
|:--|:--|
| [Read a configuration file into Go structs](config-to-structs.md) | A grammar for `key = value` lines with typed values, and Go code that fills a struct and reports the line of a wrong value |
| [Parse log lines fast](log-lines.md) | A generated parser with typed values (`ParseAST`) and `Recognize`; what each costs on 100,000 lines |
| [Process a file that does not fit in memory](big-files.md) | `#stream` and `ParseStream`: records one at a time, in 17 MiB for 5 million lines |
| [Parse indentation-based input](indentation.md) | Nested entries as nested Go maps, with a variable for the indentation and an error for inconsistent indentation |

## Evaluate and query

| Recipe | What it shows |
|:--|:--|
| [Evaluate arithmetic with variables and functions](calculator.md) | A Pratt expression with precedence, associativity and calls, and an evaluator that runs the same tree for many inputs |
| [Compile a filter language into a Go predicate](filter-language.md) | `age > 30 and name ~ "a*"` as closures, with word operators that do not collide with field names |

## Report problems

| Recipe | What it shows |
|:--|:--|
| [Report an error with its source line and a caret](error-carets.md) | `#error` messages and the position of a `*pego.SyntaxError`, printed like a compiler |
| [Report several errors in one run](several-errors.md) | `#recover`: a tree of what could be read, the errors, and what to watch for in the `skip` |

## Build tools

| Recipe | What it shows |
|:--|:--|
| [Tokenize source for syntax highlighting](highlighting.md) | A tokenizer that cannot fail, and HTML spans from the flat tree |
| [Reparse a document on every keystroke](as-you-type.md) | `Document`, `Edit` and `Stats`; a grammar shaped so that edits reuse most of the last parse |

## Ship, reuse and test

| Recipe | What it shows |
|:--|:--|
| [Ship a parser and keep it up to date](ship-a-parser.md) | `pego gen` with `go generate`, a test that fails when the generated file is stale, and `.pegoc` for grammars that stay data |
| [Validate a YAML file and point at the problem](ready-made-parsers.md) | A ready-made parser of [parsers/](../../parsers/README.md): syntax errors with positions, and your own checks with the positions of the typed tree |
| [Test hand-written code against a grammar](test-with-samples.md) | Inputs and near misses generated from the grammar, a differential test and a fuzz test |

## Where to go next

| To learn about | Read |
|:--|:--|
| The grammar language, from the beginning | [Getting started](../tutorial/getting-started.md) |
| One feature in depth | [Guides](../guide/README.md) |
| The exact rules of a construct | [Language specification](../../spec/README.md) |
| Complete grammars with tests | [examples/](../../examples/README.md) and [parsers/](../../parsers/README.md) |
