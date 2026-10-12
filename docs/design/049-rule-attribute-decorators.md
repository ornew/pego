# 049. Rule Attribute Decorators

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Allow prefix decorators immediately before a `def` rule as syntax sugar for the existing postfix rule-body attributes. For example:

```pego
@bar() @foo()
def x = (expression)
```

is equivalent to:

```pego
def x = (expression) #foo() #bar()
```

The nearest decorator is applied first and is therefore the innermost wrapper; outer decorators are applied last. Decorators reuse the existing attribute names, argument grammar, validation, AST representation, and semantics. This proposal adds no new attribute behavior.

## Motivation

Long rule bodies can make postfix attributes difficult to see near the declaration they describe. Prefix spelling can improve readability while preserving the existing attribute model. Desugaring to ordinary postfix attributes keeps one semantic implementation and lets tools share validation and behavior.

## Goals

- Add a readable prefix spelling for attributes on rule-body expressions.
- Preserve exactly the behavior and attribute ordering of the equivalent postfix chain.
- Reuse the current argument syntax, validation, and `grammar.Attributed` representation.
- Keep formatter, LSP, JSON, and compiler behavior explicit and testable.

## Non-goals

- Adding attribute meanings or reordering semantic attributes.
- Introducing annotations for types, modules, variables, or individual Pratt entries.
- Replacing expression-level postfix attributes or changing the existing atomic-expression syntax.
- Adding a separate decorator field to `RuleDef` or changing public rule APIs.

## Design

### Syntax and desugaring

A decorator is an attribute-shaped prefix form directly associated with a following `def` declaration. It uses the existing attribute spelling and named-argument parser; for example, `@diagnostic(name="number")` reuses the `#diagnostic(name="number")` argument grammar from [design 047](047-character-class-display-names.md). This proposal does not add positional-string shorthand or redesign argument parsing.

A consecutive decorator list is translated into a postfix chain in reverse source order, so the decorator nearest `def` is the innermost wrapper:

```pego
@bar() @foo()
def x = (expression)
```

means exactly:

```pego
def x = (expression) #foo() #bar()
```

No semantic attributes are generically reordered. Their existing written, inside-out behavior is retained by this explicit desugaring.

### Scope and rule actions

A decorator wraps the complete rule-body expression just as the equivalent postfix attribute does. It does not create a new scope or change the expression's type, captures, `$0`/`$n` values, action analysis, or failure behavior. Whole-rule arrow actions remain outside the decorated body:

```pego
@foo()
def x: Result = (expression) -> action
```

is equivalent to:

```pego
def x: Result = (expression) #foo() -> action
```

Parenthesizing the body makes the wrapper boundary explicit: it covers the complete intended expression, while the whole-rule arrow action remains outside it.

Decorators apply to ordinary rule definitions. If explicit Pratt rule bodies are supported, the same wrapper may apply only to the complete rule-body expression after an explicit compatibility plan; decorators never attach to individual Pratt entries and do not assign special meaning to those entries. Implementation must settle and test that boundary before claiming Pratt support.

The prefix context is restricted to `def` rules. The existing atomic-expression syntax is unchanged. No implicit decorated type, module, or variable declarations are introduced.

### Validation and source tools

Unknown decorator names, invalid arguments, duplicate-attribute rules, and invalid targets use the same validation as the equivalent postfix attributes. Malformed decorator prefixes must produce stable source-located diagnostics. Whitespace, comments, and line breaks between a decorator sequence and its `def` target must be specified, highlighted, and covered consistently by the lexer, parser, and LSP.

Keep the existing JSON representation as nested `Attributed` expressions. Do not add a `RuleDef` API field solely to retain source sugar. A source-aware formatter preserves decorator spelling, comments, and order. A grammar-AST-only formatter that has no source-sugar information may emit the equivalent postfix chain, preserving order and behavior. Source preservation must not add a runtime `RuleDef` field solely for this sugar.

### Compatibility

Existing postfix attributes keep their syntax and semantics. Existing atomic-expression syntax is not reinterpreted. The prefix form is opt-in source syntax; older parsers will reject it. No serialized AST or bytecode change is intended, subject to compatibility verification before implementation.

Update `spec/attributes.md`, grammar syntax references, formatter/LSP documentation, and links to [design 047](047-character-class-display-names.md). The canonical equivalent continues to use the documented postfix attributes.

## Alternatives considered

- **Keep postfix syntax only.** No grammar or formatter work is needed, but declarations with long bodies can visually separate annotations from the rule they affect.
- **Add a separate rule annotation system.** This could define independent semantics and scopes, but duplicates parsing, validation, tooling, and representation already provided by expression attributes.
- **Treat every prefix annotation as rule metadata.** That invites new semantics and API fields instead of preserving exact postfix equivalence.
- **Discard decorator spelling in all formatting.** This would simplify formatting but lose the readability benefit for source-aware workflows. Preserve spelling when source information is available; allow an AST-only formatter to use postfix form.

## Testing

Before implementation, acceptance should cover:

- A single decorator and stacked decorators, including exact inside-out order for `@bar() @foo()` versus `#foo() #bar()`.
- Named arguments accepted by existing attributes, including `@diagnostic(name="number")`; malformed, missing, duplicate, unknown, and invalid-target cases match postfix validation.
- Rule-body scope equivalence for captures, `$0`/`$n`, inferred and explicit action types, predicates, recovery, custom errors, and whole-rule arrow actions.
- Explicit Pratt body compatibility or an explicit rejection boundary; no decorator attachment to individual Pratt entries.
- Whitespace, comments, newline placement, source spans, parser diagnostics, LSP completion/hover/highlighting, JSON round trips, and formatter canonicalization with preserved order.
- Equivalent runtime behavior across closure, both bytecode VMs, generated Go, and TypeScript wherever each attribute is supported.
- Existing atomic-expression syntax and postfix-only grammars retain their behavior.

## Limitations

This proposal defines syntax sugar for `def` rule bodies only. It adds no general decorator semantics and does not make semantic attributes commute. Source-aware formatting preserves decorator spelling; an AST-only formatter may canonicalize to postfix attributes because the source form is no longer available.
