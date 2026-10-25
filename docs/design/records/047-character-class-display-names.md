# 047. Diagnostic Expectation Names

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Add a postfix `@diagnostic(name="...")` attribute that gives a parsing expression a concise expectation name when that expression fails. The attribute is a failure wrapper, not metadata hoisted through nested expressions. It preserves the failure position and all parse behavior; it changes only the ordinary expected-item descriptions contributed by the wrapped failure. Explicit `@error` messages remain visible and take precedence.

The target may be any parsing expression, including a complete rule body:

```pego
rule number Number = ([0-9]+ "." [0-9]+) @diagnostic(name="number")
```

The precise wrapper boundary matters. An inner failure that is recovered before it reaches an outer diagnostic wrapper is no longer a failure of that outer wrapper. The annotation therefore does not rename every nested character-class expectation.

## Motivation

Character classes are currently shown using their mechanically formatted syntax. That is precise but noisy for domain concepts such as a decimal number or an identifier. A rule-wide custom `@error` message can provide prose, but it replaces the expected items with a message and has different composition semantics. A diagnostic name offers a concise expected item while preserving the parser's existing failure position and expected-item accumulation.

The grammar already represents postfix attributes with `grammar.Attributed`. Reusing that mechanism avoids new declarations or a separate expectation-label syntax. A wrapper must be honored at its expression boundary, rather than injected into every class descriptor below it.

## Goals

- Let authors name the ordinary expected item contributed by a failed parsing expression.
- Support any expression target, including a parenthesized expression or a complete rule body.
- Preserve farthest-failure positions, matching, captures, values, spans, and recovery behavior.
- Preserve explicit custom `@error` messages and their precedence.
- Keep diagnostics consistent across interpreters, generated parsers, and serialized grammars.

## Non-Goals

- Renaming every nested terminal or class expectation regardless of wrapper boundaries.
- Changing ordered choice, recovery, backtracking, capture, or action semantics.
- Replacing `@error` custom messages or introducing interpolation/localization.
- Inferring names from rule identifiers or adding named character-class declarations.

## Design

### Syntax and Validation

`@diagnostic` takes exactly one named argument, `name`, whose value is a non-empty string literal. Reject duplicate `@diagnostic` attributes on one expression chain and reject CR, LF, U+0085, U+2028, and U+2029 so a single expected item cannot span lines. Preserve other Unicode scalar values exactly; do not normalize them.

Attributes keep their normal postfix binding and written order. Each attribute wraps the expression immediately before it, and several attributes compose from the innermost outward. `@diagnostic` is valid on any parsing expression; it is not class metadata and must not be hoisted through semantic wrappers. For example:

```pego
rule digit = [0-9] @diagnostic(name="digit")
rule number Number = ([0-9]+ "." [0-9]+) @diagnostic(name="number")
rule recovered = expression @diagnostic(name="expression") #recover(skip=";")
rule recover_first = expression #recover(skip=";") @diagnostic(name="expression")
```

In `recovered`, a failure is labeled before recovery handles it. In `recover_first`, recovery can turn the inner failure into a successful recovered value, so the outer diagnostic wrapper may see success and leave the recorded expectations unchanged. This is ordinary inside-out attribute composition, not an order-independent metadata rule.

When `@diagnostic` and `@error` appear in either order in the same attribute chain, an applicable explicit `@error(message=...)` remains the reported custom message. Other semantic attributes, including recovery, retain their existing inside-out behavior. Do not flatten or reorder the chain to discover labels or messages.

### Failure and Expectation Contract

When the wrapped expression fails, replace the ordinary, non-message expected-item descriptions contributed by that wrapped failure at its tracked farthest position with the configured name. Preserve the position and preserve explicit `@error` messages. Do not move the failure to the wrapper start, replace expectations recorded at a farther position by another failure, or collapse unrelated expectation events into one label. On success, preserve the inner expectations already recorded, as with the existing wrapper behavior.

The attribute must not change whether the expression matches, which branch wins, captures or values produced, expression spans, or the farthest-failure position. If the existing expected-item table deduplicates identical strings, the configured name follows the same rule; the proposal does not change ordering or deduplication.

### Observed Baseline and Proposed Outcome

The baseline measurements below were executed on main `7cc59d8` using the grammar source form supported by that revision. They establish the existing diagnostic positions, not implementation or acceptance of the selected target syntax.

```pego
rule number = [0-9]+ "." [0-9]+
```

- Input `12x`: `1:3: syntax error: expected ".", [0-9]`
- Input `12.`: `1:4: syntax error: expected [0-9]`

With the proposed whole-body wrapper:

```pego
rule number Number = ([0-9]+ "." [0-9]+) @diagnostic(name="number")
```

The proposed output keeps the same positions and full diagnostic shape while replacing the ordinary expected descriptions:

- Input `12x`: `1:3: syntax error: expected number`
- Input `12.`: `1:4: syntax error: expected number`

These are design targets, not verified behavior. As a separate control measured on that same revision, a whole-body `@error(message="expected a decimal number")` equivalent reports:

- Input `12x`: `1:3: syntax error: expected a decimal number`
- Input `12.`: `1:4: syntax error: expected a decimal number`

That custom-message behavior remains intact and takes precedence when applicable.

### Representation and Implementation Constraints

Represent the proposal with the existing `grammar.Attributed` and `grammar.Attribute` form; do not add a field to `CharClass` or `RuleDef` solely for this feature. Validation records the exact name and rejects invalid argument shapes before compilation. Preserve the attribute in grammar JSON and formatter round trips.

Runtime and compiler paths must honor the wrapper's failure boundary. In particular, first-character guards, scanners, repetition lowering, choice dispatch, and other optimized paths that report failures directly must not blindly inject the label into all nested class descriptors. The wrapper behavior must be equivalent across closure compilation, recursive and iterative bytecode VMs, and generated Go and TypeScript parsers. It must leave matcher identity and all successful-path analysis unchanged.

### Compatibility

Grammars without `@diagnostic` retain their existing behavior. Older implementations reject the new attribute as unknown, making it an opt-in grammar feature. The JSON AST already has an attributed-expression representation; verify strict JSON and bytecode compatibility before implementation, and use an explicit format revision if existing serialized forms cannot carry the required behavior safely.

Update `spec/language/attributes.md`, grammar and diagnostic guides, formatter and LSP attribute help. The postfix spelling applies to expressions. [Design 049](049-rule-attribute-decorators.md) records the rejected declaration-prefix alternative.

## Alternatives Considered

- **Class-only label metadata.** Narrower and potentially cheaper, but cannot name composite failures such as a decimal number. It also requires special class handling and still needs clear behavior when semantic wrappers intervene.
- **A rule-only annotation.** Simpler for whole-rule diagnostics, but cannot name a subexpression and creates a second scope/feature model.
- **`@error(message=...)`.** Already supports custom prose, but replaces expected items with an explicit message and retains its own wrapper precedence. It remains appropriate when a full message is desired.
- **A declaration-only prefix annotation.** It would need a new annotation form and still leave expression-level labels unavailable. Decorator sugar is considered separately without changing the postfix contract.
- **A field on `CharClass` or a named-class declaration.** These duplicate the existing attribute representation or introduce symbol lookup and composition rules.

## Testing

Before implementation, acceptance should include:

- Ordinary terminal, class, sequence, choice, grouped, and whole-rule-body targets; successful inputs preserve matching, values, captures, and spans.
- Failures at the wrapped farthest position replace only ordinary expected descriptions. Independent farther failures, unrelated expectation events, ordering, and deduplication remain correct.
- Both `@diagnostic`/`@error` orders preserve explicit message precedence. Both diagnostic/recovery orders demonstrate the specified boundary behavior; nested recovery and failed recovery remain covered.
- Invalid/empty names, wrong and extra arguments, duplicates, forbidden line separators, valid Unicode, JSON round trips, and formatter round trips.
- Parity across closure, recursive bytecode, iterative bytecode, generated Go, and TypeScript, including first-guard/scanner/repetition/choice paths that emit failures directly.
- LSP completion and hover, source locations, and diagnostic text agree across supported frontends.

## Limitations

This remains a proposal. The label applies only to ordinary expected items from a failure that reaches the annotated wrapper. Recovery and other semantic attributes retain normal written-order composition; the label is not a declaration-wide alias and does not cross a wrapper that has already recovered the failure. An applicable explicit `@error` message takes precedence.
