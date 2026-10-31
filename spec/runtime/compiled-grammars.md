# Compiled Grammars

A **compiled grammar** is a grammar that has been parsed, analyzed, type-checked
and translated to bytecode, and then saved to a file, conventionally with the
extension `.zpc`. Loading the file gives a parser that runs the parses of the
file's mode like the parser that was saved, without repeating that work. This
chapter defines the Go and command-line entry points, the modes and the
operations each one serves, the version rules and the checks that loading
performs. The byte layout of the file is defined in
[ZeroParse Bytecode Specification](../bytecode/README.md#file-format).

## Scope and Terminology

A **compiled grammar** holds one execution-only compile result: the bytecode
module of one **mode** and the start rule. It holds no grammar AST and no output
schema. The mode is chosen when the grammar is saved and recorded in the file.

| Mode | Go | Serves |
|:--|:--|:--|
| `parse` (default) | `zeroparse.CompiledParse` | `Parse`, `ParsePrefix`, `ParseStream` and `Iterate` without `RecognizeOnly`, `WithTrace` or `WithProfile`; `zp parse` and `zp parse -stream` |
| `recognize` | `zeroparse.CompiledRecognize` | `Parse` and `ParsePrefix` with `RecognizeOnly` and without `WithTrace` or `WithProfile`; `zp parse -check` |
| `document` | `zeroparse.CompiledDocument` | `NewDocument`; `Parse`, `ParsePrefix`, `ParseStream` and `Iterate` with `WithTrace` or `WithProfile` and without `RecognizeOnly`; `zp trace`, `zp profile` and `zp explain` |

A parse-mode and a document-mode file of the same grammar MAY hold the same
module; a recognize-mode file holds the module of the grammar's recognizer.

## Contract

### Saving and Loading

| Operation | Go | Command line |
|:--|:--|:--|
| Save in parse mode | `Parser.MarshalBinary()` or `Parser.Marshal(zeroparse.CompiledParse)` | `zp compile -g g.zp -o g.zpc` |
| Save in another mode | `Parser.Marshal(zeroparse.CompiledRecognize)`, `Parser.Marshal(zeroparse.CompiledDocument)` | `zp compile -g g.zp -mode recognize\|document -o g.zpc` |
| Name a mode | `zeroparse.ParseCompiledMode("document")`, `CompiledMode.String()` | The value of `-mode` |
| Load | `zeroparse.LoadParser(data)` | `-g file.zpc` on `parse`, `trace`, `profile`, `explain` and `compile` |
| Mode of a loaded parser | `Parser.CompiledMode()` returns the mode and `true`; `false` for a parser compiled from a grammar | |
| Recognize a compiled grammar | `zeroparse.IsCompiled(data)` | Used to tell source, JSON and a compiled grammar apart |

- The start rule is saved with the module. `zp parse`, `zp compile` and the
  debugging commands use the saved start rule when `-s` is not given, and `-s`
  overrides it with another start rule of the grammar.
- The same parser always produces the same bytes for a mode. A parser that was
  loaded and saved again produces the bytes it was loaded from (see
  [Re-saving](#re-saving)).
- `zeroparse.IsCompiled` distinguishes a compiled grammar from other data by its magic
  number.
- A loaded parser is an ordinary `*zeroparse.Parser`: it is safe for concurrent use,
  `WithStart`, `Starts` and `DefaultStart` work on it, and it runs the operations
  of its mode. For those operations, a parser loaded from a file returns the same
  tree, positions and errors as the parser compiled from the same source.

### Operations of a Loaded Parser

A loaded parser runs on the bytecode backends. `DefaultBackend` selects the
recursive bytecode VM, and `WithBackend(Bytecode)` and
`WithBackend(BytecodeIterative)` choose between the recursive and iterative VMs
when the file is loaded; the choice does not depend on the mode.

An operation that the file's mode does not serve returns an error that names the
file's mode, the mode the operation needs and the remedy; the parser does not
fall back to another mode:

```text
the compiled grammar is in parse mode; documents need document mode: compile it with -mode document (zeroparse.CompiledDocument), or compile the grammar from source
```

The operations that need the grammar itself work only on a parser compiled from
source:

| Operation | Loaded parser |
|:--|:--|
| `WithBackend(Closure)` | Error: `the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends` |
| `RecognizeOnly()` with `WithTrace` or `WithProfile` | Error in every mode: traced or profiled recognition needs the grammar source |
| `Parser.Grammar()` | `nil` |
| `OutputSchema` | An error matching `ErrSchemaUnavailable`: the schema is exported from the grammar source |
| `lint.Config.Run`, `gen.Go.Generate`, `gen.TypeScript.Generate`, `sample.New` | Need `Parser.Grammar()`, so they do not take a loaded parser |
| `zp convert`, `zp gen`, `zp lint`, `zp sample`, `zp schema` | Error: the command needs the grammar source (`.zp` or `.json`) |

### Re-Saving

- A loaded parser is saved again only in its own mode, and then reproduces the
  bytes it was loaded from. Saving it in another mode is an error that asks for
  the grammar to be compiled from source in that mode; `zp compile` writes no
  output then.
- `zp compile -g file.zpc` saves in parse mode unless `-mode` is given, so a
  recognize-mode or document-mode file is saved again with its own `-mode`.

### Versions and Compatibility

A file carries two version numbers after its magic number.

| Number | Current value | Meaning |
|:--|--:|:--|
| Format version | 7 | The layout of the file. A runtime loads only version 7 and refuses any other version |
| Instruction-set version | 29 | The bytecode of the value model. Every module declares it |

- A runtime refuses a file of another format version with
  `unsupported compiled grammar version N (want 7): compile the grammar again`.
- A runtime refuses a module of an earlier instruction set: its bytecode builds the
  syntax trees that preceded the value model. The error asks for the grammar to be
  compiled again. A file that requires a newer instruction set is refused with
  `unsupported instruction set version N`.
- A file records whether the program was compiled with source information, and the
  loaded program keeps it.
- A runtime with a narrower implementation `int` rejects repetition bounds outside
  its range.
- The same grammar MAY produce different bytes in a different release of the
  compiler. Equality of bytes is guaranteed only for one release.
- A saved file retains the left-recursion classification chosen by the compiler
  that wrote it. Loading a file does not rerun that analysis.

### Loading

Loading checks the magic number, the format version, a CRC-32 checksum, the mode,
the instruction-set version and the structure of the module (every index is in
range, every jump goes forward except the one that closes a repetition and jumps
back, the start rule is a start rule of the module, and no bytes trail the module).
It also rejects a recovery region that encloses a streaming commitment, including
files that were made with `#recover` on the `#stream` repetition.

Loading does not type-check the grammar again and does not verify that the
bytecode uses the VM stacks correctly. It trusts the mode recorded in the file as
it trusts the module's types: it checks that the mode is known, not that the
module is the one compiled for that mode, so a file that labels a recognize-mode
module as parse mode MAY return no value and no error. Load compiled grammars
only from trusted sources. A file that was made to contain type errors
MAY make actions fail while parsing, and made-up bytecode is not guaranteed to
terminate. The checksum detects accidents, not tampering, and is not a signature.
A file from a source that is not trusted therefore cannot be verified by ZeroParse;
authenticating it, or accepting grammar source and compiling it with `CompileSource`,
is up to the program.

## Validation and Errors

| Message | Cause |
|:--|:--|
| `not a compiled ZeroParse grammar` | The data does not start with the magic number |
| `unsupported compiled grammar version N (want 7): compile the grammar again` | The format version is not 7 |
| `unsupported instruction set version N: the file was compiled by an older zp; compile the grammar again` | The file was compiled for an earlier instruction set |
| `unsupported instruction set version N` | The file requires a newer instruction set |
| `invalid compiled grammar: checksum mismatch` | The file is truncated or changed |
| `invalid compiled grammar: unknown mode N` | The mode byte names no mode |
| `invalid compiled grammar: ...` | The structure is wrong although the checksum matches |
| `the compiled grammar is in M mode; O N mode: compile it with -mode N (...), or compile the grammar from source` | An operation that mode `N` serves was requested of a file in mode `M`; `O` is `parsing needs`, `recognition needs`, `documents need` or `tracing and profiling need` |
| `the compiled grammar is in M mode; traced or profiled recognition needs the grammar source: compile the grammar from source` | `RecognizeOnly` was combined with `WithTrace` or `WithProfile` |
| `the compiled grammar is in M mode and is saved only in that mode; compile the grammar from source to save it in N mode` | `Marshal(N)` was called on a parser loaded in mode `M` |
| `the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends` | `Closure` was requested of a loaded parser |
| `output schema unavailable: a compiled grammar holds no schema; export the schema from the grammar source` | `OutputSchema` was called on a loaded parser; the error matches `ErrSchemaUnavailable` |
| `F: a compiled grammar holds only the bytecode of one mode; C needs the grammar source (.zp or .json)` | Command `C` that needs the grammar was given the compiled grammar `F` |

## Compatibility and Support

A compiled grammar needs the ZeroParse engine at run time. Generated parsers do not
use compiled grammars; they are specified in [Generated Parsers](generated-parsers.md).

## Conformance

The tests of the repository save, load and re-save grammars in every mode on the
bytecode backends, compare the results with parsers compiled from source, and
check that each mode refuses the operations of the others. The byte layout is
established by [ZeroParse Bytecode Specification](../bytecode/README.md#file-format).
