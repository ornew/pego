# 061. Explicit Start Rules

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-22

## Summary

A grammar names the rules where parsing may begin with the declaration
attribute `@start`, written on the lines before `rule`. Only those rules, the
start rules, can be entered: by default parsing, by name, by compiled grammars,
documents, streams, prefix parsing and generated parsers alike. One start rule
is the default; when there are several, `@start(default)` marks it.

```pego
@start(default)
rule file = stmt*

@start
rule stmt = expr ";"

rule expr = [0-9]+
```

Here `file` is the default start rule, `stmt` can be entered by name, and
`expr` can only be called from other rules.

## Motivation

When a grammar does not declare its entries, any rule can be entered, and the
default start rule is a convention of each tool, such as a rule named `main`
or the first rule. A helper rule can then be entered from outside and its
value relied on, the same grammar starts at different rules in different
tools, and a generated parser carries a named entry, with its dispatch case and
typed converters, for every rule, although most rules are only called from
other rules. The tools cannot tell which rules a grammar means as entries.

## Goals

- The entries of a grammar are declared in the grammar, next to the rules.
- The default start rule does not depend on declaration order.
- Every way into a grammar accepts only start rules and names the rule in its
  error, with the interpreter, compiled grammars and generated Go and
  TypeScript parsers agreeing.
- Generated parsers emit entry dispatch only for start rules.

## Non-Goals

- Removing rules that no start rule reaches, or code kept only for external
  entry; see Limitations.
- Start rules for Operators Rule levels.

## Design

### Public Behavior and Contracts

`@start` and `@start(default)` are declaration attributes of a rule, defined
in [Grammar Files](../../spec/grammar-files.md#start-rules). The argument of
`@start` is the bare identifier `default` and nothing else. A grammar is
invalid when:

- no rule is marked `@start`;
- several rules are marked and none is `@start(default)`;
- more than one rule is `@start(default)`;
- a rule is marked `@start` twice;
- `@start` has any other argument;
- a [hidden rule](../../spec/actions.md#hidden-rules), whose name begins with
  `_`, is marked `@start`;
- a rule has a declaration attribute other than `@start`.

These errors are reported once the grammar is otherwise valid, so that a
grammar under construction shows its other errors first.

Parsing without a rule name uses the default start rule. Naming a rule that
is not a start rule is an error saying that the rule is not a start rule;
naming a hidden rule says that it is hidden, and naming one that does not
exist says that it is not defined. The hidden rules behind inline actions
are never start rules. The compile and
generate functions take a start argument, and every interface that takes the
name of a start rule accepts an empty name: it selects the default start rule
of the grammar, and any other name must be a start rule.

### Implementation and Invariants

The compiler resolves the start rules into the program, in declaration order,
with the default. Every parse checks its start rule before it prepares a
backend, so the check is the same for the closure engine and both bytecode
models and does not depend on which tables a backend has built.

A compiled grammar stores the start rules and the default in the bytecode
module, after the rule table, and the loader validates them: at least one
start rule, distinct and in rule-table order, and the default among them. The
syntax tree stored in a compiled grammar does not keep declaration attributes;
the loader marks its rules from the module's start rules, and a program built
from the tree must agree with the module. A recognition bundle must have the
start rules of its main module.

Generated parsers record the start rule names. The Go `ParseRule` and the
TypeScript `parseRule` look a name up among them and report the same errors as
the interpreter; the typed Go `ParseRuleAST` has a case, and converters, only
for start rules.

### Compatibility and Backend Support

Every grammar marks its start rules, and a grammar without `@start` does not
compile. The start rules are part of the bytecode module under its ISA
version, so a compiled grammar of an earlier ISA version is rejected and is
compiled again. A rule that a generated parser enters by name is marked
`@start`.

## Alternatives Considered

- **Every rule is an entry, and the default is a convention.** It needs no
  annotation, but each tool chooses its own default, and generated parsers pay
  for every rule as an entry.
- **The first rule is the default.** It needs no annotation either, but
  declaration order would carry meaning, and moving a rule would change what
  parsing does.
- **A grammar-level start declaration, such as `start main`.** It would name
  the start rules in one place, away from the rules themselves. A declaration
  attribute keeps the information on the rule, and moves and renames with it.
- **Treating unmarked grammars as having all rules as start rules.** It needs
  no marks in grammars that do not care, but keeps the implicit entries and
  makes the set of entries depend on whether any rule is marked.

## Testing

Compile errors are checked with their positions, including that other errors
come first. The restriction is checked on parsing, streams, iteration,
documents, prefix parsing and compiled-grammar encoding, on the closure engine
and both bytecode models, with and without recognition, and after a compiled
grammar round trip with and without its syntax tree. Module validation rejects
missing, out-of-range, repeated and misordered start rules and a default that
is not a start rule. Generated Go and TypeScript parsers reject non-start rules
with the interpreter's messages.

## Performance and Results

A per-rule entry of a generated Go parser, its `ParseRuleAST` case and typed
converters, costs bytes in `parser.go`, and only start rules have one. For the
bundled grammars, generated with `pego gen -types -recognize -runtime import`
(DuckDB SQL, TypeScript and JSON also with `-nodoc`), the entries of the rules
that are not start rules would cost:

| Grammar | `parser.go` bytes | Entries of other rules, bytes | Share with them |
|---|---:|---:|---:|
| DuckDB SQL | 13,381,886 | 485,337 | 3.50% |
| TypeScript | 5,416,888 | 146,819 | 2.64% |
| XML | 468,457 | 18,245 | 3.75% |
| minilang | 253,370 | 8,687 | 3.31% |
| JSON | 117,837 | 8,671 | 6.85% |
| calc_lr | 64,417 | 3,518 | 5.18% |
| outline | 44,587 | 4,028 | 8.29% |
| CSV | 43,617 | 567 | 1.28% |
| calc | 41,045 | 1,693 | 3.96% |

Generated TypeScript parsers have no per-rule typed entries; their table of
start rules costs a few bytes.

## Limitations and Open Questions

- Generated parsers contain rules that no start rule reaches, and standalone
  entry bodies of some rules that no start rule enters directly.
