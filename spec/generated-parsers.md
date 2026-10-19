# Generated Parsers

`pego gen` translates a grammar into the source of a standalone parser, in Go or
in TypeScript. A generated parser does not load or compile a grammar at run time
and does not depend on the PEGO engine. This chapter defines the generator, the
API of the generated Go and TypeScript code, the features each target supports
and where its behavior differs from the engine. Where this chapter does not state
a difference, a generated parser returns the same value, the same positions and
the same errors as the engine for the same grammar, input, start rule and
position unit.

## Scope and Terminology

The **engine** is the `pego.Parser` that parses with a compiled grammar. A
**generated parser** is the source that `pego.GenerateGo`, `pego.GenerateTypeScript`
or `pego gen` writes. The **generation options** select optional parts of the
output. The **generated package** is the Go package named by `-pkg`; the
**generated module** is the TypeScript file. The generated code includes its own
runtime, which is the same for every grammar of one generator release.

## Contract

### The Generator

```text
pego gen -g <grammar> [-lang go|ts] [-pkg <package>] [-s <rule>] [-o <file>]
         [-types] [-document] [-stream] [-recognize] [-observability]
         [-source-info] [-nodoc] [-max-depth <n>]
```

| Flag | Go option | Meaning |
|:--|:--|:--|
| `-lang` | `GenerateGo`, `GenerateTypeScript` | `go` (default) or `ts` |
| `-g` | the grammar argument | The grammar: `.pego`, `.json`, or a compiled grammar that holds the AST |
| `-pkg` | the package name argument | Required for Go; MUST be a valid Go identifier; rejected with `-lang ts` |
| `-s` | the start rule argument | The start rule. Default: the rule saved in a compiled grammar, otherwise `main` |
| `-o` | | The output file; standard output if omitted. The command does not create directories |
| `-types` | `WithTypes()` | Generate typed values (see [Typed values](#typed-values)) |
| `-document` | `WithDocument()` | Generate the Go `Document` (Go only) |
| `-stream` | `WithStream()` | Generate `ParseStream` (Go only; the start rule MUST contain a `#stream` repetition) |
| `-recognize` | `WithRecognize()` | Generate recognition without a tree |
| `-observability` | `WithGeneratedObservability()` | Generate tracing and profiling, specified in [Observability](observability.md) |
| `-source-info` | `WithGeneratedSourceInfo()` | Make the parser record [source information](nodes.md#source-information): the rule of each node and the names of captures. Without it, `Node.Rule`, `Node.Field` and `Node.Fields` of a parsed node are empty and the outputs omit them |
| `-nodoc` | `WithoutPackageDoc()` | Omit the package comment (Go only; rejected with `-lang ts`) |
| `-max-depth` | `WithGeneratedMaxDepth(n)` | The default nested rule-call limit; zero selects 100,000, a positive value overrides it, a negative value fails generation. TypeScript also requires a safe integer |

- The output is deterministic: generating twice yields identical bytes. Omitted,
  zero and an explicit 100,000 depth default generate identical source.
- Go output is formatted as by `gofmt`. Generated Go depends only on the standard
  library, and generated TypeScript has no dependencies. Generated TypeScript uses
  only erasable syntax and needs ES2020; it is type-checked under strict
  settings.
- Grammar errors are reported with positions before any file is written, in the
  same form as in other commands. An unknown start rule, an invalid package name,
  a compiled grammar without the AST (`the compiled grammar omits the AST`),
  `-stream` without a `#stream` repetition in the start rule and a grammar that
  contains `#missing` are errors.
- A Go package may contain only one generated parser, because every generated file
  defines the same names.

### Generated Go API

```go
func Parse(input string, opts ...ParseOption) (*Node, error)
func ParseRule(name, input string, opts ...ParseOption) (*Node, error)

type ParseOption func(*parseOptions)
func WithUnit(unit PositionUnit) ParseOption
func WithMaxDepth(n int) ParseOption
func WithContext(ctx context.Context) ParseOption
func WithWorkLimits(limits WorkLimits) ParseOption

type PositionUnit int
const (
	CodePoints PositionUnit = iota // the default
	Bytes
)
```

- `Parse` parses the whole input with the start rule. `ParseRule` does the same with
  the named rule; an unknown name is the error `rule <name> is not defined`. Both
  require the rule to consume the whole input.
- `Node`, `Fields`, `NodeField`, `SyntaxError` and `SyntaxErrors` are types of the
  generated package with the members of their engine counterparts, defined in
  [Nodes and Values](nodes.md) and [Parsing](parsing.md#syntax-errors). They are
  different Go types from those of package `pego`; errors are tested with
  `errors.As` against the generated types. Node JSON is identical to the engine's.
- Options apply in order. A negative final `WithMaxDepth` returns a configuration
  error before matching, actions or named-rule lookup; zero restores the generated
  default. A nil option panics when applied. A call with no options uses code
  points and the generated depth default.
- A generated parser is safe for concurrent use: each call has its own state.
- Errors in actions (such as division by zero) and exceeding the nesting limit
  return plain errors as in the engine. The depth limit counts nested rule calls; a
  larger limit does not guarantee stack capacity. The default error is
  `nesting too deep: more than 100000 rule calls`.
- `WithContext` and `WithWorkLimits` apply the semantics of
  [Cancellation and Work Limits](parsing.md#cancellation-and-work-limits) to each
  invocation, with counters that restart for every call. They apply to named rules,
  recognition, typed parsing and conversion. The last context and the last complete
  `WorkLimits` win; a nil context means no cancellation; zero limits are unlimited.
  An exhausted limit matches `ErrWorkLimit`, and `errors.As` retrieves a
  `*WorkLimitError` with `Kind` (`InputWorkLimit` or `StepWorkLimit`), `Limit`, `Used`
  and `Requested`. An interrupted entry point returns its zero result, and
  recovery does not turn an interruption into a successful partial tree.
  Cancellation is cooperative.

### Typed Values

With `-types`, the Go package also defines a Go type for each type of the grammar
and the functions

```go
func ParseAST(input string, opts ...ParseOption) (T, error) // T: the Go type of the start rule's type
func ParseRuleAST(name, input string, opts ...ParseOption) (any, error)
```

`ParseAST` returns the errors of `Parse`. The mapping of types is:

| Grammar type | Go type |
|:--|:--|
| `int`, `string`, `bool` | The same |
| Struct type `T` | `*T` with the same fields and an embedded `Span{Start, End int}` |
| Terminal type `T` of a grammar AST ([Grammar JSON](grammar-files.md#grammar-json)) | `*T` with `Span` and `Text` |
| `node[string]`, a string node | `*TNode[string]`, with `Span` and `Value`, the text |
| `error` (from `#recover`) | `*Error` with `Span`, `Text` and `Message` |
| Union `U ~ A \| B` of node types | An interface `U` implemented by `*A`, `*B` and `*Error` (and by every `*TNode[T]` if a member is a node type) |
| `[]T` | A slice; an empty list is an empty slice and not `nil` |
| `*T` | `*int`, `*string` or `*bool` for basic types; otherwise the Go type of `T`, `nil` when absent |
| A rule without an action whose type is not declared, or is declared `node` | The Go type of its value: `*TNode[TupleN[E0, ..., En]]` for a tuple of known elements, `*TNode[[]E]` for a list, `*TNode[Unit]` for the unit `()`; `*TNode[any]` when the elements are not known |
| `node`, `any`, unions with basic members or members of different shapes, a value that closes a cycle of rules | `any`, holding the converted value |
| Alias `Alias ~ T` | A Go alias to the value type of `T` |

- `TNode[T]` is `struct { Span; Value T }`; `TupleN` is `struct { E0 E0; ...; En En }`
  and `Unit` is `struct{}`. A missing element of a tuple is `nil`. A list node's `Value` is a slice.
- A grammar type whose name collides with a name that the generated code reserves
  (`Node`, `TNode`, `Match`, `Error`, `Parse`, `Unit`, `Span`, `ParseOption`, `ParseRuleAST`
  and the other exported names of the generated runtime) gets a trailing
  underscore. Names are assigned in sorted order, so `Node` becomes `Node_` and an
  existing `Node_` becomes `Node__`. Every declared alias is exported.
- After `#recover`, an error value in a list of a union type stays in the list as
  `*Error`; where a struct or terminal type is expected it becomes `nil`.
- `ParseRuleAST` consumes the whole input, applies options once before it looks up
  the name, and returns the selected rule's typed value as `any`. Unknown names and
  invalid options return `nil` with an error. A parse failure returns the zero value
  of the selected type. `ParseRule` continues to return `*Node`.
- A generator MAY build typed values directly instead of converting the tree
  that `Parse` returns when every value has a Go type of its own; the result is the
  same either way.

Typed TypeScript output (`-types`) exports an interface for every struct, terminal
and alias and the functions `parseAST` and `parseASTWithOptions`, which parse the
configured start rule and convert the node result. They return `{ value, error }`:
a successful nil result has a null value and a null error; a failure has a null
value and the error; recovery keeps the converted value and `SyntaxErrors`. The
mapping is:

| Grammar type | TypeScript value |
|:--|:--|
| Struct, terminal | An interface with `type` (the grammar name), `start`, `end` and the fields; terminals also have `text`; nullable where an absent or incompatible recovered value becomes nil |
| `node[string]`, a string node | `Match`, which extends `TNode<string>` (`type`, `start`, `end` and `value`) |
| `[]T` | A readonly array, or null; null differs from an empty array |
| `*T` | A nullable target |
| `int` | `number \| bigint`, with signed 64-bit arithmetic and safe-number canonicalization |
| `string`, `bool` | `string`, `boolean`; an absent non-optional scalar field is `""`, `false` or `0` |
| Declared alias | The complete target type |
| Declared pure node union | The member interfaces, `RecoveredError` and null; tuple and list node members remain `Node` |
| Mixed or undeclared union, `any`, `node` | `TypedValue`, following the conversion of `any` in Go |
| Tuple and list nodes | `Node` |

Names collide the same way as in Go, with trailing underscores. Typed parsing by
rule name is not available in TypeScript.

### Recognition

With `-recognize`, Go gains `Recognize(input string, opts ...ParseOption) error` and
TypeScript gains `recognize` and `recognizeWithOptions`. They check that the whole
input matches the start rule without building a tree and return the errors that
`Parse` returns, including `SyntaxErrors` for recovered errors. Actions whose values
are not read by predicates are skipped; actions that predicates read still run and
can report runtime errors.

### Incremental Documents (Go Only)

With `-document`, the Go package gains `NewDocument(text string, opts ...ParseOption) (*Document, error)` and a
`Document` with these members: `Parse`, `Edit(start, end, replacement)`, `Text`,
`Stats` and, with `-types`, `ParseAST`.

- `Edit` replaces a half-open range in the position unit chosen at construction. It
  rejects an invalid range, and a position inside a UTF-8 character in `Bytes`
  mode, without changing the text. `Text()` returns the exact current input.
- Construction options become the defaults of later calls. A call MAY set the
  depth, the context and the work limits; it MUST NOT change the unit.
- `ParseStats` has the fields `Evaluated` and `Reused`.
- A parse returns what a parse of the whole current text returns. An interruption
  discards memo history so that a later call can retry.
- A `Document` is for one goroutine. A returned tree can share nodes with later
  parses; `Node.Clone` makes a detached copy that preserves cycles and repeated
  references. With `-types`, the typed graph that `ParseAST` publishes is detached
  from the document, and caller mutation cannot change later results.
- The returned `SyntaxError` values and their `Expected` and `Messages` slices are
  owned by the caller. Parsing, diagnostic copying and typed publication share one
  work budget.
- It supports a subset of the engine's [Incremental Documents](documents.md): there
  is no `EditBatch`, no `Version`, no snapshots and no change notifications.

### Streaming (Go Only)

With `-stream`, the Go package gains `ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error`,
`Iterate` with `StreamIterator` and `ErrStreamClosed`, and the options
`WithDiagnosticCallback` and `WithDiagnosticLimit` with `TruncatedSyntaxErrors`. They
follow the contract of [Streaming](streaming.md), with these differences: a nil
`emit` parses the stream without delivering elements, and the diagnostic options are
accepted only by the stream entry points (`Parse`, `ParseRule`, `ParseAST`,
`Recognize`, `NewDocument` and `Document` methods reject them). `Parse` treats
`#stream` as an ordinary repetition.

With `-types` as well, the package gains `StreamValue`, `ParseStreamAST` and
`IterateAST` with `StreamASTIterator`. `StreamValue` is the Go type of one element
of the `#stream` repetition, not of the start rule. Each element is converted on
its own when it is committed, so the value stays valid after the callback returns.
The conversion counts toward the work limits and observes the context.

### Generated TypeScript API

```ts
export function parse(input: string | Uint8Array, unit?: Unit): ParseResult;
export function parseRule(name: string, input: string | Uint8Array, unit?: Unit): ParseResult;
export function parseWithOptions(input: string | Uint8Array, options?: ParseOptions): ParseResult;
export function parseRuleWithOptions(name: string, input: string | Uint8Array, options?: ParseOptions): ParseResult;

export interface ParseOptions {
  unit?: Unit;
  maxDepth?: number;
  signal?: CancellationSignal | null;
  maxInputWork?: number;
  maxSteps?: number;
}
export interface ParseResult { node: Node | null; error: Error | null }
export interface CancellationSignal { readonly aborted: boolean; readonly reason?: unknown }
export type ParseInterruptionKind = "canceled" | "input-work-limit" | "step-work-limit";
export class ParseInterruptionError extends Error {
  readonly kind: ParseInterruptionKind;
  readonly limit: number;
  readonly used: number;
  readonly requested: number;
  readonly cause: unknown;
}
export type Unit = 0 | 1;
export const CodePoints: Unit; // the default
export const Bytes: Unit;
export function marshal(n: Node | null): string;
```

- `parse` does not throw for bad input: it returns `{ node, error }` with a node and
  no error; no node and an error; or, after `#recover`, a node and `SyntaxErrors`.
  It throws only for defects that are not parse errors.
- `Node` has `type`, `rule`, `start`, `end`, `text`, `children`, `fields`, `field(name)`,
  `isTerminal()`, `toString()` and `toJSON()`, with the meaning given in
  [Nodes and Values](nodes.md). A field value is a `Node`, a number, a bigint, a
  string, a boolean or `null`.
- `SyntaxError` has `pos`, `line`, `col`, `expected`, `messages` and `reason()`, and
  its `message` is the text of the engine's error; `SyntaxErrors` has `errors`.
- `marshal(node)` returns exactly the bytes that `encoding/json` writes for the
  engine's node. `JSON.stringify` yields the same keys in the same order, except
  that integers beyond the safe range are rounded and, in `Bytes` mode, an invalid
  input byte is written as the surrogate that represents it.
- `marshal` and `toString` write trees of any depth. `JSON.stringify` recurses and
  can exhaust the stack for deep trees.
- `maxDepth` MUST be a positive safe integer or zero; negative values, fractions, NaN,
  infinity and unsafe integers return a configuration error in the result before
  matching. Omitted or zero selects the generated default.
- `signal` is a structural cancellation signal: any object with `aborted` and an optional
  `reason`, such as an `AbortSignal`. `maxInputWork` and `maxSteps` MUST be finite
  nonnegative safe integers, and zero means unlimited; other values return a
  configuration error in the result. The units of the limits are those of
  [Cancellation and Work Limits](parsing.md#cancellation-and-work-limits).
- An interrupted parse returns `node` as `null` and `error` as a `ParseInterruptionError`
  whose `kind` is `"canceled"`, `"input-work-limit"` or `"step-work-limit"`. For a limit,
  `limit`, `used` and `requested` give the accounting; for cancellation, `cause` is the
  `reason` of the signal. Recovery does not turn an interruption into a partial result,
  and the parser keeps no reference to the signal after the call returns. Typed
  conversion (`parseAST`) and recognition report interruptions the same way.
- A synchronous parse does not yield to the event loop: a signal that changes only
  through timers or other tasks of the same thread is not observed during the call. A
  signal that is already aborted is observed at the first poll, and the work limits do not
  depend on the event loop.
- With `-recognize`, `recognize` and `recognizeWithOptions` return `null` if the
  input matches and an `Error` otherwise. With `-observability`, the batch entry
  points accept the tracing and profiling options of [Observability](observability.md).
- A generated TypeScript module exports the same names for every grammar, so two
  parsers are imported under different namespaces. A parser keeps no state between
  calls.

### Input and Positions in TypeScript

Positions count Unicode code points (the default) or UTF-8 bytes (`Bytes`), as in the
engine, and not UTF-16 code units. The parser decodes the input first.

- A string is parsed as its UTF-8 encoding would be. A lone surrogate becomes U+FFFD.
- A `Uint8Array` is parsed as UTF-8 bytes; an invalid sequence decodes to U+FFFD, one
  per byte, as in Go. In `Bytes` mode, positions count the original bytes.
- In `Bytes` mode, text keeps the bytes: each invalid byte *b* is represented by the
  lone surrogate U+DC00+*b* in node text and in `text(...)`. `marshal` writes such a
  byte as U+FFFD.
- String operations in actions follow Go: `+` concatenates, comparisons order by
  code point, and `len` counts code points or UTF-8 bytes. Integers are signed 64-bit
  and wrap on overflow; division truncates toward zero and `%` takes the sign of the
  dividend. An integer is a `number` while it is a safe integer and a `bigint` beyond.

### TypeScript Errors and the Stack

| Error | `error` |
|:--|:--|
| The input does not match | `SyntaxError` |
| `#recover` skipped input | `SyntaxErrors`, and `node` is set |
| A runtime error in an action | `Error` with the message of the engine |
| More nested rule calls than the limit (default 100,000) | `Error`: `nesting too deep: more than 100000 rule calls` |
| The JavaScript stack is exhausted before the limit | `Error`: `nesting too deep: the JavaScript stack overflowed at N rule calls` |

The host stack of a JavaScript runtime is far smaller than the stack of a Go
program, so a TypeScript parser can reject nesting that the engine accepts.

## Validation and Errors

| Condition | Result |
|:--|:--|
| Invalid package name, unknown start rule, grammar errors | Error before output |
| `#missing` in the grammar | Generation fails |
| `-stream` or `-document` with `-lang ts` | Generation fails |
| `-pkg` or `-nodoc` with `-lang ts` | Generation fails |
| `-stream` without a `#stream` repetition in the start rule | Generation fails |
| Negative `-max-depth` | Generation fails |

## Compatibility and Support

| Feature | Generated Go | Generated TypeScript |
|:--|:--|:--|
| Rule expressions, captures, atomic and discarded expressions | Supported | Supported |
| Operators Rules, left recursion (direct and indirect) | Supported | Supported |
| Predicates, variables, lookahead, cut | Supported | Supported |
| Actions, types, built-in functions | Supported | Supported |
| `@error`, `#recover`, `SyntaxErrors` | Supported | Supported |
| `#missing` | Not supported | Not supported |
| Position units | `WithUnit` | `unit` |
| Another start rule | `ParseRule` | `parseRule` |
| Recognition | `Recognize` with `-recognize` | `recognize` with `-recognize` |
| Typed values | `ParseAST`, `ParseRuleAST` with `-types` | `parseAST`, `parseASTWithOptions` with `-types` |
| Streaming | `ParseStream`, `Iterate` with `-stream` | None; `#stream` is an ordinary repetition |
| Incremental documents | A subset with `-document` | None |
| Cancellation and work limits | `WithContext`, `WithWorkLimits` | `signal`, `maxInputWork`, `maxSteps`; `ParseInterruptionError` |
| Nested rule-call limit | `WithMaxDepth`, `-max-depth` | `maxDepth`, `-max-depth`; the host stack also bounds nesting |
| Choice of backend, saved grammar, `Parser.Grammar()` | Not applicable | Not applicable |
| Tracing and profiling | With `-observability` | With `-observability` (batch parsers) |

A generated parser embeds the runtime of the generator that produced it and does not
follow later changes of the grammar or of the generator. Regenerating can rename grammar types that collide with names that the new runtime
reserves, and can change exported signatures (for example, entry points that took
a variable number of `Unit` values take `ParseOption` values).

## Conformance

The repository tests every grammar in `examples/` and the grammars of the engine
tests against the generated Go and TypeScript parsers, in both position units and
with strings and bytes as input, and require identical trees (as JSON) and errors.
TypeScript output is type-checked with `tsc --strict` and additional strict flags.
