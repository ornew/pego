# 048. Inline Grammar Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

The source form `(expression => term)` represents an inline grammar action.
Parsing, formatting and AST serialization recognize this form, but compilation
rejects it. This record proposes its execution contract; it does not claim
runtime or backend support.

```pego
type Number terminal
type Word terminal
type Expr = Number | Word
type Main struct { Value Expr }

rule number Number = [0-9]+
rule word Word = [a-z]+
rule item Expr = (number => $1) / (word => $1)
rule main Main = item => Main{Value: $1}
```

The existing whole-rule action remains `=> term` after the complete ordinary
rule expression. Operators Rule actions apply separately to operand and
operator entries. Operator-entry actions keep their specialized `$lhs`,
`$rhs` and `$op` context. A declaration-level action on an Operators Rule
may be omitted or use the explicit `=> ...` default-CST form; any other action
is rejected at compile time.

## Proposed Contract

An inline action wraps the preceding expression inside its parentheses. For a
sequence such as `(a b => A) c`, `A` receives the values matched by `a` and
`b`, and the outer sequence continues with the inline action result followed
by the value from `c`. The action boundary closes earlier named captures; a
later inline action sees the preceding result as a positional value. The
boundary introduces no hidden rule, variable scope, cut, memo, or left
recursion boundary.

For a non-sequence ordered choice, the selected branch's value is `$1`; `$0`
is a singleton list containing that value. Choice selects a branch by PEG
matching before its required action runs. An action failure is fatal and does
not retry another branch or enter recovery. Failed wrapped expressions do not
run their actions. Existing value-needed analysis may omit an action whose
result is unused, including a failing action.

These semantics preserve the enclosing expression's matching, capture,
rollback, cut, recovery, memoization and recursion behavior. Type checking
uses the rule's declared or inferred result type. Actions return a node or
`nil`.

## Alternatives

- **Whole-rule actions only.** This is simpler, but cannot reduce a matched
  sequence before later sequence elements.
- **A suffix on one primary.** It cannot name the complete preceding sequence
  as the reduction input.
- **Braced local actions.** Braces overlap with type constructors and
  declaration bodies. Parentheses delimit the expression and action term.
- **Synthetic helper rules.** They add rule depth, memo entries, trace events
  and synthetic names rather than expressing a local reduction boundary.

## Acceptance Gates

Before runtime adoption, verify action boundaries and captures, `$0`/`$n`,
choice selection, fatal action errors, action elision, type checking, and
behavior inside predicates, recovery, repetition, cuts, left recursion and
memoized rules. Compare closure, recursive and iterative VMs, generated Go
and TypeScript. Check JSON, bytecode, formatter round trips, source ranges and
LSP behavior. These remain pending criteria.

## Limitations

The source parser currently accepts the syntax for representation only; the
compiler rejects it. Runtime execution and backend parity remain unimplemented.
