# PEGO Cookbook

Short recipes for concrete problems. Each one is a complete solution: the problem in a sentence or two, the grammar and
the Go program, the output they print, and a few lines on how it works and what to change. They differ from the other
documents in how they are organized:

- The tutorial teaches the language from the first rule on.
- The [guides](../guide/README.md) explain one feature at a time, in depth.
- The cookbook starts from a task (read a configuration file, report three errors at once, reparse on every keystroke)
  and combines the features that the task needs.

## Running a Recipe

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

## Read Data

| Recipe | What it shows |
|:--|:--|
| [Read a configuration file into Go structs](config-to-structs.md) | A grammar for `key = value` lines with typed values, and Go code that fills a struct and reports the line of a wrong value |
| [Parse log lines fast](log-lines.md) | A generated parser with typed values (`ParseAST`) and `Recognize`; what each costs on 100,000 lines |
| [Process a file that does not fit in memory](big-files.md) | `#stream` and `ParseStream`: records one at a time, in 17 MiB for 5 million lines |
| [Sum a column of a big CSV file](sum-a-csv-column.md) | The CSV grammar of `parsers/csv` streamed record by record: a total in about 13 MB, and the offset of the first bad record |
| [Read a header and then records](header-and-records.md) | A stream whose first element is a header: telling the kinds of element apart in the callback |
| [Stop a stream when the caller gives up](cancel-a-stream.md) | `WithContext` plus a reader that fails from outside, for a producer that goes quiet |
| [Parse indentation-based input](indentation.md) | Nested entries as nested Go maps, with a variable for the indentation and an error for inconsistent indentation |
| [Parse an expression embedded in text](prefix-fragment.md) | `ParsePrefix`: a `{{ expression }}` template renderer that parses the fragment where it starts and learns where it ends |

## Evaluate and Query

| Recipe | What it shows |
|:--|:--|
| [Evaluate arithmetic with variables and functions](calculator.md) | An Operators Rule with precedence, associativity and calls, and an evaluator that runs the same tree for many inputs |
| [Compile a filter language into a Go predicate](filter-language.md) | `age > 30 and name ~ "a*"` as closures, with word operators that do not collide with field names |
| [Find nodes in a tree and at a position](find-nodes.md) | `Walk` for every call with its span and depth, and a `Cursor` for the innermost node at an offset |

## Report Problems

| Recipe | What it shows |
|:--|:--|
| [Report an error with its source line and a caret](error-carets.md) | `@error` messages and the position of a `*pego.SyntaxError`, printed like a compiler |
| [Report several errors in one run](several-errors.md) | `#recover`: a tree of what could be read, the errors, and what to watch for in the `skip` |
| [Keep going after a bad statement in nested blocks](recover-statements.md) | Recovery at the statement rule shared by every block: errors from all levels and the statements that were fine |
| [Find out why a grammar is slow](slow-grammar.md) | `pego profile` on a 20,000-line input: which rule wastes input, the change that fixes it, and the profile that proves it |

## Build Tools

| Recipe | What it shows |
|:--|:--|
| [Tokenize source for syntax highlighting](highlighting.md) | A tokenizer that cannot fail, and HTML spans from the flat tree |
| [Reparse a document on every keystroke](as-you-type.md) | `Document`, `Edit` and `Stats`; a grammar shaped so that edits reuse most of the last parse |
| [Keep the last good tree while the user types](keep-last-good-tree.md) | An `Editor` around a `Document` that shows the last tree that parsed while a line is half typed, and what each keystroke costs |

## Ship, Reuse and Test

| Recipe | What it shows |
|:--|:--|
| [Embed a grammar in a program and share the parser](embed-grammar.md) | `go:embed`, one compiled parser used from many goroutines with `WithStart`, and a `-race` test |
| [Ship a parser and keep it up to date](ship-a-parser.md) | `pego gen` with `go generate`, a test that fails when the generated file is stale, and `.pegoc` for grammars that stay data |
| [Validate a YAML file and point at the problem](ready-made-parsers.md) | A ready-made parser of [parsers/](../../parsers/README.md): syntax errors with positions, and your own checks with the positions of the typed tree |
| [Test hand-written code against a grammar](test-with-samples.md) | Inputs and near misses generated from the grammar, a differential test and a fuzz test |
| [Check grammars in continuous integration](lint-in-ci.md) | One script for `pego fmt -check`, `pego lint -strict` and `pego test`, and a workflow that runs it |

## Where to Go Next

| To learn about | Read |
|:--|:--|
| One feature in depth | [Guides](../guide/README.md) |
| The exact rules of a construct | [Language specification](../../spec/README.md) |
| Complete grammars with tests | [examples/](../../examples/README.md) and [parsers/](../../parsers/README.md) |
