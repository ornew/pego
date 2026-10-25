# 037. Replayable Edit Fuzzing

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add a Go API and `pego edit-fuzz` command that exercise arbitrary grammars against `Document` by applying deterministic edit-operation sequences and comparing each checked state with a fresh parse. A failure is saved as a versioned event log containing the exact input and replacements as bytes, so replay does not depend on a random-number implementation or future generator choices. The tool also minimizes a failing sequence while preserving a stable failure signature.

Illustrative saved input:

```json
{
  "version": 1,
  "grammar_sha256": "…",
  "grammar_normalization": "none",
  "start": "main",
  "backend": "bytecode-iterative",
  "unit": "codepoints",
  "seed": 41,
  "text_encoding": "base64",
  "initial_base64": "YcOp",
  "operations": [
    {"kind": "edit", "start": 1, "end": 2, "text_base64": "eA=="},
    {"kind": "undo"},
    {"kind": "redo"},
    {"kind": "checkpoint"}
  ]
}
```

Text and replacement payloads are base64-encoded raw bytes; edit offsets use the selected unit and refer to the text state immediately before the operation.

## Implementation

Package [`editfuzz`](../../../editfuzz) and `pego edit-fuzz` implement this design; the guide is
[fuzzing edits](../../guide/sampling-and-fuzzing.md#fuzzing-edits). The implementation settles the open names and refines some
rules:

- The API is `editfuzz.Fuzz(src, Options)`, `Replay(src, *Sequence)`, `Minimize(src, *Sequence, maxAttempts)`,
  `Save` and `Load`. `Fuzz` returns a `*Failure` holding the sequence, its saved path and the minimized sequence;
  `Replay` returns a `*Mismatch` or an `*InvalidError`. `Options` also has `Sequences`, `Minimize` and
  `MaxAttempts`, which the CLI exposes as `-sequences`, `-minimize` and `-max-attempts`.
- Units are written `codepoints` and `bytes`, and backends `closure`, `bytecode` and `bytecode-iterative`, the names
  of the other `pego` commands. `-backend` and `-unit` take comma-separated lists and default to all of them.
- Undo and redo do not apply the inverse of the recorded edit. Undo restores the observed text with the smallest
  single edit on character boundaries, found by a common prefix and suffix of whole characters. Such an edit
  always reproduces the exact bytes, so there is no rebuild fallback: a Document that ends with other text is a
  failure. This keeps every undo incremental, including after an edit joined bytes into a character or CodePoints
  normalization changed the replacement.
- After every edit, undo and redo, `Document.Text()` must equal the expected text: the splice of the replacement
  in Bytes, and the splice of the replacement with each invalid byte replaced by U+FFFD in CodePoints. The fresh
  parse reads `Document.Text()`, so without this check a wrong text would go unnoticed.
- A failure is a `*Mismatch` whose category is `value`, `error`, `text` or `panic`. `text` also covers an undo or
  redo edit that the Document rejects, which cannot be the sequence's fault. A panic is a failure even if both
  parses panic alike; its path, `panic.edit`, `panic.document`, `panic.fresh` or `panic.both`, keeps minimization
  from turning an incremental failure into a crash of every parse. The mismatch holds the two differing values at
  its path, not the whole results.
- Results are compared through their JSON encoding, which replaces invalid UTF-8 with U+FFFD; strings of the tree
  that are not valid UTF-8 are therefore also compared quoted, under `value_invalid_utf8` (category `value`),
  after the value itself, so that Bytes results differing only in invalid bytes differ.
- Decoding requires exactly the fields of the format, with case-sensitive names and no duplicates, and a lowercase
  hex grammar hash.
- One sequence in eight starts from initial texts joined into about 6 KiB, so that Documents also use the
  representation they choose for texts of 4 KiB or more.
- Failure files are named after a hash of their contents and created with a hard link from a synced temporary file,
  or exclusively where hard links are unsupported, so an existing file is never replaced; saving identical
  contents returns the existing file. Writing validates a sequence as reading does. `pego edit-fuzz`
  requires `-save-failures` with `-minimize` when it generates sequences, and rejects generation flags with
  `-replay`.

## Motivation

`internal/engine/document_test.go` already compares randomized incremental edits with fresh parses, while `sample` supplies seeded grammar-generated inputs. These checks establish useful foundations but do not let a grammar author save and replay a failing edit sequence through the CLI. `Document` reuses mutable nodes and has no public undo operation, so a harness must compare values before a later parse can move reused nodes and must define how its own undo/redo stack operates. See [019](019-incremental-document-and-tree-tooling.md), [`Document`](../../../internal/engine/document.go), and the [sample package](../../../sample/sample.go).

A seed alone is not a durable reproducer: changes in generator logic, corpus ordering or Go's random implementation can change the sequence. Persisting exact edit operations makes replay independent of those changes. This design defines an edit event log, not an ordinary single-input expected-result fixture.

## Goals

- Let grammar authors fuzz a selected start rule from corpus bytes or seeded generated text.
- Compare `Document` with a fresh parse at explicit checkpoints, including values, spans and errors.
- Save exact failing operations and metadata, then replay them without random generation.
- Minimize the operation sequence and replacement bytes while preserving the same failure class.
- Exercise closure, recursive bytecode and iterative bytecode in both byte and rune position units.

## Non-Goals

- No change to parse or edit semantics, no new incremental storage and no claim that fuzzing proves equivalence for all inputs.
- No serialized parser-module format, external property-testing dependency or generated-parser support.
- No assumption that an RNG seed produces identical cases across releases; saved bytes and operations are authoritative.
- No benchmark or performance gate as part of the fuzz command.

## Design

### Public Behavior and Contracts

Provide a small `sample`-adjacent Go API and the `pego edit-fuzz` command. Package placement is open to a prototype; the proposed entry point is:

```go
type EditFuzzOptions struct {
    Corpus       [][]byte
    Seed         uint64
    Start        string
    Backend      Backend
    Unit         Unit
    Operations   int
    SaveFailures string
}
func FuzzEdits(grammarSource []byte, opts EditFuzzOptions) error
func ReplayEdits(grammarSource []byte, sequence EditSequence) error
func MinimizeEdits(grammarSource []byte, sequence EditSequence) (EditSequence, error)
```

The CLI accepts `-g grammar.pego`, `-s start`, `-backend`, `-unit byte|rune`, `-seed`, `-corpus`, `-operations`, `-save-failures`, `-replay` and `-minimize`. Corpus files are read as bytes; absent an explicit corpus, seeded `sample` output can provide initial text. Corpus paths are sorted before seeded selection. A replay file takes precedence over generation. Its `grammar_sha256` is computed over the exact grammar source bytes before parsing, with `grammar_normalization: "none"`; this detects accidental source mismatch. The Go entry points also take exact grammar source bytes, verify the saved hash before replay or minimization, and compile their own parser with the recorded options. A precompiled `Parser` is insufficient provenance because it does not retain the original source; formatting its AST cannot reconstruct the source hash. The hash does not make replay safe for untrusted grammars.

An `EditSequence` contains schema version, raw grammar hash and normalization metadata, start rule, backend, position unit, seed for provenance, base64-encoded initial bytes and ordered operations. Edit replacements are also base64 raw bytes, preserving invalid UTF-8 exactly through JSON. In byte mode offsets count bytes and must be valid character boundaries under the existing `Document.Edit` contract. In rune mode offsets count the parser's decoded code points, including its documented handling of invalid UTF-8 bytes. Each edit's offsets refer to the current text immediately before that event.

Version 1 operations are `edit`, `undo`, `redo` and `checkpoint`. The initial state is checked once. Edits can be grouped before a `checkpoint`; only checkpoints compare the current document parse with a fresh parse, making multi-edit-before-parse histories explicit in the saved sequence. Undo and redo change the current text using the fuzzer's own history stack. The event log records those operations, not hidden random choices.

Raw event payloads record input provenance, while the history and differential oracle track the actual bytes returned by `Document.Text()` after construction and each accepted edit. In CodePoints mode, malformed initial text and replacements normalize to decoded code points; raw payload bytes must not be spliced into an oracle mirror or treated as the resulting document state.

For an edit, the harness saves the prior observed document bytes and inverse edit in its undo stack. It applies undo through `Document.Edit` when the inverse range is a legal boundary in the current text. If joining bytes has made the inverse range illegal (for example, the edit formed a valid multibyte rune across an old boundary), undo rebuilds a fresh `Document` from the saved prior observed document bytes and options. That fallback restores the requested state but does not claim to test incremental invalidation for an impossible inverse edit. Undo restores the prior observed document state, not the raw input provenance. Redo reapplies the saved forward edit; redo history is cleared by a new edit. Undo/redo without a corresponding stack entry is an invalid sequence.

At each checkpoint, compare successful values and full tree spans, recovered `SyntaxErrors` in order and fields, and ordinary syntax-error fields rather than formatted strings alone. Snapshot or structurally compare the whole result before another parse can move shared nodes. Compare errors by concrete kind and position, line, column, expectations and messages. Runtime errors that cannot be structurally compared use a documented type-and-message fingerprint.

Failure minimization removes operation ranges, then shortens replacement bytes and simplifies valid edit boundaries while preserving explicit checkpoints and a stable failure fingerprint: mismatch category, backend, unit and first differing result/error location. Invalid candidate sequences are discarded rather than counted as the target failure. Apply a configurable attempt ceiling. Save original and minimized sequences atomically and never overwrite corpus files.

### Implementation and Invariants

Compile `Parser` from the supplied exact grammar source and build on `NewDocument`, `Document.Edit`, `Document.Parse`, `Document.Text` and `Node.Clone`. Fresh oracle parses use identical start/backend/unit options and the current `Document.Text()` bytes, preserving the same decoding semantics. A result is snapshotted before subsequent document parses because incremental reuse may move nodes in earlier results. Positions in both outputs are compared in the declared unit.

Generate offsets at valid boundaries for the active unit, deliberately including start, end, EOF, zero-width insertions/deletions and multibyte boundaries. Generate replacement payloads as arbitrary bytes, including malformed UTF-8, and encode them losslessly. Persist a failure sequence before minimization so interruption leaves a reproducer.

### Compatibility and Backend Support

This is additive tooling around the existing Go `Document` API. Closure, recursive bytecode and iterative bytecode are tested separately; the harness compiles the source with the metadata needed by each selected backend and reports unavailable backends rather than silently skipping them. Both byte and rune units are required. The CLI recompiles the grammar and does not change saved module formats. TypeScript and generated parsers do not expose `Document` and are outside this design.

An edit sequence is an ordered state-transition log with a differential oracle. An ordinary parse fixture describes one input and expected output; the two formats have distinct purposes and should not be merged.

### Implementation Sequence

1. Implement in-memory edit events, explicit checkpoints and a fresh-vs-document comparator in Go tests, reusing existing randomized document coverage.
2. Add the versioned lossless byte schema, replay, undo/redo rules and atomic failure persistence; validate hashes, units, boundaries and malformed events.
3. Add seeded corpus/edit generation and the CLI, reporting seed and exact saved artifact path on failure.
4. Add bounded minimization and stable failure fingerprints; verify minimized files replay to the same mismatch without changing originals.
5. Run saved corpora and generated sequences through every available backend and both units; report AST-only backend limits explicitly.

## Alternatives Considered

- **Report only a random seed.** It is compact, but generator or RNG changes can produce a different history. Keep the seed for provenance and save the exact bytes and operations for replay.
- **Keep differential fuzzing only in repository tests.** It is easy to maintain but does not give grammar authors a portable reproducer. Reuse the comparator in a public API and CLI.
- **Use only grammar-generated valid inputs.** They do not cover malformed or editor-intermediate text. Accept arbitrary corpus bytes and keep generation optional.
- **Reuse the ordinary parse-fixture schema.** One input/output expectation does not describe edit, undo, redo or checkpoint transitions. Keep the event log separate.
- **Undo every edit by applying its inverse.** This is incremental when the inverse boundaries remain legal, but cannot restore bytes by editing inside a newly formed valid rune. Rebuild from the saved exact byte checkpoint only for that case and state the lost incremental coverage.
- **Compare only final text after a sequence.** This misses transient stale-cache errors that later edits can hide. Check at explicit events and require undo/redo to restore observed document states.

## Testing

Use a small synthetic grammar corpus plus saved regressions covering success, failure, recovered errors, left recursion, lookahead dependencies and long repetition resumption. For each backend and unit, compare values, spans, syntax errors and runtime-error fingerprints at checkpoints. Include Unicode boundaries, malformed UTF-8 round trips through JSON, empty input, edits at EOF, grouped edits before a checkpoint, no-op edits, CodePoints normalization and observed-state undo/redo, inverse-boundary fallback, unavailable backends, exact-source hash checks for both Go and CLI entry points, wrong grammar hash, malformed JSON/schema versions, invalid undo/redo and interrupted persistence.

Run a fixed seed twice and compare generated sequence bytes. Replay a saved sequence in a fresh process and compare every checkpoint. Test minimization on deliberately injected mismatch categories, enforce the attempt ceiling, and replay original and minimized artifacts. Verify failure writes are atomic and corpus files are unchanged. A bounded CI fuzz smoke test supplements, but does not replace, saved regression sequences.

## Performance and Results

This is correctness tooling rather than an optimization. Measure generation/replay throughput only to choose practical CI limits; keep parser time separate from comparator and minimizer work. Use fixed corpora and operation counts, report allocations and maximum sequence size, and do not treat fuzz throughput as parser performance. Raw artifacts stay in a caller-selected local directory or CI artifact, not in the source tree unless promoted as an ordinary regression fixture under its own format.

## Limitations and Open Questions

Caller-provided instrumentation or extensions can observe different execution counts for fresh and document parses. Keep those observers out of the value/error oracle; the tool compares returned values and errors rather than callback invocation counts. The fingerprint must be stable enough for minimization without conflating distinct failures. The package location and CLI name can be finalized after an API prototype; neither changes the lossless event-log contract.
