# PEGO Core IR Specification

Core IR is the intermediate representation that sits between a checked grammar and the code a backend emits. It
describes each rule of a grammar as a tree of typed parsing expressions whose observable effects are explicit: the
input a node consumes, the expectations it records, the captures it writes and the errors it can raise. Optimizations
are rewrites of Core IR, and each of them is valid exactly when it preserves those effects.

This specification defines what Core IR denotes, its node kinds and their meaning, the attributes and effects that
analysis computes, the invariants that every well-formed module satisfies, and the text form that the IR package
prints. It does not define how a target turns Core IR into code, the passes that rewrite it, or Flow IR, the
lower-level representation that follows it. Core IR is internal to the PEGO engine and written for engine maintainers and pass authors: it is not a public API, and
nothing here changes how a grammar behaves, which the [Grammar Language Specification](../language/README.md) and the
[Parser and Runtime Specification](../runtime/README.md) define.

This specification is authoritative for the meaning of nodes, the attributes and effects, and the invariants that the
verifier enforces. A disagreement with the code of `internal/engine/core` is a bug, fixed on whichever side is wrong.
Details that this specification does not state are defined by the code.

## Contents

| | Chapter | Defines |
|--:|:--|:--|
| 1 | [Modules and Rules](modules.md) | The module, its rules, scopes and call graph, and how a rule makes its value |
| 2 | [Nodes](nodes.md) | The kinds of expression node and what each one matches |
| 3 | [Terms](terms.md) | The typed language of actions, predicates and bindings |
| 4 | [Attributes and Effects](attributes.md) | The attributes of a node, nullability, first terminals and the effects |
| 5 | [Invariants](invariants.md) | What a well-formed module guarantees and what verification checks |
| 6 | [Text Form](text-form.md) | The printed form of a module, with a worked example |

The [Glossary](../glossary.md) lists the terms of the specifications.

## Conventions

The key words MUST, MUST NOT and MAY are used as in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and only where
a requirement is normative. The meaning of a node is stated as what the node does at a position of the input: it
succeeds, consuming input, or fails. Matching, expectations, captures and values follow the
[Grammar Language Specification](../language/README.md); this specification names a behavior of the language and does
not restate it.
