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
- Select the faster implementation as the default only when measurements are
  repeatable and no supported workload suffers a material regression.

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
LSP and diagnostic consumers; do not add those concerns to the public grammar
API solely to accommodate the generated parser. The grammar must preserve
contextual words, hard-newline rules, action and predicate blocks, Operators
Rules and malformed-input recovery.

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

### Selection Surface

Expose a per-call source-parser selection for `ParseGrammar` and
`CompileSource`, and corresponding explicit selection for CLI or tooling
commands that parse source (`parse`, `compile`, `gen`, `fmt`, `lint` and LSP).
The existing entry points keep their current signatures and delegate to a
documented default. A named parser option or sibling `...WithParser` functions
may provide the explicit selector; do not use a process-wide mutable switch or
an environment variable as the only selection mechanism. `Compile` of an
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

Keep the manual parser as the initial default. Compare both parsers on the
same grammar corpus and hardware, separating source lex/parse/AST construction
from semantic compilation and code generation. If one implementation is
consistently faster across representative files without a material regression
in small, large, malformed, or comment-heavy grammars, make that parser the
default and keep the other explicitly selectable. If results are within
measurement noise or workloads trade places materially, retain the manual
default until a clear selection policy is justified.

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
  malformed/incomplete editor input. Use identical inputs, pinned Go version,
  repeated alternating samples, and report paired ranges. Measure full
  source-to-compiled-parser cost separately. Choose the default based on these
  results rather than generation novelty.

## Limitations and Open Questions

This remains a proposal. The exact public selector names and whether every
editor path exposes parser selection require API review. Comment/layout
metadata and incomplete-input recovery may be the hardest parity requirements.
If the typed parser cannot preserve the current formatter and LSP contracts,
retain the manual parser as the default and narrow the generated parser's
scope before reconsidering replacement.
