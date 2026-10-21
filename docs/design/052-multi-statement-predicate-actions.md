# 052. Multi-Statement Predicate Actions

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-13

## Summary

A Predicate Action block can contain multiple statements. The block
`{ a; b; c }` has the same parsing behavior as adjacent Predicate Actions
`{ a } { b } { c }` at the same Rule Expression position. Nonempty blocks are
implemented across the supported interpreters and generated parsers; empty
blocks remain compile-time errors.

## Motivation

A grammar may need to initialize or update parser variables and then test them
without consuming input. Previously each Predicate Action contained one
expression, so a sequence of setup and tests needed adjacent brace groups. A
single block can make the ordered relationship easier to read while retaining
the behavior of ordinary PEG sequencing.

## Goals

- Define a multi-statement block as a direct shorthand for adjacent single-
  statement Predicate Actions.
- Preserve the existing evaluation, failure, variable, capture and backtracking
  behavior of each constituent Predicate Action.
- Keep the block non-consuming and valueless, with short-circuit evaluation.
- Support the same statement separators already used by action blocks.

## Non-Goals

This feature does not add Value Action blocks, change Predicate Action value
rules, add mutable variables, or create a new scope, rule, memoization point or
recovery boundary. It does not change the behavior of a single-statement
Predicate Action.

## Contract

The statements in a Predicate Action block execute in source order. Each
statement has the behavior it would have as its own single-statement Predicate
Action at that position:

```pego
rule item = { a; b; c }
```

is equivalent to:

```pego
rule item = { a } { b } { c }
```

A binding updates the current rule invocation's variable environment as
it does in a standalone Predicate Action, so a later statement and later
grammar expression can observe the binding. A term statement uses the existing
predicate rule: `false` or an evaluation error fails the parse; otherwise it
succeeds. Evaluation stops at the first failing statement. No statement
consumes input or produces a parser value.

Desugaring preserves the existing adjacent-expression CST shape. A block that
is an item of a sequence expands into adjacent Predicate Actions in that
sequence. In a single-expression position, including a capture, choice arm,
repetition or complete rule body, it becomes the grouped adjacent sequence.
Thus a grouped sequence containing only predicates can produce the existing
empty Seq value; the individual predicate statements remain valueless. This
does not create a variable, capture, rule or rollback scope.

Capture and variable visibility at each statement follows the same expression
scope as the corresponding adjacent Predicate Action. If the containing
sequence later backtracks, variable definitions made by the block are rolled
back at the same point as definitions made by the equivalent sequence of
single-statement actions. The block introduces no hidden boundary.

Statements are separated by semicolons or hard newlines. Newlines inside an
opened expression delimiter remain governed by that delimiter. Empty
statements, empty blocks and trailing-separator behavior must remain explicit
in the grammar syntax and diagnostics; this feature does not make an empty
block an epsilon predicate.

## Alternatives Considered

- **Keep one statement per block.** This is the current behavior and avoids a
  new block contract, but forces several adjacent brace groups for sequential
  setup and tests.
- **Treat the block as one Boolean expression.** This is simpler to compile,
  but does not support sequential bindings and is not equivalent to the
  existing adjacent-action form.
- **Evaluate every statement and combine results afterward.** This loses the
  short-circuit and rollback behavior of ordinary PEG sequencing and can run
  statements after a predicate that should already have failed.

## Compatibility

Existing single-statement Predicate Actions remain unchanged. Multi-statement
blocks that currently produce an unsupported-feature diagnostic become
executable; no Value Action syntax or behavior changes. Source AST, formatter,
JSON, lint and editor support must agree on the statement boundaries and
source locations.

## Testing

Tests compare a multi-statement block with its adjacent-action
form for successful and failing inputs across closure, recursive and iterative
VMs, generated Go and TypeScript. Cover binding visibility, false tests,
runtime errors, short-circuiting, backtracking rollback, captures, memoized
variable reads, cuts, recovery, repetition and left recursion. Check empty
blocks, semicolon/newline separators, comments and multiline expressions,
formatter/JSON round trips, type diagnostics and source spans. Existing
single-statement predicates must remain byte-for-byte behaviorally compatible.

## Limitations and Open Questions

Nonempty blocks are implemented. Empty blocks remain compile-time errors and
do not act as epsilon predicates. Source parsing determines empty statements
and trailing separators using action-block conventions. Any optimization must
preserve the observable equivalence to adjacent single-statement Predicate
Actions.


The full root test suite, generated parser module suite, focused LSP and engine
tests, and site tests/build passed for the integrated implementation. Independent
review found no runtime blocker.

## Default-Path Performance Control

On Go 1.27.1, darwin/arm64, Apple M3 Max, existing JSON and XML CodePoints
parse benchmarks compared main with this implementation using closure and
generated Go backends. In a five-sample main-then-feature block, the feature
medians were about 2–7% slower. In a three-sample feature-then-main block,
the feature medians were about 1–9% faster. The reversed order changed the
direction of every timing difference, so these runs do not establish a stable
change to ordinary parsing performance. They do not measure Predicate Action
block execution itself. Raw samples remain outside the repository.
