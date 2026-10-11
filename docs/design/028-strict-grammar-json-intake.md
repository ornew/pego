# 028. Strict Grammar JSON Intake

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add an opt-in strict JSON entry point for tools that exchange PEGO grammar
ASTs. `grammar.DecodeJSON(data)` rejects unknown object fields
and duplicate keys while preserving `grammar.UnmarshalJSON(data)` and its
current permissive compatibility behavior.

## Motivation

`grammar.UnmarshalJSON` decodes JSON into generic maps before constructing
the AST. Unknown fields are ignored and duplicate keys follow the current
last-value-wins behavior. This is compatible with existing callers, but it
can hide misspelled fields or conflicting values in authored grammar files.
The guide documents that behavior and there is no strict intake API.

`grammar.Validate` already provides public structural validation for callers
that build ASTs directly. It does not check JSON syntax, unknown fields or
duplicate keys, and it deliberately does not replace compiler checks for
names, types, attributes or Pratt semantics. A strict JSON decoder should
reuse the same structural check after decoding rather than broaden
`Validate` into a format-specific validator.

## Goals

- Offer a discoverable opt-in JSON API that rejects unknown fields at every
  grammar-object level and duplicate keys at every JSON-object level.
- Preserve the exact existing `UnmarshalJSON` function signature and default
  behavior for existing callers and function values.
- Report deterministic paths and byte offsets for strictness errors.
- Preserve the existing one-document requirement, numeric bounds, `@type`
  registry, and structural validation behavior.
- Preserve the registry's interface-family invariant: a registered pointer
  type must implement the interface expected at that AST position.
- Keep strict intake separate from semantic compilation and resource limits.

## Non-goals

- Changing the default behavior of `UnmarshalJSON` or the serialized grammar
  format.
- Rejecting unknown fields in compiler or hand-built AST paths.
- Replacing `grammar.Validate`, adding semantic validation, or imposing JSON
  size/depth limits.
- Adding a CLI flag in this API design. A later CLI may expose the strict
  entry point through an explicit opt-in flag.

## Design

### Public API and compatibility

Add a strict-only entry point:

```go
func DecodeJSON(data []byte) (*Grammar, error)
```

`DecodeJSON` rejects both unknown fields and duplicate keys. It requires one
complete JSON value followed only by whitespace, constructs the registered
AST types, and then calls `Validate`, matching the structural guarantee of
`UnmarshalJSON`. The existing `UnmarshalJSON` remains unchanged and
permissive; callers choose strictness by choosing the new function.

Prefer this separate function over adding variadic functional options to
`UnmarshalJSON`. Existing calls would still compile, but its function type
would change, breaking callers that store or pass `func([]byte) (*Grammar,
error)` values. Options would also add configuration to a function whose
current default is part of the compatibility contract. A separate strict
entry point keeps old behavior and types stable. Do not add a `WithOptions`
wrapper or independent unknown/duplicate toggles until a concrete use case
requires partial strictness.

### Strictness and error paths

Unknown names are rejected using exact JSON-tag spelling; matching is
case-sensitive. `@type` is recognized only where an interface-valued AST
node requires it. Its registered concrete pointer type must implement the
expected interface (`reflect.PointerTo(st).Implements(t)`); a known type from
the wrong AST family is rejected just like an invalid node for that position.
A key not present on the concrete AST type is an error,
including unknown keys nested inside statements, expressions, terms, type
specifications and action nodes.

Any repeated object key is rejected, even when its values are equal. Duplicate
comparison uses the decoded JSON string, so escaped spellings of the same key
also collide. The decoder reports the first duplicate encountered in input
order, at the second occurrence. It does not choose first-wins or
last-wins semantics in strict mode.

Strictness errors use a `*grammar.JSONDecodeError` with `Path`, `Offset`, `Msg` and
`Cause` fields. `Path` starts at `$`, follows the validator's field and
zero-based array-index notation, and identifies the offending key; unusual
object names use a quoted bracket segment. `Offset` is the zero-based byte
offset of the offending key token (the second token for duplicates). JSON
does not carry grammar source positions, so these errors have no `grammar.Pos`.
Syntax errors keep their standard JSON cause and byte offset. Structural
errors after successful decoding continue to return `*grammar.ValidationError`
with its existing path and position fields.

### Implementation and invariants

The strict path must inspect object members before reducing them to a Go map,
because map construction loses duplicate occurrences. It can use a token or
small raw-object representation that retains each decoded key, value and key
offset until the expected AST type is known. Type-directed decoding then
checks exact JSON tags and the registered `@type`, constructs the same AST
shape as `UnmarshalJSON`, and invokes `Validate` once. It must not mutate
decoded values to recover from malformed input or silently discard strict
errors.

`UnmarshalJSON` remains on the current decode path until any shared internal
refactoring preserves its observed compatibility behavior: unknown fields
remain ignored and duplicate keys retain last-value-wins behavior. Both
functions continue rejecting trailing JSON values/data and integer values
outside destination types. Neither function resolves names or performs type,
attribute or Pratt checks; callers still use `pego.Compile` for those checks.

### CLI and tooling integration

Tooling that wants typo detection can opt in by calling `grammar.DecodeJSON`.
A future `pego compile` or `pego gen` strict-input flag should select this
entry point only for JSON grammar files; source grammar parsing is unaffected.
The default CLI path should remain compatible unless a separate migration
decision changes it.

## Alternatives considered

- **Make `UnmarshalJSON` strict by default.** This catches mistakes without
  caller changes, but rejects files that currently rely on ignored extension
  fields or duplicate-key handling. It is a compatibility change and is not
  proposed here.
- **Add variadic functional options to `UnmarshalJSON`.** Existing direct
  calls compile, but function-value assignments break because the function
  type changes. It also mixes a new policy surface into the legacy API.
- **Add separate switches for unknown fields and duplicate keys.** This offers
  finer policy control, but creates partially strict modes whose safety and
  migration meaning are unclear. Start with one strict contract that rejects
  both forms of ambiguity.
- **Use `encoding/json.Decoder.DisallowUnknownFields`.** The current codec
  handles interface nodes through a registered `@type` field and custom
  reflection conversion. A standard decoder alone does not preserve duplicate
  keys and would not provide the required AST paths for nested errors.
- **Expose only `Validate`.** Structural AST validation cannot detect unknown
  or repeated keys after generic map decoding has erased them. Keep it as the
  reusable AST check and add format-specific strictness at JSON intake.
- **Rely only on a linter.** A linter can report likely authoring mistakes,
  but it cannot ensure that strict build/tooling paths reject ambiguous input.

## Testing

Preserve regression cases showing that `UnmarshalJSON` still accepts unknown
fields and applies last-value-wins duplicate handling. For `DecodeJSON`, test
unknown keys at the root and at every representative nested AST object,
including misspelled known keys and case variants. Test duplicate keys with
equal and conflicting values, escaped-equivalent names, repeated `@type`, and
keys inside arrays of nodes. Assert exact `Path`, byte `Offset` and error
classification, including the second occurrence for duplicates.

Run strict and permissive intake over the existing valid grammar JSON corpus,
round-tripped `MarshalJSON` output, malformed syntax, `null`, trailing values,
integer boundaries, unknown `@type` names and malformed interface nodes. Check
that a known but cross-family `@type` (for example, a `Ref` where a statement
is expected) is rejected, including when `@type` follows other object fields.
Check that strict decoding still runs `Validate`, that later compilation reports
semantic errors through the compiler, and that generated Go and TypeScript
consumers receive unchanged ASTs. Add CLI coverage only with a separate
strict-flag design or implementation.

## Limitations

The new API does not make legacy `UnmarshalJSON` strict, and it does not impose
resource limits; hostile-input budgets are a separate design concern.
Consumers should rely on the documented error type, path and byte offset
rather than exact diagnostic wording.
