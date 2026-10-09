# 021. Structural Validation of Grammar ASTs

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

`grammar.Validate(g)` reports a structural defect as a
`*grammar.ValidationError` with an AST path and nearest known source position.
JSON loading and the compiler share this check. Malformed required children,
negative positional references and reversed repetition bounds return errors
before analysis can dereference them.

## Motivation

Grammars can enter through source, JSON or hand-built public ASTs. Source
parsing already rejects malformed syntax, but JSON decoding could accept null
struct fields, omitted expression children and negative positional references.
Compilation could then panic. JSON decoding also accepted a valid document
followed by another value or garbage, and integer conversion could silently
truncate an `int32` field.

A shared structural check avoids different assumptions at each entry point.
Tools can run the same check before calling the formatter, whose signature
returns a string rather than an error.

## Goals

- Reject malformed structure before compiler analysis, even when type checking
  is skipped or an AST-bearing saved grammar is reconstructed.
- Return reusable paths and positions without mutating the grammar.
- Preserve existing empty collections, shared subtrees and negative unbounded
  repetition maxima.
- Detect pointer cycles without a recursive validator or an arbitrary depth cap.

## Design

### Public contract

`Validate(*Grammar) error` returns the first structural error. The concrete
`ValidationError` exports `Path`, `Pos` and `Msg`. Paths start at `$`, use
JSON field names and zero-based array indexes. The nearest known source
position is retained; unknown positions remain zero. Traversal is deterministic,
checking each node's bounds before visiting children in declaration order.

Required children and collection entries must be nonnil. Optional interface
children may be genuinely nil; typed nils are rejected. Supported node types
are the concrete AST types in package `grammar`; embedding them does not add a
new supported node type. A positional reference must be nonnegative. A
repetition's minimum must be nonnegative and its maximum must be negative
(unbounded) or at least the minimum.

Name resolution, type checking, attributes and Pratt semantics remain compiler
checks. Source layout is excluded. Successful structural validation is not a
guarantee that a grammar compiles, formats to valid source or fits a resource
budget. Existing source-language syntax is unchanged.

### Implementation and entry points

An explicit traversal stack and active/completed node states distinguish
pointer cycles from shared subtrees. Leaf nodes need no cycle bookkeeping.
Paths and source-position lookup are materialized only on failure. This
validation adds work proportional to the traversed AST and child edges;
the stack and state map are local to each call.

`UnmarshalJSON` requires EOF after one decoded document, checks integers
against their destination width, then validates the decoded grammar.
Unknown-field and duplicate-key behavior remains permissive. The compiler
validates before enumerating definitions or running analysis and translates
structural failures into its existing error list, retaining path and position.
Go, typed Go and TypeScript generation already use this compiler.

Format and MarshalJSON retain their signatures and trust their AST input.
Their callers may use Validate first. There is no serialized-format version
change or change to generated runtime behavior.

## Alternatives considered

- **Only guard the reported compiler dereferences.** Small fixes would leave
  other required children and pre-compilation tools exposed to the same class
  of failures.
- **Duplicate JSON and compiler checks.** This avoids a public function but
  creates two structural contracts to maintain. The public AST already permits
  tools to construct grammars, so one reusable function is appropriate.
- **Use a JSON round trip to validate ASTs.** This loses source positions,
  allocates serialized data and cannot safely traverse pointer cycles.
- **Reject every shape the source parser cannot emit.** Empty sequences and
  choices already have behavior for hand-built ASTs. Validation preserves
  those contracts rather than introducing a source-normalization requirement.
- **Make JSON strict by default.** Rejecting unknown fields or duplicate keys
  changes compatibility. An optional strict decoder needs a separate policy
  and remains unimplemented.

## Testing

Regressions cover every required child family in JSON and hand-built ASTs,
typed nils for all registered interface nodes, optional typed nils, shared
subtrees, expression/type/term cycles, deterministic diagnostics, position
fallback, 50,000 nested nodes, integer overflow and trailing JSON. External
embedded node values and pointers must return unsupported-node errors.
Compiler tests cover skipped type checks, saved-AST reconstruction and Go,
typed Go and TypeScript generator entry points.

Existing parser, generated-code and saved-grammar suites check valid inputs.
Preparation measurements compare source compilation and saved grammar loading;
correctness-related performance effects are recorded in the implementation
commit, without an optimization-log entry.

## Limitations and open questions

The validator reports one structural defect per call. It does not validate
Unicode class scalars/range semantics or offer semantic validation without
compilation. Formatting layout and resource budgets remain separate concerns.
A strict JSON intake API, including unknown fields and duplicate-key policy,
requires its own design. Bytecode compilation canonicalizes negative unbounded
maxima to -1 while preserving the caller and saved AST's original field; see
[wide repetition bounds](020-wide-repetition-bounds.md).
