# 054. Generated Grammar Source Parser

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-13

## Summary

Provide a typed Go parser for PEGO grammar source generated from PEGO's own
complete grammar, while retaining the hand-written source parser as a
selectable implementation. Differential tests must establish equivalent
grammar ASTs and diagnostics. Paired measurements determine which parser is
the default for source-consuming APIs and commands; no speed advantage is
assumed.

## Motivation

The hand-written parser is currently the only implementation of PEGO grammar
syntax. A parser described by PEGO's own grammar could reduce duplicated
syntax logic and make grammar evolution easier to inspect, while a generated
typed Go implementation may offer a practical fast path. Retaining the manual
parser as an explicit alternative provides an independent oracle and fallback
while the generated parser's correctness, diagnostics and performance are
established.

## Goals

- Describe the complete grammar-file syntax in a canonical root `.pego` file.
- Generate a typed Go parser from that grammar without requiring users to run
  generation before building or using PEGO.
- Keep both generated and manual source parsers selectable at source-parsing
  entry points, without mutable global configuration.
- Compare parser outputs, source metadata and errors before changing defaults.
- Establish and tune the hand-written lexer/parser as a fair reference before
  selecting a default.
- Select the generated implementation only if it is measurably faster than the
  tuned manual frontend under equivalent public AST and diagnostic contracts,
  without a material supported-workload regression.

## Non-Goals

This proposal does not replace the grammar compiler, type checker, formatter,
semantic analysis, or the `Compile(*grammar.Grammar, ...)` AST entry point.
It does not make generated Go the default before differential and performance
gates pass, and it does not remove the manual parser in the first stage. It
does not claim PEGO can generate its parser without a checked-in bootstrap
artifact.

## Proposed Design

### Grammar and Generated Parser

Keep one complete, normative source grammar at the repository root. Generate a
typed Go parser that constructs a concrete source AST, including nodes such as
`File`, `Def`, `RuleExpr`, `Type`, `ActionExpr` and `Comment`. A separate adapter
converts that source AST to the existing public `grammar.Grammar` API. Keep
source positions, comments and layout on the source-AST path for formatter,
LSP and diagnostic consumers. The public source metadata contract is defined
in [Source Metadata and Declaration Comments](057-source-info-and-comment-ownership.md)
for documentation and source tools, independently of frontend selection. The
grammar must preserve
contextual words, hard-newline rules, action and predicate blocks, Operators
Rules and malformed-input recovery.

#### Struct Definition Field Separators

Fields in a Struct Type Definition Statement use semicolons or hard newlines.
Constructor fields and function arguments use commas. The source parser and
adapter must preserve field order, comments, positions and diagnostics while
checking separator parity against the manual parser. This changes source
syntax rather than field semantics in the public AST or compiled type table.

The source grammar accepts identifiers beginning with `_` at parse time.
Reserved-name checks belong to validation and compilation, so parsing such a
name succeeds while validation reports the reserved-name error. Attribute
names are lexically adjacent to `@`: both `@name` and `@name(...)` are valid,
while `@ name` is not. The root grammar's `Comments` field is always a list;
its surrounding whitespace is discarded by composing existing grammar
constructs. This local construction does not introduce general filtering or
typed-list collection semantics; those remain a separate proposal.

The checked-in generated Go source is the bootstrap artifact used during an
ordinary build. Regeneration must be reproducible and must not invoke the
parser being regenerated to construct itself at runtime. Keep the generator
and generated parser in package boundaries that avoid an import cycle through
`pego.CompileSource` or grammar compilation. A generation check fails when the
generated source is stale.

### Incremental Source Span Construction

The canonical source tree currently constructs normalized package-clause metadata
in `Package.Source`. Top-level type declarations additionally retain a
syntax-only `TypeDef.SourceSpan`: a typed body node from the `type` keyword
through the completed type specification. Leading and trailing boundary comments,
statement separators and following whitespace stay outside that span. Comments
inside a continuous type specification, including nested fields and grouping,
stay inside it. A separate typed whole-rule body production provides the parsed
name and specification. Both package clauses and type declarations use the
actual body node's range, so error recovery runs once. Speculative recognition of
a recovery-bearing body can duplicate diagnostics and must not be used to
construct its span. No inline action or untyped sequence-field lookup is required.

The internal offset adapter maps zero-based rune offsets to public one-based
`grammar.Pos` values. Its span-only update retains existing normalized ownership,
diagnostic token positions and formatter layout. Type declaration comment
ownership remains a separate construction step: a completed declaration's
boundary must explicitly supply the next declaration's leading and detached
groups. Reclassifying a raw comment list in the adapter would duplicate or move
ownership and is not part of this design.

This increment is a source-span contract, not a complete public-AST adapter or a
parser selection change. Differential span cases cover aliases, terminals,
structs, multiline groups, comments, separators, Unicode and CR/LF/CRLF. The
existing generated-consumer gate also compares typed type spans with the runtime
source tree; full diagnostic and formatting parity remain required before the
proposal can be accepted.

### Selection Surface

Expose a per-call source-parser selection for `ParseGrammar` and
`CompileSource`, and corresponding explicit selection for CLI or tooling
commands that parse source (`parse`, `compile`, `gen`, `fmt`, `lint` and LSP).
Existing calls keep their defaults. Add optional per-call source-parser
options to the existing entry points rather than separate `WithOptions` or
`WithParser` entry points. Do not use a process-wide mutable switch or an
environment variable as the only selection mechanism. `Compile` of an
already constructed `grammar.Grammar` is unaffected.

The generated parser's concrete source-AST shape is checked against the typed
nodes declared by the root grammar. Its adapter is then checked against the
public `grammar.Grammar` result expected by source-consuming APIs. The manual
parser need not construct the generated parser's source AST; compare its
public-grammar output, diagnostics and source metadata at the shared API
boundary. Preserve meaningful public source forms such as explicit `Group`
nodes, statement `Block` nodes, ordered positional attribute arguments
including null slots, named arguments, and the unified binary/unary operator
representation. Do not normalize these distinctions away to make the parsers
appear equal. Parser selection must not change the accepted language,
semantic validation pipeline, diagnostics policy or error ordering.

### Default Selection

Keep the manual parser as the initial default. When parser-performance work is
resumed, first profile and tune its lexer/parser on representative grammar
sources; do not treat its current implementation as a permanently fixed
baseline. Then compare it with the generated parser through the same source
API, including the generated source-AST-to-`grammar.Grammar` adapter. The
measured operations must produce equivalent public grammar ASTs, source
metadata and diagnostics. A direct comparison between manual `grammar.Grammar`
construction and generated concrete-source-AST construction is useful as an
initial cost observation, but it is not an equivalent-output comparison and
cannot select a winner.

Separate frontend parsing from semantic compilation and code generation.
Use repeated alternating samples on the same corpus and hardware, including
small, large, malformed and comment-heavy grammars. Select the generated
parser as default only if it is consistently faster than the tuned manual
frontend under the equivalent-output gate and has no material supported-
workload regression. Otherwise retain the manual default and the explicit
parser selector; do not claim a speed advantage from unlike AST or diagnostic
work.

## Alternatives Considered

- **Keep only the manual parser.** This avoids bootstrap and duplicate
  implementations, but leaves the grammar description and implementation as
  separate sources of truth.
- **Replace the manual parser immediately.** This is simpler after a proven
  migration, but removes the independent comparison path before grammar,
  metadata and diagnostic parity are established.
- **Generate an untyped parser and convert its generic tree.** This may be
  easier to generate, but adds allocations and conversion logic and risks
  losing typed AST and source-layout information.
- **Select a parser dynamically at runtime.** This could tailor each call, but
  adds benchmark or policy complexity to a deterministic source API and makes
  behavior harder to reproduce. Prefer one measured static default with an
  explicit per-call override.
- **Keep the display grammar in Markdown only.** This is easy to read, but
  cannot serve as a direct generator input or be checked against the source
  parser as an executable grammar artifact.

## Compatibility and Bootstrap

Existing source APIs and commands retain their defaults until the selection
is validated. An explicit manual-parser selection provides a diagnostic
fallback. Existing `.pego`, JSON grammar AST, compiled grammar and generated
parser formats remain unchanged; generated grammar parsing is only a frontend
change. The root grammar and checked-in generated Go parser must be updated
together. Bootstrap and generation checks must work from a clean checkout
without network access or a prior generated artifact outside the repository.

## Acceptance Tests

- Differentially parse every repository `.pego` file, grammar fixture and
  documentation example with both parsers. For the generated path, compare
  the concrete source AST with the root grammar's declared node structure,
  then compare its adapter output with the manual parser's public
  `grammar.Grammar` result. Preserve `Group`, `Block`, ordered positional
  arguments and null slots, named arguments, and the common operator-node
  representation. Also compare source positions, comments/layout, error
  counts, error order, locations and messages. A normalized AST may be
  compared only as a separate semantic check, never as a substitute for these
  source and public-AST checks.
- Cover valid, malformed and incomplete input; CR/LF/CRLF; nested comments;
  hard newlines and delimiters; contextual identifiers; type declarations;
  attributes and pragmas; action blocks; Operators Rules; Unicode literals and
  character classes; and reserved or invalid names. Verify that leading-`_`
  names parse but fail at the validation/compile stage, and that whitespace
  between `@` and an attribute name is rejected.
- Fuzz bounded source strings and grammar mutations, requiring identical
  accept/reject results and equivalent errors. Preserve stable partial
  results for editor recovery where supported.
- Compare formatter parse/format/parse stability and LSP diagnostics and
  source ranges using each parser selection.
- Verify clean-checkout bootstrap, deterministic generation, root Go tests,
  all parser modules, CLI commands, site examples and generated parser checks.
- Benchmark parse-only time, bytes/op and allocations on small common
  grammars, large grammars, comment-heavy files, deeply nested forms and
  malformed/incomplete editor input. First profile and optimize the manual
  frontend without changing its acceptance, public AST, source metadata or
  diagnostic behavior; record its baseline and changes. Then use identical
  inputs, a pinned Go version and repeated alternating samples for the tuned
  manual and generated parser plus adapter. Report paired ranges and compare
  only equivalent public AST and diagnostic work for selection. Measure full
  source-to-compiled-parser cost separately. Choose the default based on these
  results rather than generation novelty.

### Preliminary Frontend Baseline

An initial baseline used five alternating 200 ms samples on Go 1.27.1,
darwin/arm64 on an Apple M3 Max with `GOMAXPROCS=16`. The hand-written parser
returns the public grammar AST; the generated parser returns its concrete
source AST without the public adapter.
Diagnostic equivalence was not checked, so these timings and byte counts are
not a fair frontend comparison and do not establish a winner or default. The
medians below were measured at source revision `90b4cf7`. Each operation parses
the complete source string: `pego.ParseGrammar` for the manual frontend and
generated Go `ParseAST` in CodePoints for the typed frontend. Compilation and
code generation are outside the timed region. Small uses
`rule main = [a-z]+ #atomic`; the other inputs are `pego.pego`,
`parsers/golang/golang.pego` and `parsers/python/python.pego`.

| Grammar | Input bytes | Manual ns/op | Manual B/op | Manual allocs/op | Generated ns/op | Generated B/op | Generated allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Small | 26 | 1,451 | 2,488 | 19 | 11,086 | 50,000 | 30 |
| Self grammar | 22,964 | 1,228,660 | 2,920,062 | 8,338 | 3,776,813 | 3,530,988 | 9,322 |
| Go | 56,593 | 2,592,098 | 6,838,759 | 18,407 | 8,424,863 | 8,015,079 | 21,815 |
| Python | 71,708 | 3,573,514 | 8,854,649 | 22,515 | 10,774,236 | 11,293,620 | 28,409 |

These values are an initial reference only. Reproduce them with
[`bench/sourceparser.sh`](../../bench/sourceparser.sh); do not use them to
select a parser until the adapter and diagnostic-equivalence gates are met.
The driver generates a typed parser in a temporary module, compares declaration
counts, and alternates measurement order; it does not assert full AST or error
equality. Raw samples remain outside Git. Source-frontend tuning and default
selection are deferred behind feature development, bug fixes and general
parser optimizations; adapter and diagnostic parity can proceed independently.

## Limitations and Open Questions

This remains a proposal. The exact public selector names and whether every
editor path exposes parser selection require API review. Comment/layout
metadata and incomplete-input recovery may be the hardest parity requirements.
If the typed parser cannot preserve the current formatter and LSP contracts,
retain the manual parser as the default and narrow the generated parser's
scope before reconsidering replacement.
