# 056. Simplified Grammar Syntax View

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-14

## Summary

Provide a read-only, simplified syntax view derived from a complete ZeroParse
grammar. The view removes value-construction actions, result-type annotations,
metadata attributes and recovery directives while preserving Predicate
Actions, recognition predicates and non-recovery pragmas such as `#repeat`,
`#atomic` and `#stream`. Keep the existing full formatter unchanged. Expose the view through
`grammar.FormatSyntax(g) string` and `zp fmt -syntax`, and generate a
documentation page that displays both full and simplified source from the
canonical root grammar.

## Motivation

The full grammar is the executable source of truth, but construction details
and diagnostics metadata can obscure its recognition structure for readers.
A separately rendered view can make the language's rules easier to scan while
remaining mechanically derived from that source. Maintaining a second grammar
by hand would drift; rewriting or mutating the original grammar to produce the
view would risk changing compilation and formatter behavior.

## Goals

- Produce a deterministic, read-only simplified syntax string from a grammar
  without mutating its AST.
- Keep `grammar.Format` as the loss-preserving full formatter.
- Preserve Predicate Actions, recognition predicates and pragmas that shape
  recognition or input consumption, including `#repeat`, `#atomic` and
  `#stream`.
- Remove value-construction actions, associated result types, metadata
  attributes and recovery directives from the simplified view.
- Generate a single documentation page containing both full and simplified
  versions of the canonical root grammar.
- Keep the generated page within the existing Markdown publication flow.

## Non-Goals

This does not create a second executable grammar, change parsing or compilation,
or guarantee that the simplified view accepts exactly the same inputs when a
predicate depends on values or types produced by construction actions. It does
not replace the canonical root grammar or change the output of `grammar.Format`.

## Design

### Simplification Contract

Treat simplification as a projection over the grammar AST. It must not modify
the input grammar, and repeated calls must return identical output. The first
version removes:

- Value Actions that construct parse results, together with result-type
  annotations that exist only to describe those values.
- `@` metadata attributes, including custom diagnostic text and expectation
  labels.
- `#recover` directives, which affect error recovery and diagnostics rather
  than the basic rule structure.

Retain Predicate Action statements, recognition predicates, and non-recovery
pragmas, including `#repeat`, `#atomic` and `#stream`. They can change which
inputs a rule accepts, how much input it consumes, or the boundaries at which
it evaluates; dropping them would make the view misleading. Remove Value
Actions: whole-rule `=>` actions, operator-entry actions, inline Value Actions,
and default action markers. Remove result-type declarations/annotations,
metadata `@` attributes, and `#recover` wrappers. Preserve rule names,
alternatives, sequence order, character classes, literals, captures that
clarify structure, comments not attached to elided syntax, and Operators Rule
precedence/associativity.

Value-dependent predicates are a boundary case. A predicate may inspect a
value or type created by a Value Action. The simplified view still preserves
the predicate expression but removes its construction dependency, and thus
cannot promise recognition equivalence for such grammars. Mark this as a
limitation in the API and generated page; readers should use the full grammar
when predicate behavior depends on constructed values or result types.

### Go API and CLI

Keep `grammar.Format(g)` unchanged as the full, loss-preserving source
formatter. `grammar.FormatSyntax(g) string` returns the simplified view
without mutating the input AST. It uses the canonical formatter rules where
they remain applicable.

The `zp fmt -syntax` mode writes the simplified view to standard output.
It rejects combining the mode with `-w` or `-l`; it is output-only. Ordinary
`zp fmt`, `zp fmt -w` and `zp fmt -l` continue to use the full formatter.

### Generated Documentation

Use `spec/language/syntax.md.tmpl` as the Markdown template, with one fenced-code
placeholder for the full grammar and one for the simplified view. Generate
`spec/language/syntax.md` from the exact root `zp.zp` bytes for the full section and
from the parsed grammar plus the same projection used by the Go API for the
simple section. `go generate ./spec/language` refreshes the page deterministically.
Keep the result as ordinary Markdown so the current site
publisher, navigation and code highlighting continue to work. State that the
simplified view is for readability and may not preserve value-dependent
recognition.

The canonical root grammar's current predicates depend only on parser mode,
not on constructed values. Its simplified view can therefore be checked for
acceptance parity over the valid root-grammar corpus. This is a focused corpus
gate, not a general guarantee for arbitrary grammars.

## Alternatives Considered

- **Maintain a separate presentation grammar manually.** This allows editorial
  simplification, but duplicates the language definition and is likely to
  become stale. Prefer deriving the view from the executable grammar.
- **Remove predicates and pragmas.** This produces a more compact outline,
  but can change the accepted language, repetition bounds and input-consuming
  behavior. Do not use this as the default simplified view.
- **Preserve every action, type, attribute and recovery directive.** This is
  equivalent to the full grammar and does not improve readability. Keep the
  full formatter available for this complete representation.
- **Rewrite or mutate the source AST before formatting.** This risks changing
  caller state and can affect later compilation. Use an immutable projection
  or formatter mode instead.
- **Use a generated HTML include or custom site component.** This introduces
  a separate publication path. Prefer generated Markdown and the existing site
  pipeline unless it cannot display both sections accessibly.

## Compatibility and Backend Support

The projection and API are additive. Existing grammar parsing, compilation,
serialized grammar forms and `grammar.Format` output remain unchanged. The CLI
mode must be explicit and must never overwrite a source file. The generated
documentation is derived output and should be checked for staleness by the
normal generation test.

## Testing

- Verify projection does not mutate the source AST, including comments,
  attributes, action blocks and source positions; repeated calls are stable.
- Assert that `grammar.Format` output remains byte-for-byte unchanged for
  representative grammars.
- Use grammars with Predicate Actions, predicates, `#repeat`, `#atomic`,
  `#stream`, Value Actions, result types, `@error`, diagnostic labels and
  `#recover`; confirm only the specified constructs are removed and
  recognition-relevant syntax is retained.
- Check acceptance parity for the current root grammar corpus; include a
  separate value-dependent predicate case that documents the non-equivalence
  limitation rather than asserting parity.
- Include a predicate dependent on a constructed value and verify the API and
  generated page communicate that recognition equivalence is not guaranteed.
- Verify `zp fmt -syntax` output, standard full formatting, and rejection of
  `-syntax -w` without modifying input files.
- Regenerate the Markdown page twice and require identical output; check both
  sections against the Go API and run the ordinary site Markdown/link checks.

## Limitations and Open Questions

- The simplified view is not guaranteed to be an executable or equivalent
  grammar when predicates depend on constructed values or result types.
- Preserve comments that remain attached to visible grammar; comments inside
  elided actions or other removed constructs may be omitted.

The API, CLI, generated-page staleness and projection contracts have focused
test coverage. Local root-module tests, site tests and the site link-checking
build pass. These checks do not claim remote CI or deployed behavior.
