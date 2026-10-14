# Python recognizer target feasibility

This note evaluates a possible Python code-generation target. It does not define a PEGO language feature or promise compatibility with existing generated parsers.

## Current generator shape

The Go and TypeScript generators compile a grammar to PEGO's internal `Program`, then emit a standalone runtime plus generated rule functions (`internal/engine/gen.go`, `internal/engine/gen_ts.go`). Both generators embed their runtime into the generated source. Their action and predicate expressions are emitted as host-language expressions. Arbitrary Go or TypeScript expressions cannot be translated reliably into Python.

A small Python target is technically plausible if its first scope is explicit and rejects unsupported constructs. The smallest useful trial would recognize inputs for action-free grammars, with literals, character classes, any-character, sequence, ordered choice, repetition, rule references, and whole-input matching. It would not construct PEGO `Node` values or claim CST, typed-output, recovery, streaming, Operators Rule, predicate, or action parity.

## Example output

`python_recognizer_sample.py` is illustrative generated output for a grammar with an ordered choice of two literals under repetition. It is standalone and uses only Python's standard library. It has not been wired to PEGO's compiler or executed as a compatibility test.

## Decisions before implementation

1. Identify a concrete Python consumer and confirm recognizer-only output is useful.
2. If users require parse trees or typed values, define a portable action/value contract first. Translating arbitrary host-language action text is not a viable compatibility strategy.
3. Specify the supported Python version and whether the target accepts Unicode strings only or also byte input with PEGO's position-unit behavior.
4. Define generation-time errors for unsupported constructs, including source locations.

## Focused validation cases for a later prototype

- Literal success and mismatch.
- Ordered choice where the first alternative is a prefix of the second.
- Nested rule references and recursion limits.
- Repetition stopping after a failed item and rejecting zero-progress loops.
- Whole-input matching rejecting an otherwise valid prefix plus trailing input.
- Farthest-position expected-token aggregation.
- Unicode input and position behavior, once the input contract is selected.
- Explicit rejection of predicates, actions, recovery, streaming, and Operators Rules until supported.

No validation or performance result is claimed here.
