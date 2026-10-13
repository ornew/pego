# 052. Multi-Statement Predicate Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-13

## Summary

Allow a Predicate Action block to contain multiple statements. The proposed
block `{ a; b; c }` has the same parsing behavior as the adjacent Predicate
Actions `{ a } { b } { c }` at the same grammar-expression position. This is a
language proposal; current multi-statement Predicate Action blocks are rejected
by semantic analysis.

## Motivation

A grammar may need to initialize or update parser variables and then test them
without consuming input. Today each Predicate Action contains one expression,
so a sequence of setup and tests must be written as adjacent brace groups. A
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

This proposal does not add Value Action blocks, change Predicate Action value
rules, add mutable variables, or create a new scope, rule, memoization point or
recovery boundary. It does not change the behavior of a single-statement
Predicate Action.

## Proposed Contract

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

An assignment updates the current rule invocation's variable environment as
it does in a standalone Predicate Action, so a later statement and later
grammar expression can observe the binding. A term statement uses the existing
predicate rule: `false` or an evaluation error fails the parse; otherwise it
succeeds. Evaluation stops at the first failing statement. No statement
consumes input or produces a parser value.

Capture and variable visibility at each statement follows the same expression
scope as the corresponding adjacent Predicate Action. If the containing
sequence later backtracks, variable definitions made by the block are rolled
back at the same point as definitions made by the equivalent sequence of
single-statement actions. The block introduces no hidden boundary.

Statements are separated by semicolons or hard newlines. Newlines inside an
opened expression delimiter remain governed by that delimiter. Empty
statements, empty blocks and trailing-separator behavior must remain explicit
in the grammar syntax and diagnostics; this proposal does not make an empty
block an epsilon predicate.

## Alternatives Considered

- **Keep one statement per block.** This is the current behavior and avoids a
  new block contract, but forces several adjacent brace groups for sequential
  setup and tests.
- **Treat the block as one Boolean expression.** This is simpler to compile,
  but does not support sequential assignments and is not equivalent to the
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

Acceptance should compare a multi-statement block with its adjacent-action
form for successful and failing inputs across closure, recursive and iterative
VMs, generated Go and TypeScript. Cover assignment visibility, false tests,
runtime errors, short-circuiting, backtracking rollback, captures, memoized
variable reads, cuts, recovery, repetition and left recursion. Check empty
blocks, semicolon/newline separators, comments and multiline expressions,
formatter/JSON round trips, type diagnostics and source spans. Existing
single-statement predicates must remain byte-for-byte behaviorally compatible.

## Limitations and Open Questions

This remains a proposal. The exact diagnostics for empty statements and
trailing separators should match existing action-block conventions. Any
optimization must preserve the observable equivalence to adjacent
single-statement Predicate Actions.
