# Compiled Grammars

A **compiled grammar** is a grammar that has been parsed, analyzed, type-checked
and translated to bytecode, and then saved to a file, conventionally with the
extension `.pegoc`. Loading the file gives a parser that behaves like the one
that was saved, without repeating that work. This chapter defines the Go and
command-line entry points, what a file with and without the grammar AST
supports, the output schema section, the version rules and the checks that
loading performs. The byte layout of the file is defined in
[PEGO Bytecode Specification](bytecode.md#file-format).

## Scope and Terminology

A **compiled grammar** holds the bytecode module, the start rule and, optionally,
the grammar AST. A file **with the AST** holds the grammar AST in addition to the
bytecode. A file **without the AST** holds the bytecode, and, when a complete
recognizer is available, an independent **recognition bundle**. It holds the
**schema section**, the output schema of the grammar, only when it was saved with
the schema.

## Contract

### Saving and Loading

| Operation | Go | Command line |
|:--|:--|:--|
| Save with the AST | `Parser.MarshalBinary()` | `pego compile -g g.pego -o g.pegoc` |
| Save without the AST | `Parser.Marshal(pego.WithoutAST())` | `pego compile -g g.pego -no-ast -o g.pegoc` |
| Save without the AST, with the schema | `Parser.Marshal(pego.WithoutAST(), pego.WithSchema())` | `pego compile -g g.pego -no-ast -schema -o g.pegoc` |
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

A compiled grammar always holds bytecode. A save with the AST has no recognition
bundle and no schema section. A save without the AST has a recognition bundle when
a complete recognizer is available. It has a schema section only when the save
requests one (`WithSchema`, or `-schema` on the command line). A save with the AST
MUST NOT request the schema section: the loaded parser derives its schema from the AST.

| | With the AST | Without the AST |
|:--|:--|:--|
| `Parse`, `ParsePrefix`, `ParseStream`, `NewDocument` | Supported | Supported on the bytecode backends |
| Default backend | Closure | Recursive bytecode |
| `WithBackend(Closure)` | Supported | Error: `the closure backend needs the grammar, which the compiled grammar omits` |
| `WithBackend(Bytecode)`, `WithBackend(BytecodeIterative)` | Supported | Supported |
| `RecognizeOnly()`, `pego parse -check` | Supported | Supported on the bytecode backends when the file holds a recognition bundle; a file without a bundle returns an error |
| `Parser.Grammar()` | The grammar, without source positions other than those of casts in a module that records source information | `nil` |
| `pego.Lint`, `pego lint` | Supported | Not supported |
| `pego.GenerateGo`, `pego.GenerateTypeScript`, `pego gen` | Supported; the output equals that from source | Not supported |
| `pego convert` to `.pego` or `.json` | Supported, without comments or layout | Error: `the compiled grammar omits the AST` |
| `pego sample` | Supported | Error: `the parser has no grammar AST` |
| `pego explain`, `pego trace`, `pego profile` | Supported | Supported |
| `WithStart`, `Marshal` | Supported | Supported |
| Output schema (`OutputSchema`) | Supported; derived from the AST on the first call | Supported when the file holds a schema section; otherwise `ErrSchemaUnavailable` |

The schema is exported from the AST by the same checks that compile the grammar. The
derivation happens on the first `OutputSchema` call, not when the file is loaded.
See [Output Schemas and Queries](schemas-and-queries.md).

### Re-Saving

- `pego compile -g file.pegoc -no-ast` drops the AST of an existing file.
- A file without the AST cannot be turned into a file with the AST. Compiling one
  without `-no-ast` fails and writes no output.
- Compiling a source or a file with the AST using `-no-ast` writes a recognition
  bundle when a complete recognizer is available.
- Re-saving a file without the AST keeps its recognition bundle and its schema
  section. A file without the AST and without a bundle cannot gain a bundle by
  re-saving.
- Saving a file with the AST writes its AST and no recognition bundle and no schema
  section.
- `-schema` is an error without `-no-ast`, and `WithSchema` is an error without
  `WithoutAST`, because a file with the AST derives its schema from the AST.

### Versions and Compatibility

A file carries two version numbers after its magic number.

| Number | Current value | Meaning |
|:--|--:|:--|
| Format version | 4 | The layout of the file. A runtime loads only version 4 and refuses any other version |
| Instruction-set version | 26 | The bytecode of the value model. Every module and recognition bundle declares it |

- A runtime refuses a module of an earlier instruction set: its bytecode builds the
  syntax trees that preceded the value model. The error asks for the grammar to be
  compiled again.
- A runtime refuses a file of another format version with
  `unsupported compiled grammar version N (want 4)`, and a file that requires a
  newer instruction set with `unsupported instruction set version`.
- A file records whether the program was compiled with source information, and the
  loaded program keeps it.
- A runtime with a narrower implementation `int` rejects repetition bounds outside
  its range.
- The same grammar MAY produce different bytes in a different release of the
  compiler. Equality of bytes is guaranteed only for one release.
- A saved file retains the left-recursion classification chosen by the compiler
  that wrote it. Loading a file does not rerun that analysis.

### Schema Section

A file without the AST MAY carry a schema section after its recognition bundle. The
section is a compact JSON document, separate from the bytecode and the AST:

```text
{"schema": <complete schema>,
 "starts": [{"start": "main", "rules": ["main", ...], "types": ["inferred/main", ...]}, ...]}
```

- `schema` is the complete output schema of the grammar: every rule and declared type.
  Its start rule is the default start rule of the module.
- `starts` has one entry for each start rule of the module, in declaration order. Each
  entry names the rules and the types (by ID) of that start rule's export.
- The export of a start rule is the complete schema restricted to the names in its
  entry. The entries keep the order of the complete schema.
- A loaded schema section is kept as it was loaded, so re-saving reproduces its bytes.

A loader MUST check the section against the module: the document has no unknown
fields and no trailing data, the complete schema is valid, its start rule is the
default start rule, `starts` has one entry for each start rule in order, and every
rule and type named by an entry exists in the complete schema.

A file with the AST MUST NOT carry a schema section. A file without the AST and without
a schema section has no output schema. Its `OutputSchema` returns an error matching
`ErrSchemaUnavailable`.

### Loading

Loading checks the magic number, the format and instruction-set versions, a CRC-32 checksum, the structure
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
| `unsupported compiled grammar version N (want 4)` | The format version is not 4 |
| `unsupported instruction set version N: the file was compiled by an older pego; compile the grammar again` | The file was compiled for an earlier instruction set |
| `unsupported instruction set version N` | The file requires a newer instruction set |
| `invalid compiled grammar: checksum mismatch` | The file is truncated or changed |
| `invalid compiled grammar: ...` | The structure is wrong although the checksum matches, or the schema section is invalid |
| `invalid compiled grammar: a schema section with the AST` | A file with the AST carries a schema section |
| `the closure backend needs the grammar, which the compiled grammar omits` | `Closure` was requested for a file without the AST |
| `recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source` | Recognition was requested for a file without the AST and without a bundle |
| `output schema unavailable: the compiled grammar omits the AST and has no schema section; save it with the schema (pego compile -no-ast -schema, or pego.WithSchema with pego.WithoutAST)` | `OutputSchema` was called on a file without the AST and without a schema section; the error matches `ErrSchemaUnavailable` |
| `the compiled grammar omits the AST` | A command that needs the AST (`convert`, `gen`, `lint`) was given a file without it |

## Compatibility and Support

A compiled grammar needs the PEGO engine at run time. Generated parsers do not
use compiled grammars; they are specified in [Generated Parsers](generated-parsers.md).

## Conformance

The tests of the repository save, load and re-save grammars on every backend and
compare the results with parsers compiled from source. The byte layout is
established by [PEGO Bytecode Specification](bytecode.md#file-format).
