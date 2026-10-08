# 013. Generating TypeScript Parsers

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-08

## Summary

Generate a TypeScript module for a parser, without dependencies, from a grammar (`pego gen -lang ts`,
`pego.GenerateTypeScript`). It returns the same trees and errors as the engine and the generated Go parsers
([008](008-code-generation.md)).

## Design

### The same generator, another runtime

The TypeScript generator (`internal/engine/gen_ts.go`) is the Go generator with a different output: it compiles the
grammar with the engine, walks the same analysis results (memoized rules, left-recursion leaders, value-free twins,
plain rules, projected repetitions, first-character dispatch, the recognizer) and emits a function per parsing
expression and a function per action and predicate. It shares the expectation table, the naming and `first` with the
Go generator (`tsGen` embeds `generator`).

The runtime, `internal/engine/tsrt/runtime.ts`, is a port of `genrt/runtime.go`, function for function: rule calls with
deferred memoization (`firstCall`, with the per-rule eager switch), left recursion by growing the seed, memo entries
keyed by variable values, expectation records isolated in memoized calls and kept for reuse, captures with frames and
the trail, `attachCaptures` and `finish`, `#error` and `#recover`, the Pratt loop, and every built-in function including
the fused `concat`. Keeping the two side by side makes a change to one easy to carry to the other; the parity test
catches what is not.

| Part | Contents |
|:--|:--|
| Runtime | `tsrt/runtime.ts` after its `--- Runtime ---` line, the same in every file. It type-checks on its own. |
| Rule table | A constant per rule (`R0`, `R1`, ...; `Q0`, ... for the recognizer) with the engine's analysis results, and the table `rules` for `parseRule` |
| Parser expressions | One function per expression; literals, `.`, anchors and rule calls are called inline |
| Actions and predicates | A function each, of TypeScript expressions; lambdas become arrow functions |
| Public API | `parse`, `parseRule`, `recognize` (with `-recognize`), `Node`, `SyntaxError`, `SyntaxErrors`, `marshal` |

### Where JavaScript differs from Go

The runtime hides each difference, so that results are the engine's, not merely similar:

- **Strings are UTF-16.** Positions count code points or UTF-8 bytes, so the input is decoded first: into an
  `Int32Array` of code points, or kept as UTF-8 bytes, with a table from positions to offsets in the string for node
  text (none when they coincide, as for ASCII). A string is parsed as its UTF-8 encoding (lone surrogates as U+FFFD);
  a `Uint8Array` is decoded exactly as Go decodes it (`utf8.DecodeRune`: an invalid sequence is U+FFFD of one byte),
  which `TextDecoder` does not do. In actions, string comparison orders by code point (Go's byte order) and `len`
  counts code points or UTF-8 bytes. Literals carry both their code points and their bytes, computed by the generator
  with Go's conversions. In byte mode, Go's node text holds the raw bytes even when they are not valid UTF-8, so the
  runtime keeps each invalid byte *b* in text as the lone surrogate U+DC00+*b* (as Python's `surrogateescape`
  does): text stays injective, `len` counts such a surrogate as one byte, comparisons order it as that byte, the
  S-expression form writes it as `\xNN` (as `strconv.Quote` does), and `marshal` writes it as U+FFFD (as
  `encoding/json` does).
- **Ints are 64-bit and wrap around in Go.** An int is a `number` while it is a safe integer and a `bigint` beyond
  (normalized after each operation), so ordinary arithmetic stays on numbers, equal ints are always `===` (memo keys
  compare variable values with `===`), and overflow, `MinInt64 / -1` and the like give Go's results. Division of safe
  integers by `Math.trunc` is exact; `-0` is turned into `0`.
- **JSON.** `JSON.stringify` cannot match `encoding/json` (which escapes `<`, `>`, `&`, U+2028 and U+2029 and writes
  big ints exactly), so `marshal` writes the engine's bytes; `Node.toJSON` gives the same structure for
  `JSON.stringify`. `marshal` and `toString` keep the values left to write on a stack of their own: left recursion,
  left-associative Pratt operators and `foldl` build trees as deep as the input is long without nesting calls, and
  writing them recursively overflowed the stack on trees the parse had built fine. `JSON.stringify` still recurses
  (the guide says so).
- **Unicode versions differ.** `toString` quotes text as `strconv.Quote` does, which escapes the characters that
  `strconv.IsPrint` rejects. The JavaScript runtime's Unicode properties (`\p{L}` and so on) follow its own version of
  Unicode (Node.js 24 has Unicode 16, Go 1.27 Unicode 17: thousands of code points differ), so the generator writes
  Go's table into the module instead: the lengths of the alternating runs of rejected and accepted code points, in
  base 36, about 3 KB. The table is that of the Go that generates the parser.
- **No `panic`.** Fatal errors and evaluation errors are thrown as plain objects (`Fatal`, `EvalError`, not `Error`
  subclasses, so throwing them captures no stack trace), and caught where `genrt` recovers. Results are
  `Node | null` on success and `undefined` on failure, instead of `(value, ok)` pairs.
- **The stack is small.** Rule calls recurse, as in Go, but Node.js's default stack holds only some hundreds to a few
  thousand nested rule calls. The generated code calls literals and rules inline to save frames, the runtime turns a
  stack overflow into an error (`nesting too deep: the JavaScript stack overflowed at N rule calls`), and the guide
  shows how to run the parser with a larger stack. This is the one documented way in which a generated TypeScript parser
  can reject input that the engine accepts.

### API

`parse(input, unit?)` returns `{ node, error }` instead of throwing, because a parse that recovered with `#recover`
has both a tree and errors, as Go's `(node, err)` does. Errors are `SyntaxError` and `SyntaxErrors` (subclasses of
`Error`, whose `message` is Go's `Error()`), or a plain `Error` for runtime errors in actions and too deep nesting.

`pego.GenerateTypeScript(g, start, opts...)` is a function of its own rather than an option of `GenerateGo`: the Go
generator needs a package name and supports typed values, neither of which applies to TypeScript, so an option would
leave parameters that mean nothing for one of the languages. `GenOption`s are shared: `WithRecognize` works for both,
and `WithTypes` is an error for TypeScript. The command keeps one `gen` command with `-lang go|ts`.

The module uses only erasable TypeScript (no enums, namespaces or parameter properties), so Node.js, Deno and Bun run it
without a build step, and only the ES2020 library (for `BigInt`), so it needs no DOM or Node.js types.

### Verifying identical behavior

`TestGeneratedTSParsersMatchEngine` (`internal/engine/gen_ts_test.go`) generates a module for every grammar of the
corpus of the Go generator's test (the engine's test grammars, `testdata/typed` and `examples/`), plus a grammar of ints
and strings that exercises wrapping, big ints, negative division and code-point comparison, and the nested-parentheses
grammar exactly at and beyond the nesting limit. Node.js runs every input in both units, as bytes and (when valid UTF-8)
as a string, in a worker with a 1 GB stack. For each it compares with the engine (compiled without projections, as the
Go test does):

1. the result as JSON, byte for byte (the node from `marshal`, and the error message);
2. the S-expression form, byte for byte (`toString` against `Node.String`);
3. the error of `recognize` against the engine's recognition.

Besides the inputs of the corpus, it parses every prefix of each short input and the input without each of its bytes
(variants that fail in many places, some of them not valid UTF-8). With variants of inputs up to 2,000 bytes instead
of 40, the test compares 40,128 results; that run is too slow to keep, but it passes.

It also type-checks all generated modules with `tsc --strict` plus `--noUnusedLocals`, `--noUnusedParameters`,
`--noUncheckedIndexedAccess`, `--exactOptionalPropertyTypes`, `--erasableSyntaxOnly` and others, at `--target es2020`.
The test is skipped without `node`, and the type check without `tsc`.

### Alternatives considered

- **A TypeScript VM for the bytecode ([010](010-bytecode-vm.md)).** One runtime for all grammars, loading `.pegoc`
  files, and an iterative VM would have no stack problem. But an interpreter in JavaScript is slower than generated
  functions that the JIT compiles, and the generator had all the machinery already.
- **Compiling the Go runtime to WebAssembly.** Exact by construction, but the module would carry a Go runtime, cross a
  boundary for every tree it returns, and not be TypeScript a user can read.
- **Iterative rule calls.** Would remove the stack limit, but every expression function would have to become a state
  machine; the generated code would be far larger and slower. A larger stack is easy to get where it matters.
- **Exceptions for syntax errors.** Idiomatic in JavaScript, but a recovered parse has a tree and errors at once.

## Limitations

- Typed values (`-types`) are not generated for TypeScript, nor stream or incremental parsing.
- Deep nesting is limited by the JavaScript stack (see above) as well as by the limit of 100,000 nested rule calls.
- In byte mode, Go concatenates bytes: `text($a) + text($b)` of two halves of a character is that character, while
  the runtime's two surrogates stay two surrogates. Only input that is not valid UTF-8 can tell.
- `JSON.stringify` writes ints beyond the safe integers inexactly; `marshal` does not.
