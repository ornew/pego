# 038. Versioned Structured Output Schemas

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Expose a deterministic, versioned description of grammar-declared types and inferred rule output shapes for tooling and query consumers. The format distinguishes declared typed values, inferred shapes, CST nodes, captures, and dynamic `any`; it does not promise Tree-sitter JSON compatibility. The API and JSON below are illustrative proposals, not accepted or implemented.

## Motivation

PEGO parses declared `TypeDef` values in `grammar/ast.go`, checks and infers rule results internally, and generates typed Go and TypeScript results ([design 012](012-typed-values.md), [design 030](030-typed-typescript-values.md)). The public `pego.Node` representation also includes reserved CST kinds (`Match`, `Seq`, `List`, `Operator`, `Error`), captures, and scalar fields. A schema consumer cannot treat every Node as a declared AST type or infer a complete schema from observed parses. `pego.Parser` is the public compiled-grammar boundary; `engine.Program` and its inference structures are internal implementation details.

## Goals and non-goals

- Give tools a stable schema-format version, explicit type shapes, and deterministic serialization.
- Preserve declared names separately from inferred rule shapes and record CST, capture, and dynamic output categories.
- Represent unions, optional/nil-capable values, lists, scalar fields, ordered children, and rule provenance without guessing.
- Support schema diffing and query type checking without coupling the format to a target language.
- Do not change grammar checking, parse results, generated Go/TypeScript values, or Node JSON. No Tree-sitter compatibility is implied.

## Proposed public contract

A future public API could be:

```go
type OutputSchema struct { FormatVersion int; Start string; Types []SchemaType; Rules []SchemaRule }
type SchemaOption func(*schemaOptions)
func WithAllSchemaRules() SchemaOption
func (p *Parser) OutputSchema(opts ...SchemaOption) (*OutputSchema, error)
var ErrSchemaUnavailable error
func (s *OutputSchema) MarshalJSON() ([]byte, error)
```

The exact public fields and method names remain proposals. The JSON envelope could be:

```json
{"format":"pego-output-schema","version":1,"start":"file","positionUnitIndependent":true,"types":[],"rules":[]}
```

`version` versions this serialized contract, not grammar semantics. A reader must reject an unsupported version or preserve it opaquely; it must not silently interpret a future format as v1. The configured start rule is the default export root; an explicit method option may request all rules. Version 1 omits a grammar digest because canonical grammar identity is not defined. Consumers must use the supplied schema instance/content for validation and must not assume type names alone establish compatibility across grammars.

Each type entry has an identity and shape. Declared identities use grammar type names; inferred identities use rule names and an explicit inferred origin; reserved CST kinds occupy a separate namespace. Equal shapes do not merge identities. Fields are serialized by UTF-8 name, union alternatives by stable schema identity, and children in parser order. Recursive shapes use named references rather than recursive inline expansion. Declaration order may be included as metadata, but no serialized meaning depends on map iteration.

Field expressions encode `scalar`, `node`, `list`, `optional`, `union`, and `any`. `int`, `string`, and `bool` are scalar kinds, not child nodes. Optionality is grammar-level nil-capability; scalar `0`, empty string and `false` are ordinary values, not evidence that a type is optional. Target-language zero/default mappings are separate metadata; it does not promise a distinct absent-versus-present-nil runtime state or JSON key-presence behavior. Lists remain lists when empty or of length one. Unions retain alternative identities. Inferred `any` remains dynamic and is never narrowed from examples.

A rule entry identifies its declared or inferred result shape and producing rule. It must not imply that a rule's ordinary CST output is a declared struct. CST entries describe ordered child edges and possible nil children; capture entries describe names and known or dynamic value categories. The schema describes possible values, not source spans, runtime values, retained punctuation/comments, or execution order.

## Implementation and determinism

Build the export from the checked grammar/type information behind `pego.Parser`, not generated source, reflection over an arbitrary result, or observed samples. Keep ordering explicit and encode recursive references by identity. Resolve schema options once in order; absent the all-rules option, use the configured start rule. Reject unresolved or unchecked internal shapes rather than emitting partial output. An AST-less loaded parser lacking declared/inferred metadata returns an error matching ErrSchemaUnavailable; bytecode behavior and names alone cannot reconstruct that information. Persisting schemas with compiled artifacts requires a separate versioned-format design. Add stable repeated-export and round-trip checks, with semantic comparisons separate from exact-byte determinism. A future cross-grammar identity field requires a separately specified canonicalization and digest before it is added to the versioned format.

## Compatibility and backend support

Schema export is metadata on a compiled grammar and is independent of runtime backend: Closure, recursive bytecode, and iterative bytecode should return the same schema. Generated Go and TypeScript typed values are already supported; the schema describes their grammar-level shapes without claiming target-language layout or serializer identity. `GenerateGo`/`GenerateTypeScript` remain separate APIs. Grammar JSON and Node JSON are separate formats.

## Implementation sequence

1. Specify envelope evolution, reachability defaults, dynamic values, and nil/cardinality semantics.
2. Add `Parser.OutputSchema` for declared and checked inferred types, then deterministic encoding.
3. Add CST/capture shape descriptions and schema diff support.
4. Let the structural-query compiler ([design 040](040-structural-queries-and-highlighting.md)) type-check patterns against an explicitly supplied schema.

## Alternatives considered

- **Export generated declarations:** backend-specific names/layout would omit CST, dynamic output, and source-level identity. Export checked grammar metadata instead.
- **Infer schema from parsed values:** one sample can hide union alternatives or incorrectly narrow `any`. Do not use observations as type evidence.
- **Reuse Node JSON:** it is a runtime value format and cannot express possible optional/list/union shapes or declared-versus-inferred provenance.
- **Promise Tree-sitter compatibility:** it would impose different node and source-retention semantics. A consumer-specific adapter can be specified separately.

## Testing and performance plan

Cover every grammar type constructor, recursive references, unions, optional/nil-capable and list nesting, scalar fields, inferred `any`, CST-only rules, unreachable definitions, and invalid shapes. Verify deterministic bytes, round trips, unsupported-version handling, backend parity, and typed Go/TypeScript declarations against the schema. Measure export time and allocations on small and large grammars separately from parse and code generation. No measurements or performance promises are available.

## Limitations and open questions

The start-reachable default versus all-rules export and the exact public shape remain implementation choices; the recommendation is start-reachable with an explicit all-rules option. Cross-grammar schema identity remains absent from v1 until canonicalization is defined. Traversal details used by queries follow [design 031](031-iterative-tree-traversal.md); schema types are the proposed dependency for [design 040](040-structural-queries-and-highlighting.md).
