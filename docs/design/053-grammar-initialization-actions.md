# 053. Grammar Initialization Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-13

## Summary

Add one optional grammar-level `init` block whose assignments run before each
selected start rule. The block supports multiple assignment statements, but no
other statement form. For example:

```pego
init {
    indent := 0
    nl := false
    x := 0
}
```

The initialized variables are available to the selected start rule and its
descendants for that parse invocation. Their values use the existing action
variable types (`int`, `string` and `bool`), and bindings retain the current
immutable shadowing rules. Initialization has no captures or caller-provided
variable environment. This is a proposed source-language feature; the example
does not imply current support.

## Motivation

Context-sensitive grammars often require initial values for indentation,
mode or counters. Today those values must be established inside an ordinary
rule, coupling initialization to a particular entry point and making other
start rules easy to overlook. A grammar-level initializer gives every entry
point the same explicit starting state without mutable package globals.

## Goals

- Initialize parser variables consistently before any selected start rule.
- Run the block once per parse invocation with fresh state, including when a
  parser instance is reused or called concurrently.
- Allow multiple assignments with the same separators and expression rules
  as other action blocks.
- Reject every non-assignment statement at compile time.
- Preserve the behavior of grammars that omit the initializer.

## Non-Goals

This proposal does not add arbitrary startup callbacks, mutable parser-global
state, input-consuming initialization, a second initialization phase for
Document edits, or a general-purpose module initializer. It does not change
Predicate Action or Value Action execution.

## Proposed Contract

A grammar may contain zero or one `init` declaration. It consists of a
braced, possibly multiline list of assignments, separated by the existing
action-block statement separators:

```pego
init { indent := 0; newline := false }
rule main = line*
```

The declaration may appear with the grammar's other top-level declarations;
its source position does not determine execution order. A second `init`
declaration is a compile-time error. Each statement MUST be an assignment of
the form `name := expression`; a bare expression, Value Action, or nested
block is rejected. The right-hand side uses the existing action expression
language and is evaluated in source order, so an assignment may read a
variable initialized earlier in the same block.

Execute the initializer exactly once per parse invocation before entering the
selected start rule. This includes every `Document.Parse` call, even when its
memoized result can be reused, each `ParseStream` invocation, recognition,
arbitrary start-rule selection, and grammars loaded from saved or generated
representations. Its definitions form the initial variable environment for
that rule and are visible to descendants under the existing nearest-definition
lookup rules. The state is fresh for every parse and is never stored on a
shared parser as mutable data. Parsing with no `init` block starts with the
existing empty environment.

Initialization consumes no input and produces no parse value. An error while
evaluating an initializer expression aborts the parse before the start rule
runs and is reported as an action evaluation error; it is not a syntax error,
does not enter grammar recovery, and cannot be backtracked. The implementation
must not expose a partially initialized environment to a start rule. A failed
`Document.Parse` must leave the Document reusable and must not retain partially
initialized state or caches that could affect its next call.

## Alternatives Considered

- **Initialize in the start rule.** This works for one entry point, but each
  alternate start rule must repeat the setup and can accidentally start with
  different state.
- **Initialize in every rule that needs a variable.** This duplicates work,
  shadows caller values and does not provide one grammar-wide starting
  environment.
- **Use parser options or a callback.** This moves grammar semantics into host
  code, creates a different initialization contract for each API and weakens
  portability to generated parsers.
- **Use package-level mutable variables.** This would make results depend on
  parser reuse and call order and would be unsafe for concurrent parses.

## Compatibility

Grammars without `init` retain their behavior. Older implementations reject
the new declaration as unknown syntax. Source parsing, formatting, AST/JSON
representation, compilation, saved grammars, generated Go and TypeScript, and
the CLI must agree on initializer order and diagnostics. Initialization must
not add mutable global state to any backend.

## Testing

Test every ordinary and explicitly selected start rule, including recognition,
stream invocations, saved grammars and generated parsers. For Documents, test
that initialization runs on every call even when parsing reuses memoized
results, and that a failed initializer leaves the Document safe to reuse.
Verify per-call reset, reentrancy and concurrent use; source order and
references to earlier assignments; assignment-only validation; duplicate
declarations; runtime
errors before the start rule; and absence of input consumption or parse
values. Compare closure, recursive and iterative VMs, generated Go, TypeScript,
Document reparsing and stream entry points. Include formatting/JSON round
trips and source locations for malformed blocks.

## Limitations

This remains a proposal. Every source and compiled entry point must preserve
the once-per-invocation contract; no initializer result is cached across
invocations.
