# 038. Versioned Structured Output Schemas

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Expose a deterministic, versioned description of grammar-declared types and inferred rule output shapes for tooling and query consumers. The format distinguishes declared typed values, inferred shapes, CST nodes, captures, and dynamic `any`; it does not promise Tree-sitter JSON compatibility. The Go version-1 export and deterministic semantic diff are implemented. Broader validation remains incomplete.

## Motivation

PEGO parses declared `TypeDef` values in `grammar/ast.go`, checks and infers rule results internally, and generates typed Go and TypeScript results ([design 012](012-typed-values.md), [design 030](030-typed-typescript-values.md)). The public `pego.Node` representation also includes reserved CST kinds (`Match`, `Seq`, `List`, `Operator`, `Error`), captures, and scalar fields. A schema consumer cannot treat every Node as a declared AST type or infer a complete schema from observed parses. `pego.Parser` is the public compiled-grammar boundary; `engine.Program` and its inference structures are internal implementation details.

## Goals and Non-Goals

- Give tools a stable schema-format version, explicit type shapes, and deterministic serialization.
- Preserve declared names separately from inferred rule shapes and record CST, capture, and dynamic output categories.
- Represent unions, optional/nil-capable values, lists, scalar fields, ordered children, and rule provenance without guessing.
- Support schema diffing and query type checking without coupling the format to a target language.
- Do not change grammar checking, parse results, generated Go/TypeScript values, or Node JSON. No Tree-sitter compatibility is implied.

## Public Go Contract

The implemented Go API exposes the engine schema through public aliases:

```go
type OutputSchema struct {
    Format string
    Version int
    Start string
    PositionUnitIndependent bool
    Types []SchemaType
    Rules []SchemaRule
}
func (p *Parser) OutputSchema(opts ...SchemaOption) (*OutputSchema, error)
func WithAllSchemaRules() SchemaOption
var ErrSchemaUnavailable error
func (s *OutputSchema) Validate() error
func (s *OutputSchema) UnmarshalJSON(data []byte) error
func DiffOutputSchemas(before, after *OutputSchema) (*OutputSchemaDiff, error)
```

`encoding/json` marshals the exported fields. `Validate` checks the v1 envelope
and shape graph for both decoded and directly constructed schemas; `UnmarshalJSON`
also rejects unknown JSON fields. Validation rejects cyclic inline shapes but
allows named-reference recursion. This is not complete semantic validation.
Evolution beyond version 1 remains a proposal. `DiffOutputSchemas` returns sorted added, removed, or changed entries for types keyed by namespace-qualified type ID and rules keyed by rule name. It does not classify compatibility. The version-1 JSON envelope is:

```json
{"format":"pego-output-schema","version":1,"start":"file","positionUnitIndependent":true,"types":[],"rules":[]}
```

`version` versions this serialized contract, not grammar semantics. A reader must reject an unsupported version or preserve it opaquely; it must not silently interpret a future format as v1. The configured start rule is the default export root; an explicit method option may request all rules. Version 1 omits a grammar digest because canonical grammar identity is not defined. Consumers must use the supplied schema instance/content for validation and must not assume type names alone establish compatibility across grammars.

Each type entry has an identity and shape. Declared identities use grammar type names; inferred identities use rule names and an explicit inferred origin; reserved CST kinds occupy a separate namespace. Equal shapes do not merge identities. Fields are serialized by UTF-8 name, union alternatives by stable schema identity, and children in parser order. Recursive shapes use named references rather than recursive inline expansion. Declaration order may be included as metadata, but no serialized meaning depends on map iteration.

Field expressions encode `scalar`, `node`, `list`, `optional`, `union`, and `any`. `int`, `string`, and `bool` are scalar kinds, not child nodes. Optionality is grammar-level nil-capability; scalar `0`, empty string and `false` are ordinary values, not evidence that a type is optional. Target-language zero/default mappings are separate metadata; it does not promise a distinct absent-versus-present-nil runtime state or JSON key-presence behavior. Lists remain lists when empty or of length one. Unions retain alternative identities, including a valid empty union from the grammar AST. Inferred `any` remains dynamic and is never narrowed from examples.

A rule entry identifies its declared or inferred result shape and producing rule. It must not imply that a rule's ordinary CST output is a declared struct. CST entries describe ordered child edges and possible nil children; capture entries describe names and known or dynamic value categories. The schema describes possible values, not source spans, runtime values, retained punctuation/comments, or execution order.

## Semantic Schema Diff

`DiffOutputSchemas(before, after)` accepts version-1 schemas and compares type and rule contents by identity. It ignores type/rule entry order, struct-field order, and union-alternative order. It preserves child order and named references. Envelope fields such as `Start` are ignored. Added, removed, and changed results are sorted by type ID or rule name; unchanged inputs return non-nil empty change slices.

The diff rejects unsupported format/version, invalid or duplicate type/rule identities, empty or duplicate field names, shapes without a kind, references without an ID, and cyclic inline shapes. Named `ref` values are compared as written; the diff does not verify that reference targets exist or perform the full external-graph validation described above. JSON unmarshaling performs its own limited v1 graph checks. No source-compatibility or binary-compatibility classification is implied.

## Implementation and Determinism

Build the export from the checked grammar/type information behind `pego.Parser`, not generated source, reflection over an arbitrary result, or observed samples. Keep ordering explicit and encode recursive references by identity. Resolve schema options once in order; absent the all-rules option, use the configured start rule. Reject unresolved or unchecked internal shapes rather than emitting partial output. A loaded parser lacks the checked metadata and returns an error matching ErrSchemaUnavailable, even when its compiled file includes an AST; bytecode behavior and names alone cannot reconstruct that information. Persisting schemas with compiled artifacts requires a separate versioned-format design. Add stable repeated-export and round-trip checks, with semantic comparisons separate from exact-byte determinism. A future cross-grammar identity field requires a separately specified canonicalization and digest before it is added to the versioned format.

## Compatibility and Backend Support

Schema export is metadata on a compiled grammar and is independent of runtime backend: Closure, recursive bytecode, and iterative bytecode should return the same schema. Generated Go and TypeScript typed values are already supported; the schema describes their grammar-level shapes without claiming target-language layout or serializer identity. `GenerateGo`/`GenerateTypeScript` remain separate APIs. Grammar JSON and Node JSON are separate formats.

## Remaining Implementation Sequence

The Go v1 envelope, start-reachable/all-rules selection, declared and checked inferred types, CST/capture categories, deterministic encoding, strict JSON-key and basic graph checks for decoded and directly constructed v1 schemas, and semantic schema diff are implemented. Structural-query compilation validates a supplied schema before using it. Remaining work includes specifying further semantic constraints on externally authored graphs and extended query type checking as described in [design 040](040-structural-queries-and-highlighting.md).

## Alternatives Considered

- **Export generated declarations:** backend-specific names/layout would omit CST, dynamic output, and source-level identity. Export checked grammar metadata instead.
- **Infer schema from parsed values:** one sample can hide union alternatives or incorrectly narrow `any`. Do not use observations as type evidence.
- **Reuse Node JSON:** it is a runtime value format and cannot express possible optional/list/union shapes or declared-versus-inferred provenance.
- **Promise Tree-sitter compatibility:** it would impose different node and source-retention semantics. A consumer-specific adapter can be specified separately.

## Testing and Performance Plan

Cover every grammar type constructor, recursive references, unions, optional/nil-capable and list nesting, scalar fields, inferred `any`, CST-only rules, unreachable definitions, and invalid shapes. Verify deterministic bytes, round trips, unsupported-version handling, backend parity, and typed Go/TypeScript declarations against the schema. Measure export time and allocations on small and large grammars separately from parse and code generation.

## Performance and Results

A synthetic benchmark compares validated JSON decoding with plain DTO decoding
and measures structural-query compilation. On Go 1.27.1, Apple M3 Max
(darwin/arm64), with `GOMAXPROCS=4`, `GOGC=100`, no memory limit and three
alternating baseline/candidate pairs at 200 ms per sample, the medians were:

The baseline was main revision `87a4088`; both binaries used identical
benchmark sources. Plain DTO decoding is a control for measurement noise; it
does not validate the schema. Ratios are medians of the three paired
candidate/baseline ratios, so they need not equal the quotient of the
displayed medians.

| Operation | Types | Baseline ns/op | Candidate ns/op | Time ratio | Bytes/op ratio |
|---|---:|---:|---:|---:|---:|
| Validated JSON decode | 16 | 61,492 | 69,599 | 1.13x | 1.11x |
| Validated JSON decode | 128 | 465,192 | 510,089 | 1.10x | 1.12x |
| Plain DTO decode | 16 | 35,704 | 35,449 | 0.99x | 1.00x |
| Plain DTO decode | 128 | 276,774 | 278,115 | 1.01x | 1.00x |
| Compile query, known field | 16 | 245 | 8,357 | 34.14x | 25.39x |
| Compile query, known field | 128 | 731 | 64,752 | 87.66x | 192.55x |
| Compile query, absent field in shared DAG | 16 | 1,156,038 | 17,910 | 0.02x | 202.55x |
| Compile query, absent field in shared DAG | 128 | 1,183,114 | 75,181 | 0.06x | 683.62x |

The normal query-compilation cost increases because each call now validates
the complete mutable schema. The shared-DAG case becomes substantially faster
because a failed field lookup visits each inline shape once, at the cost of
allocations for complete validation. The strict JSON decoder also checks nested
keys and trailing input and copies input into its buffer. Query compilation
is an explicit preparation step, not part of input parsing. For applications
that compile many queries against one schema, a separate immutable, validated
schema handle could amortize validation without allowing mutation to bypass
checks; this has not been implemented. The benchmarks are in
`internal/engine/output_schema_validate_bench_test.go` and
`internal/engine/structural_query_bench_test.go`. Input parse performance was
not measured by these benchmarks.

## Limitations and Open Questions

The Go v1 API uses a start-reachable default with an explicit all-rules option.
Cross-grammar schema identity remains absent until canonicalization is defined;
complete semantic constraints for externally authored shape graphs and
compatibility classification for diffs are also open. Traversal details used by queries follow [design 031](031-iterative-tree-traversal.md);
schema types are supplied explicitly to the programmatic query compiler described
in [design 040](040-structural-queries-and-highlighting.md).

## Implemented Go API

`engine.Program.OutputSchemaPrototype(start, allRules)` provides the version-1
metadata implementation, exposed through `Parser.OutputSchema` and
`WithAllSchemaRules` in Go. It requires a checked grammar; loaded parsers and
compilation with type checking disabled return `ErrSchemaUnavailable`. It does
not change parse results. The public types mirror the versioned engine schema;
the Go surface and JSON format remain experimental.

The default selects the supplied start rule and its syntactic rule-call closure,
then follows exported shape references to declared and reserved CST types.
`allRules` includes every rule and type, including unreachable definitions.
Types and rules are sorted by identity/name, fields by name, and union
alternatives by their canonical JSON description. Identities use `declared/`,
`inferred/`, and `cst/` namespaces; even declared rule results retain a separate
rule-shape identity and declared provenance. Named recursive references remain
references. Scalar, optional, list, union, dynamic `any`, terminal, and CST/capture
categories are explicit. Ordinary CST children are conservatively described as
ordered optional node lists; their exact positional alternatives are not inferred.
A generic node child retains all reserved CST identities in the reachable export,
so a schema-checked query can name an uncaptured `Match` below a `Seq`.

The envelope decoder rejects unsupported formats and versions. Its v1 graph check
requires unique nonempty type IDs and rule names, nonempty unique field names,
consistent type identities and origins, supported shape kinds and scalar names,
valid shape members for each kind, list/optional elements,
references that resolve to an ID in the schema's type table, and acyclic inline
shapes. Shared acyclic inline shapes are visited once; named-reference cycles
are allowed. The same check is available through
`Validate` for in-memory schemas and runs before structural-query compilation.
The current contract does not define whether `Start` must occur in `Rules`,
whether every rule result must reference its corresponding `inferred/` type,
or whether externally supplied `cst/` entries must exactly match the built-in
reserved kinds and shapes. Persisting schemas in compiled files remains unimplemented. The semantic diff
is implemented separately and does not perform this whole-graph validation. Focused tests cover the limited graph checks
along with determinism, reachability, round trips and categories; export
performance and broader backend/typed-output validation remain open.
