# 047. Character-Class Display Names

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Allow a grammar author to give a character class a stable, human-readable name for syntax-error expectations without changing which characters it accepts. The proposed spelling attaches a metadata attribute to the existing class expression:

```pego
def digit = (?0-9)#display(name="decimal digit")
```

When this class is an expected item in a syntax error, the diagnostic shows `decimal digit` instead of the mechanically formatted class. Matching, captures, spans, and recovery behavior remain unchanged.

## Motivation

Character classes are represented by `grammar.CharClass` as ranges and a negation flag. Their expectation text is currently derived from the class expression and is used by the closure compiler, bytecode, and generated Go and TypeScript runtimes. This accurately describes the syntax but can produce noisy or domain-insensitive errors for classes such as an identifier-start set.

The grammar already supports postfix attributes such as `#error` and `#recover`; unknown attribute names are rejected. A metadata attribute fits that extension point and avoids assigning a new meaning to existing class syntax or introducing reusable named-set declarations.

## Goals

- Let authors replace a class's displayed expectation label with an exact string.
- Preserve the class's matching and parse-tree semantics.
- Keep diagnostics deterministic across interpreters, generated parsers, and serialized grammars.
- Preserve existing class descriptions when no display name is present.

## Non-goals

- Defining named or reusable character classes.
- Changing Unicode range validation, negation, matching, or formatter rules for classes.
- Replacing `#error` custom messages or changing its precedence.
- Adding localized diagnostic catalogs or runtime-dependent labels.

## Design

### Public behavior and contracts

Introduce `#display(name="...")` as metadata on a character class. Require exactly one `name` argument containing a non-empty string literal. Reject duplicate `#display` attributes on the same class and reject CR, LF, U+0085, U+2028, and U+2029 so one expected item remains one line. Preserve all other Unicode scalar values exactly; do not normalize or reinterpret them. The recommended initial validation accepts the attribute on a class through grouping and a postfix attribute chain, but rejects it when the underlying expression is not a character class. In particular, define and test attribute ordering: `#display` may name a class whose expected item is otherwise affected by `#error`; an outer `#display` whose immediate expression is a semantic wrapper such as `#error` is rejected rather than silently searching through it.

The display string replaces the class's normal textual form only where that class contributes an expected item to a syntax error. It does not alter matching identity, whether the expression succeeds, its span, capture behavior, or the parser's farthest-failure position. If an enclosing `#error(message=...)` supplies a custom message, existing `#error` behavior continues to take precedence. The expected-item table is keyed by exact description strings, so distinct classes with the same display label produce one displayed expected item under the existing deduplication rule. This changes only the diagnostic description, not class or expression identity.

Example:

```pego
def identifier = first:(?a-zA-Z)#display(name="letter") rest:(?a-zA-Z0-9)*
```

The attribute names the first class's expectation, not the entire `identifier` rule or the `rest` class.

### Implementation and invariants

Represent the feature as validated metadata on the existing `grammar.Attributed` node rather than adding a display field to `CharClass`. This keeps grammar JSON additive through the existing attribute representation and preserves the class AST shape. Add validation for the underlying class and required argument, including the documented interaction with neighboring attributes. The grammar formatter already emits postfix attributes; it must preserve the label through parse-format-parse round trips.

When compiling a class, derive the expectation description from the display attribute if present, otherwise use the current `charClassString` result. Keep the normal class matcher and matcher identity untouched. The new text is a diagnostic descriptor; exact-text expected-item interning and deduplication continue to apply. This single description path must feed closure compilation, recursive and iterative bytecode, and generated Go/TypeScript output so every backend reports the same label. Error sorting and `#error` composition remain governed by existing rules.

Treat `#display` as transparent metadata everywhere outside expectation construction. It must not change first-character guards, scan/repetition lowering, recognition, type or value analysis, capture scopes, left-recursion analysis, or optimizer eligibility. Optimized paths that report failures directly, including first guards and scanners, must still select the display label.

### Compatibility and backend support

Grammars without `#display` retain their current syntax and diagnostics. New grammar source fails validation on older implementations because unknown attributes are rejected; this is a new opt-in grammar feature rather than a silent behavior change. The JSON AST uses `Attributed` and `Attribute` already, so no new top-level JSON field is required. Confirm that strict grammar JSON validation accepts the existing attribute shape and that bytecode serialization retains the resulting description string; if a format revision is needed, gate it explicitly rather than changing old bytecode interpretation.

Update `spec/attributes.md`, character-class documentation, formatter cases if needed, and LSP built-in attribute completion/hover. The LSP should explain that this is diagnostic metadata and that `#error` remains the way to provide a full custom failure message.

### Implementation sequence

1. Add grammar validation, attribute-order rules, and source-format round-trip support.
2. Route class expectation description selection through the shared compiler/generator helper without changing matcher IDs.
3. Add parity tests for ordinary and optimized failure paths across closure, both bytecode VMs, generated Go, and TypeScript.
4. Update the attribute specification and LSP built-in completion/hover.

## Alternatives considered

- **Add a name field directly to `CharClass`.** This makes compiler access simple, but duplicates metadata alongside the existing attribute mechanism and requires a distinct AST JSON field and formatter path.
- **Use a dedicated named-class declaration.** A declaration could support reuse, but it introduces symbol lookup, forward-reference, and composition rules beyond the requested diagnostic label.
- **Use `#error(message=...)`.** This already replaces an error message, but it applies custom failure text rather than naming one expected class; it can also obscure other expected alternatives. `#display` is narrower.
- **Infer a label from a comment or rule name.** Such inference is unstable under refactoring and cannot reliably distinguish a class's role from its containing rule.

## Testing

Verify that labeled and unlabeled classes accept identical inputs and produce identical node types, captures, and spans. Cover positive and negated classes, ranges, Unicode scalars, malformed UTF-8, and class failure after a successful prefix. Compare error position and expected-item ordering across closure, recursive bytecode, iterative bytecode, generated Go, and TypeScript. Include two distinct ranges with the same label to pin exact-text expected-item deduplication, and a control with distinct labels to verify they remain separate.

Test empty labels, wrong argument names/types, duplicate attributes, applying the attribute to a non-class, each prohibited line separator, formatter round trips, grammar JSON round trips, compiled bytecode loading, and both supported and rejected `#error` orderings. Add optimized first-guard, scanner, repetition, recognition, and left-recursion controls so metadata transparency and direct failure reporting are verified. Confirm LSP hover/completion advertises the attribute and that diagnostics use the same display label.

## Limitations and open questions

The recommendation is to allow a display string only on a character class, through grouping and a documented postfix metadata chain, with no localization or interpolation. Reject empty labels and CR, LF, U+0085, U+2028, and U+2029. The remaining implementation decision is how to identify the class through adjacent attributes; the proposed rule rejects a `#display` placed outside a semantic wrapper and permits `#display` on the class before an enclosing `#error`.
