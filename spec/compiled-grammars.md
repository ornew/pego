# Compiled Grammars

A **compiled grammar** is a grammar that has been parsed, analyzed, type-checked
and translated to bytecode, and then saved to a file, conventionally with the
extension `.pegoc`. Loading the file gives a parser that behaves like the one
that was saved, without repeating that work. This chapter defines the Go and
command-line entry points, what a file with and without the grammar AST
supports, the version rules and the checks that loading performs. The byte
layout of the file is defined in [PEGO Bytecode Specification](bytecode.md#file-format).

## Scope and Terminology

A **compiled grammar** holds the bytecode module, the start rule and, optionally,
the grammar AST. A file **with the AST** holds the grammar AST in addition to the
bytecode. A file **without the AST** holds the bytecode, and, when a complete
recognizer is available, an independent **recognition bundle**.

## Contract

### Saving and Loading

| Operation | Go | Command line |
|:--|:--|:--|
| Save with the AST | `Parser.MarshalBinary()` | `pego compile -g g.pego -o g.pegoc` |
| Save without the AST | `Parser.Marshal(pego.WithoutAST())` | `pego compile -g g.pego -no-ast -o g.pegoc` |
| Load | `pego.LoadParser(data)` | `-g file.pegoc` on every command that takes a grammar |
| Recognize a compiled grammar | `pego.IsCompiled(data)` | Used to tell source, JSON and a compiled grammar apart |

- The grammar AST is the grammar as written: inline actions stay in their rules and
  are lowered to hidden rules again when the file is loaded.
- The start rule is saved with the grammar. `pego parse`, `pego compile` and
  `pego gen` use the saved start rule when `-s` is not given, and `-s` overrides it.
- The same parser always produces the same bytes. A parser that was loaded and
  marshaled again produces the bytes it was loaded from, if no other
  option changes the form (see [Re-saving](#re-saving)).
- `pego.IsCompiled` distinguishes a compiled grammar from other data by its magic
  number.
- A loaded parser is an ordinary `*pego.Parser`: it is safe for concurrent use,
  `WithStart` changes its start rule, and `Parse`, `ParsePrefix`, `ParseStream`
  and `NewDocument` work on it within the limits of the next section. A parser
  loaded from a file returns the same tree, positions and errors as the parser
  compiled from the same source.

### With and Without the AST

A compiled grammar always holds bytecode. A save with the AST uses container
version 2 and has no recognition bundle. A save without the AST uses container
version 3 with a recognition bundle when a complete recognizer is available.

| | With the AST | Without the AST |
|:--|:--|:--|
| `Parse`, `ParsePrefix`, `ParseStream`, `NewDocument` | Supported | Supported on the bytecode backends |
| Default backend | Closure | Recursive bytecode |
| `WithBackend(Closure)` | Supported | Error: `the closure backend needs the grammar, which the compiled grammar omits` |
| `WithBackend(Bytecode)`, `WithBackend(BytecodeIterative)` | Supported | Supported |
| `RecognizeOnly()`, `pego parse -check` | Supported | Supported on the bytecode backends when a version 3 recognition bundle is present; a legacy file without a bundle returns an error |
| `Parser.Grammar()` | The grammar, without source positions other than those of casts in a module that records source information | `nil` |
| `pego.Lint`, `pego lint` | Supported | Not supported |
| `pego.GenerateGo`, `pego.GenerateTypeScript`, `pego gen` | Supported; the output equals that from source | Not supported |
| `pego convert` to `.pego` or `.json` | Supported, without comments or layout | Error: `the compiled grammar omits the AST` |
| `pego sample` | Supported | Error: `the parser has no grammar AST` |
| `pego explain`, `pego trace`, `pego profile` | Supported | Supported |
| `WithStart`, `Marshal` | Supported | Supported |
| Output schema (`OutputSchema`) | Not supported; see [Output Schemas and Queries](schemas-and-queries.md) | Not supported |

### Re-Saving

- `pego compile -g file.pegoc -no-ast` drops the AST of an existing file.
- A file without the AST cannot be turned into a file with the AST. Compiling one
  without `-no-ast` fails and writes no output.
- Compiling a source or a file with the AST using `-no-ast` writes a version 3
  file when a complete recognizer is available. Re-saving a version 3 file without
  the AST with `-no-ast` preserves its bundle.
- A legacy version 2 file without the AST has no bundle and cannot gain one by
  re-saving.
- A valid legacy or experimental version 3 file with the AST may load, but saving
  it again writes canonical version 2 and drops its recognition bundle.

### Versions and Compatibility

A file carries two version numbers after its magic number.

| Number | Current values | Meaning |
|:--|--:|:--|
| Format version | 2 or 3 | The layout of the file: version 2 for a save with the AST, version 3 for a save without the AST that has a recognition bundle. A runtime loads only versions 2 and 3 |
| Instruction-set version | 23 | The bytecode of the value model. Every module and recognition bundle declares it |

- A runtime refuses a module of an earlier instruction set: its bytecode builds the
  syntax trees that preceded the value model. The error asks for the grammar to be
  compiled again.
- A runtime refuses a file that requires a newer format or instruction set, with
  `unsupported compiled grammar version` or `unsupported instruction set version`.
- A file records whether the program was compiled with source information, and the
  loaded program keeps it.
- A runtime with a narrower implementation `int` rejects repetition bounds outside
  its range.
- The same grammar MAY produce different bytes in a different release of the
  compiler. Equality of bytes is guaranteed only for one release.
- A saved file retains the left-recursion classification chosen by the compiler
  that wrote it. Loading a file does not rerun that analysis.

### Loading

Loading checks the magic number, both versions, a CRC-32 checksum, the structure
(every index is in range, every jump goes forward except the one that closes a repetition and jumps back, no bytes trail the module) and
the nesting depth. It also rejects a recovery region that encloses a streaming
commitment, including files that were made with `#recover` on the `#stream`
repetition.

Loading does not type-check the grammar again and does not verify that the
bytecode uses the VM stacks correctly. A file that was made to contain type errors
MAY make actions fail while parsing, and made-up bytecode is not guaranteed to
terminate. The checksum detects accidents, not tampering, and is not a signature.
A file from a source that is not trusted therefore cannot be verified by PEGO;
authenticating it, or accepting grammar source and compiling it with `CompileSource`,
is up to the program.

## Validation and Errors

| Message | Cause |
|:--|:--|
| `not a compiled PEGO grammar` | The data does not start with the magic number |
| `unsupported compiled grammar version N (want 3)` | The format version is not supported |
| `unsupported instruction set version N: the file was compiled by an older pego; compile the grammar again` | The file was compiled for an earlier instruction set |
| `unsupported instruction set version N` | The file requires a newer instruction set |
| `invalid compiled grammar: checksum mismatch` | The file is truncated or changed |
| `invalid compiled grammar: ...` | The structure is wrong although the checksum matches |
| `the closure backend needs the grammar, which the compiled grammar omits` | `Closure` was requested for a file without the AST |
| `recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source` | Recognition was requested for a legacy file without the AST and without a bundle |
| `the compiled grammar omits the AST` | A command that needs the AST (`convert`, `gen`, `lint`) was given a file without it |

## Compatibility and Support

A compiled grammar needs the PEGO engine at run time. Generated parsers do not
use compiled grammars; they are specified in [Generated Parsers](generated-parsers.md).

## Conformance

The tests of the repository save, load and re-save grammars on every backend and
compare the results with parsers compiled from source. The byte layout is
established by [PEGO Bytecode Specification](bytecode.md#file-format).
