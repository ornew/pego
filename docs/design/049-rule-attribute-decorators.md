# 049. Rule Attribute Prefixes

- **Status**: Rejected
- **Author**: @ornew
- **Date**: 2026-10-12

## Decision

Attributes remain postfix annotations on expressions. Declaration-prefix
attributes such as `@error("expected a token")` before a `rule` declaration
are not part of the selected syntax.

```pego
rule token Token = part name @error("expected a token") => build($1)
```

To annotate a complete sequence, group it before the postfix attribute. The
whole-rule action on an ordinary rule stays after the annotated expression.

## Rationale

A prefix form would need a second association rule for which declaration and
expression it wraps, particularly across comments, intervening declarations
and formatter edits. Postfix attributes already compose at the expression
level and keep their written order. Keeping one form avoids duplicating
validation, source association and formatting behavior.

Attributes use `@name` and behavioral pragmas use `#name`; their argument and
execution rules remain distinct. This decision does not change attribute
semantics, add declaration metadata, or reorder annotations.

## Consequences

Documentation and tools should show postfix expression annotations only.
Ordinary rules and Operators Rule declarations do not accept a declaration
prefix attribute. See [Attributes](../../spec/attributes.md) for
the selected syntax.
