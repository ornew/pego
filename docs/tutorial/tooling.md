# Working With the Tooling

A grammar that works on the inputs you tried will fail on one you did not, and it will be slower than you expect on
some input. This lesson shows the tools that find out why: `explain`, `trace`, `profile` and `sample`, then the editor
and the playground.

## What You Will Build

Nothing new: you use the configuration grammar to look at a failure, the calls of a parse, and inputs that the grammar
generates itself.

## Before You Start

Complete [Building Typed Trees](typed-trees.md): you need `config.pego`.

## Why Did It Fail?

`pego parse` reports the farthest failure and what was expected there. `pego explain` shows how the parser got there: the
calls that recorded an expectation at the failing position, innermost first, and the rules they were called from.
Here `debug = tru` is missing the end of `true`:

```bash
printf 'host = "localhost"\nport = 8080\ndebug = tru\n' | pego explain -g config.pego
```

```text
3:9: syntax error: expected "#", "\"", "false", "true", [ \t\r\n], [0-9]

Calls that recorded what was expected at 3:9 (innermost first):

  blank 3:8: matched " ", then expected [ \t\r\n]
    in ws 3:8
    in entry 3:1
    in main 1:1

  blank 3:9: expected [ \t\r\n]
    in ws 3:8
    in entry 3:1
    in main 1:1

  ws 3:8: matched " ", then expected "#"
    in entry 3:1
    in main 1:1

  number 3:9: expected [0-9]
    in value 3:9
    in entry 3:1
    in main 1:1

  bool 3:9: expected "false", "true"
    in value 3:9
    in entry 3:1
    in main 1:1

  value 3:9: expected "\""
    in entry 3:1
    in main 1:1
```

Every item of the expected list comes from one of these calls, so you can see which rule to change to improve a message.

## What Does the Parser Do?

`pego trace` prints the whole call tree of a parse: every rule call with its start position and whether it matched.
This is the trace for `a = 1`, including the failed attempt at a second entry at the end:

```bash
pego trace -g config.pego -i 'a = 1'
```

```text
main 1:1
  ws 1:1
    blank 1:1 -> failed
  ws -> matched 1:1-1:1 ""
  entry 1:1
    key 1:1 -> matched 1:1-1:2 "a"
    ws 1:2
      blank 1:2 -> matched 1:2-1:3 " "
      blank 1:3 -> failed
    ws -> matched 1:2-1:3 " "
    ws 1:4
      blank 1:4 -> matched 1:4-1:5 " "
      blank 1:5 -> failed
    ws -> matched 1:4-1:5 " "
    value 1:5
      number 1:5 -> matched 1:5-1:6 "1"
    value -> matched 1:5-1:6 "1"
    ws 1:6
      blank 1:6 -> failed
    ws -> matched 1:6-1:6 ""
  entry -> matched 1:1-1:6 "a = 1"
  entry 1:6
    key 1:6 -> failed
  entry -> failed
main -> matched 1:1-1:6 "a = 1"
```

## Why Is It Slow?

`pego profile` runs the parse and reports, for each rule, the calls, how many were evaluated and how many were answered
from the memo, the input consumed, the input wasted by failed calls and the time spent in the rule itself. It ends with a
short list of what to look at:

```bash
pego profile -g config.pego -n 5 -i 'host = "localhost"
port = 8080
'
```

The columns to read first are `wasted`, for rules that scan a lot of input and then fail, and `evals`, for rules that
run several times at one position. The times depend on your machine, so they are not shown here.

## Inputs From the Grammar

`pego sample` generates inputs that the grammar accepts, from a seed, so that a run is repeatable. They show what the
grammar really allows, and make a test or a fuzz corpus without writing inputs by hand. `-invalid` generates near-miss
inputs that the grammar rejects:

```bash
pego sample -g config.pego -n 4 -seed 1
pego sample -g config.pego -n 3 -seed 1 -invalid
```

```text
""
"y_#ᵾp\n = \"-\""
"\n\n\tz_=false#"
"q\t \t =#M+\n true"
"\\"
"y_#ᵾp"
"\t]"
```

The first four lines are valid inputs and the last three are not; the invalid ones are a key without
`=` and a value, or no entry at all.

## In the Editor and the Browser

`pego lsp` runs a language server for `.pego` files with diagnostics, formatting, navigation, hover, rename, completion
and semantic highlighting; the [Editor Support](../guide/editor-support.md) guide shows how to set it up in VS Code, Neovim,
Helix and Emacs. The [playground](https://pego.ornew.net/playground/) runs PEGO in the browser: write a grammar, type
input and see the tree or the error immediately, and share the state through the address.

## Next Steps

You have now covered the whole path from a first grammar to a shipped, tested and debugged parser. The guides in
[Developing Grammars](../guide/README.md#developing-grammars) go deeper on each tool, and the
[cookbook](../cookbook/README.md) solves concrete tasks. To keep practicing, extend the configuration language with
`[section]` headers and lists of values, then read the grammar of [parsers/json](../../parsers/json/json.pego) to see how
a larger grammar is organized.
