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

Any rule could be entered, and the default start rule was a convention of each
tool: `main` on the command line and in fixtures, the first rule in the
playground. A helper rule could be entered from outside and its value relied
on, the same grammar started at different rules in different tools, and
generated parsers carried a named entry, with its dispatch case and typed
converters, for every rule, although most rules are only ever called from
other rules. The tools had no way to tell which rules a grammar meant as
entries.

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
- Start rules for Operators Rule levels or for hidden rules.

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
- a rule has a declaration attribute other than `@start`.

These errors are reported once the grammar is otherwise valid, so that a
grammar under construction shows its other errors first.

Parsing without a rule name uses the default start rule. Naming a rule that
is not a start rule is an error saying that the rule is not a start rule;
naming one that does not exist says that it is not defined. The compile and
generate functions keep their start argument: an empty name selects the
default start rule, and any other name must be a start rule.

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

Every existing grammar must mark its start rules; a grammar without `@start`
no longer compiles. The ISA version of the bytecode module changes, so older
compiled grammars must be compiled again. Generated parsers that were entered
at a helper rule by name must mark that rule `@start`.

## Alternatives Considered

- **Every rule is an entry, and the default is a convention.** This was the
  previous behavior. It needs no annotation, but each tool chose its own
  default, and generated parsers pay for every rule as an entry.
- **The first rule is the default.** It needs no annotation either, but
  declaration order would carry meaning, and moving a rule would change what
  parsing does.
- **A grammar-level start declaration, such as `start main`.** It would name
  the start rules in one place, away from the rules themselves. A declaration
  attribute keeps the information on the rule, and moves and renames with it.
- **Treating unmarked grammars as having all rules as start rules.** It keeps
  old grammars working, but leaves the implicit behavior in place and makes the
  set of entries depend on whether any rule is marked.

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

Emitting typed entry code only for start rules shrinks generated Go parsers.
Sizes of `parser.go` in bytes, before and after:

| Grammar | Before | After | Change |
|---|---:|---:|---:|
| DuckDB SQL | 35,398,859 | 34,913,571 | -1.37% |
| TypeScript | 12,364,275 | 12,217,505 | -1.19% |
| XML | 1,033,566 | 1,015,370 | -1.76% |
| minilang | 514,912 | 506,274 | -1.68% |
| JSON | 263,354 | 254,732 | -3.27% |
| calc_lr | 132,195 | 128,726 | -2.62% |
| CSV | 92,260 | 91,742 | -0.56% |
| outline | 88,878 | 84,898 | -4.48% |
| calc | 71,852 | 70,208 | -2.29% |

Generated TypeScript parsers keep their size, one byte apart: they had no
per-rule typed entries, and only the table of start rules is new.

## Limitations and Open Questions

- Generated parsers still contain rules that no start rule reaches, and a
  standalone entry body is still emitted for some rules that only external
  entry used before.
- Hidden rules cannot be named yet, so they cannot be start rules; when they
  can, marking one `@start` is to be an error.
