# 001. Grammar JSON Marshalling

- **Status**: Final
- **Author**: @ornew
- **Date**: 2025-06-29

## Summary

This document proposes a design for marshalling and unmarshalling the `Grammar` object to and from JSON. The key principle is to create a JSON representation that faithfully mirrors the structure and order of a PEGO source file, making it a suitable foundation for two-way conversion.

## Motivation

The ultimate goal is to achieve self-hosting, where the PEGO parser can parse its own grammar files (`.pego`). Before we can parse the PEGO syntax itself, we need a way to represent the grammar structure in a data interchange format. JSON is a universally understood and easy-to-generate format that serves this purpose well.

By enabling `Grammar` <-> JSON conversion, we can:
1.  Define grammars in JSON, allowing us to bootstrap the parsing process without a fully functional PEGO parser.
2.  Create tools that can read, analyze, or generate PEGO grammars.
3.  Simplify testing by creating grammar fixtures in a human-readable JSON format.
4.  Establish a foundation for two-way conversion between PEGO source and its JSON representation by preserving definition order and structure.

## Goals

- Implement custom JSON marshalling for the `Grammar` object and all its constituent parts.
- The JSON representation must be a faithful, ordered representation of the grammar definitions, with a consistent structure for `TypeDef` and `RuleDef`.
- The marshalling process must be reversible.
- The resulting JSON should be human-readable and follow a strict naming convention:
    - All field names must be `camelCase`.
    - All `@type` discriminator values must be `PascalCase`.

## Non-Goals

- This design does not cover the implementation of the PEGO parser itself.
- Performance of the marshalling/unmarshalling process is not a primary concern at this stage.

## Design Details

The `Grammar` will be represented as a list of `statements`. Each statement is either a `TypeDef` or a `RuleDef`, distinguished by a `@type` discriminator. This ensures definition order is preserved.

> **Update (2026-10)**: The grammar AST was rewritten. The structures below describe the current format; the principles (ordered `statements`, `@type` discriminators, camelCase fields) are unchanged.

### Go Data Structures

The AST lives in the `grammar` package (`grammar/ast.go`). The interface types are:

| Interface | Implementations |
|:--|:--|
| `Statement` | `TypeDef`, `RuleDef` |
| `TypeSpec` | `StructSpec`, `AliasSpec`, `TerminalSpec` |
| `TypeExpr` | `TypeRef`, `ListType`, `OptionalType`, `UnionType` |
| `Expr` (parser expressions) | `Ref`, `Literal`, `CharClass`, `Any`, `Seq`, `Choice`, `Repeat`, `Optional`, `And`, `Not`, `Atomic`, `Discard`, `Capture`, `Cut`, `Top`, `Bottom`, `BeginInput`, `EndInput`, `BeginLine`, `EndLine`, `Predicate`, `Attributed`, `Pratt` |
| `Term` (action and predicate expressions) | `IntLit`, `StringLit`, `BoolLit`, `NilLit`, `CaptureRef`, `IndexRef`, `VarRef`, `Member`, `New`, `Call`, `Lambda`, `Binary`, `Unary`, `Assign` |

A rule definition carries its type, body, and action directly:

```go
type RuleDef struct {
    Name   string   `json:"name"`
    Type   TypeExpr `json:"type,omitempty"`
    Expr   Expr     `json:"expr"`
    Action Term     `json:"action,omitempty"`
}
```

Source positions (`Pos`) are not serialized.

### JSON Representation

A value of an interface type is an object whose `@type` is the Go type name. Other fields follow the `json` tags.

```json
{
  "package": "calc",
  "statements": [
    {
      "@type": "TypeDef",
      "name": "Pair",
      "spec": {
        "@type": "StructSpec",
        "fields": [
          { "name": "Key", "type": { "@type": "TypeRef", "name": "Match" } }
        ]
      }
    },
    {
      "@type": "RuleDef",
      "name": "main",
      "expr": {
        "@type": "Seq",
        "items": [
          { "@type": "Capture", "name": "k", "expr": { "@type": "Ref", "name": "key" } },
          { "@type": "Literal", "value": "=" }
        ]
      },
      "action": {
        "@type": "New",
        "type": "Pair",
        "fields": [
          { "name": "Key", "value": { "@type": "CaptureRef", "name": "k" } }
        ]
      }
    }
  ]
}
```

### Implementation Strategy

1.  **Type registry**: Every concrete AST type is registered by name (`grammar/json.go`).
2.  **Reflection-based codec**: `MarshalJSON` and `UnmarshalJSON` walk the structures with reflection, adding or resolving `@type` for interface-typed values. New AST types only need to be registered.
3.  **Two-way conversion**: `grammar.Format` prints a grammar as PEGO source, so JSON and PEGO source can be converted in both directions (`pego convert`, which also reads compiled `.pegoc` files that include the AST; `pego fmt` formats `.pego` source).
4.  **Testing**: A round-trip test covers every AST type (`grammar/json_test.go`).

## Conclusion

This design provides a robust and extensible way to serialize and deserialize the `Grammar` object. The consistent structure for `TypeDef` and `RuleDef` within an ordered `statements` list, combined with a clear naming convention, makes the JSON a faithful representation of the source. This is a critical step towards achieving self-hosting and building advanced tooling.
