# 058. Positional Terminal Constructors

- **Status**: Superseded
- **Author**: @ornew
- **Date**: 2026-10-14

> **Note (2026-10)**: Terminal types and their positional constructors were removed. A token type is now a text
> struct, `type T = { Text string }`, built with `T{Text: x}`, and `zp fix` rewrites the forms this record describes
> (see [Value Types and Generic Nodes](060-value-types-and-generic-nodes.md)). The record keeps the original syntax.

## Summary

A terminal Type may be constructed with one positional string argument, for
example `Int{"0"}` or `Int{text($digits)}`. The supplied string becomes the
terminal text; it is not parsed against the terminal's Rule Expression. Struct
constructors retain their named-field syntax.

## Motivation

A terminal-typed Rule Definition can produce a terminal by matching
input. An Action Expression may also need to construct that terminal from a
string already produced or transformed by another expression. Positional
construction expresses this directly without representing a terminal as a
struct with fields.

```zp
type Int terminal
rule digits Int = [0-9]+
rule zero Int = "0" => Int{"0"}
rule normalized Int = n:digits => Int{text($n)}
```

## Accepted Contract

### Syntax and Type Checking

Use `T{arg, ...}` when `T` resolves to a terminal Type. The source AST node is
`TerminalConstructor{Type string, Args []Term}` and preserves all supplied
arguments so that invalid arity can receive a positioned compile-time
 diagnostic. Exactly one argument is required. A known non-string argument is
a compile-time error. An argument of unknown Type (`_`) is rejected with a hint to cast it,
as in `value.(string)`; the cast is the check that runs.

Terminal aliases and join members are resolved through existing canonical
Type resolution. If the target resolves to a terminal, it uses this
constructor contract. An empty `T{}` remains parsed as the existing zero-field
`New`; if `T` resolves to a terminal, compilation rejects it for having zero
arguments. A struct continues to use `T{Field: value, ...}`. This feature does
not add positional struct construction.

### Value and Range

A valid constructor produces a terminal node whose text is exactly the
provided string. It does not validate that text against the terminal's
recognition Rule Expression and performs no implicit conversion.

The new terminal node's range is the full range of the current Action
Expression, from its start through its end. It is not derived from the string
argument's length. The selected position unit determines the range. Returning
an already existing terminal preserves that node's identity and original
range.

### AST, Runtime and Compatibility

A valid `TerminalConstructor` lowers to the dedicated `ETERMINAL` bytecode
operation, appended as opcode 124. The bytecode ISA version is 5 when this
operation is emitted; it is emitted only when used. A list-only grammar may
still use `list()` and remain on ISA 3. New AST-bearing compiled grammars use
new AST tags that older AST decoders reject. AST-free list-only artifacts
remain ISA 3 compatible. Typed Go helpers are emitted only when generated
code uses them.

The closure, recursive VM, iterative VM, generated Go, typed Go and TypeScript
backends must agree on terminal Type, text, identity and range. Existing
terminal-typed Rule Expressions, `text`, struct constructors and serialized
formats outside the documented AST/ISA additions retain their behavior.

## Alternatives Considered

- **A dedicated `terminal(T, text)` function.** This avoids brace overloading
  but hides the target Type in a function argument and still needs a typed
  construction rule.
- **Allow `T{Text: value}`.** This models a terminal as a struct with a public
  field and conflicts with terminal Type declarations.
- **Restrict terminal creation to matched rules.** This prevents Actions from
  building a typed terminal from an available string.
- **Stringify arbitrary values.** This introduces conversion rules unrelated
  to assignability and can hide type errors; require a string-valued input.

## Testing and Validation Gates

- Accept a string literal and `text(...)`; reject zero, multiple and
  non-string arguments when their static Type is known.
- Verify arguments of unknown Type (`_`) are rejected unless cast, and that a
  failed cast reports the normal Action Expression error.
- Test terminal aliases and join resolution, exact supplied text, current
  Action Expression ranges in CodePoints and Bytes, and preservation of an
  existing node's range and identity.
- Compare closure, recursive and iterative VMs, generated Go, typed Go and
  TypeScript for values, errors and diagnostics.
- Check source AST/JSON/formatter behavior, compiled-grammar ISA handling,
  caller AST immutability, and unaffected struct constructors and
  terminal-typed Rule Expressions.

Local validation covers source and strict JSON round trips, all three runtime
backends and both position units, AST-bearing and AST-free saved grammars,
generated Go and TypeScript, and typed result conversion. The root-module
suite, generated parser suite and site build/link check pass. Independent
critical review found no outstanding material implementation issue. Remote
CI remains paused.
