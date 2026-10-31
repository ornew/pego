# 038. Versioned Structured Output Schemas

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Expose a deterministic, versioned description of grammar-declared types and inferred rule output shapes for tooling and query consumers. The format distinguishes declared typed values, inferred shapes, CST nodes, captures, and dynamic `any`; it does not promise Tree-sitter JSON compatibility. The Go export of schema version 2 and the deterministic semantic diff with its compatibility classification are implemented; the schema is exported from the grammar source. The version-2 validation contract includes canonical rule/type relationships, reserved CST identities and strict JSON decoding. Structural queries are checked against the schema, including nested and join shapes.

## Motivation

ZeroParse parses declared `TypeDef` values in `grammar/ast.go`, checks and infers rule results internally, and generates typed Go and TypeScript results ([design 012](012-typed-values.md), [design 030](030-typed-typescript-values.md)). The public `zeroparse.Node` representation also includes reserved CST kinds (`Match`, `Seq`, `List`, `Operator`, `Error`), captures, and scalar fields. A schema consumer cannot treat every Node as a declared AST type or infer a complete schema from observed parses. `zeroparse.Parser` is the public compiled-grammar boundary; `engine.Program` and its inference structures are internal implementation details.

## Goals and Non-Goals

- Give tools a stable schema-format version, explicit type shapes, and deterministic serialization.
- Preserve declared names separately from inferred rule shapes and record CST, capture, and dynamic output categories.
- Represent joins, optional/nil-capable values, lists, scalar fields, ordered children, and rule provenance without guessing.
- Support schema diffing and query type checking without coupling the format to a target language.
- Do not change grammar checking, parse results, generated Go/TypeScript values, or Node JSON. No Tree-sitter compatibility is implied.

## Public Go Contract

The schema types and the comparison are in package `schema`; the parser exports a schema:

```go
// package schema
type Output struct {
    Format string
    Version int
    Start string
    PositionUnitIndependent bool
    SourceInfo bool
    Types []Type
    Rules []Rule
}
func (s *Output) Validate() error
func (s *Output) UnmarshalJSON(data []byte) error
func Compare(before, after *Output) (*Diff, error)

// package zeroparse
func (p *Parser) OutputSchema(opts ...SchemaOption) (*schema.Output, error)
func WithAllSchemaRules() SchemaOption
var ErrSchemaUnavailable error
```

`encoding/json` marshals the exported fields. `Validate` checks the version 2 envelope
and shape graph for both decoded and directly constructed schemas. The
`UnmarshalJSON` contract rejects unknown fields, field names with incorrect
case, and duplicate object keys at every nesting level. Validation rejects
cyclic inline shapes but allows named-reference recursion. Evolution beyond
version 2 is a proposal. `Compare` returns sorted added, removed,
or changed entries for types keyed by namespace-qualified type ID and rules
keyed by rule name. It does not classify compatibility. The version-2 JSON
envelope has this shape. This envelope-only illustration omits required rule
and type entries and is not a valid complete schema:

```json
{"format":"zp-output-schema","version":2,"start":"file","positionUnitIndependent":true,"types":[],"rules":[]}
```

`version` versions this serialized contract, not grammar semantics. A reader must reject an unsupported version or preserve it opaquely; it must not silently interpret a future format as version 2. The configured start rule is the default export root; an explicit method option may request all rules. Version 2 omits a grammar digest because canonical grammar identity is not defined. Consumers must use the supplied schema instance/content for validation and must not assume type names alone establish compatibility across grammars.

Each type entry has an identity and shape. Declared identities use grammar type names; inferred identities use rule names and an explicit inferred origin; reserved CST kinds occupy a separate namespace. Equal shapes do not merge identities. Fields are serialized by UTF-8 name, join alternatives by stable schema identity, and children in parser order. Recursive shapes use named references rather than recursive inline expansion. Declaration order may be included as metadata, but no serialized meaning depends on map iteration.

Field expressions encode `scalar`, `node`, `list`, `optional`, `join`, and `top`. `int`, `string`, and `bool` are scalar kinds, not child nodes. Optionality is grammar-level nil-capability; scalar `0`, empty string and `false` are ordinary values, not evidence that a type is optional. Target-language zero/default mappings are separate metadata; it does not promise a distinct absent-versus-present-nil runtime state or JSON key-presence behavior. Lists remain lists when empty or of length one. Joins retain alternative identities, including a valid empty join from the grammar AST. Inferred `top` remains dynamic and is never narrowed from examples.

A reachable `list` shape also brings the reserved `cst/List` identity into the
export. Parser repetitions and typed list fields are represented by runtime
`Node` values of type `List`; retaining that identity lets schema-checked
queries name the runtime value, including when a list is empty.

A rule entry identifies its declared or inferred result shape and producing rule. It must not imply that a rule's ordinary CST output is a declared struct. CST entries describe ordered child edges and possible nil children; capture entries describe names and known or dynamic value categories. The schema describes possible values, not source spans, runtime values, retained punctuation/comments, or execution order.

Canonical validation requires `Start` to name an entry in `Rules`. Each rule's
`Result` must be a reference exactly to `inferred/<rule-name>`, and that type
entry must exist. The `cst/` namespace is reserved for the built-in CST
identities and their canonical shapes. For these reserved-shape checks, omitted,
nil, and empty optional shape slices are equivalent.

## Semantic Schema Diff

`schema.Compare(before, after)` validates both complete operands with
`Validate` before comparing version 2 type and rule contents by identity. It
ignores type/rule entry order, struct-field order, and join-alternative order.
It preserves child order and named references. Envelope fields such as `Start`
are validated but are not included in the diff result. Added, removed, and
changed results are sorted by type ID or rule name; unchanged inputs return
non-nil empty change slices.

Both operands must pass the full version 2 validation contract, including reference
resolution and canonical rule/type and reserved-CST relationships. After validation, comparison
canonicalizes each graph while memoizing visited shape identities, then compares
the canonical forms. This preserves shared DAG structure instead of serializing
or recursively expanding each reference path, which could multiply work for a
valid graph with extensive sharing.

## Compatibility Classification

Each change of the diff has a class, `Breaking` or `Compatible`, with the impacts and rules that
produced it; the rule table is in the
[runtime specification](../../../spec/runtime/schemas-and-queries.md#comparing-schemas).

The criterion: a change is breaking when an existing reader, including code written against the generated types,
may stop compiling or stop handling a value. It is compatible only when it adds something a reader can ignore.

The reason is the generated types. A reader of the output holds Go structs, `*T` for optional values, interfaces for
joins and TypeScript types, and the generator derives them from the type IDs and shapes. A judgment by the set of
values alone would call narrowing safe, because every old value still fits. But `join[int, string]` to `int` or
`optional[T]` to `T` changes the generated Go type, and the reader no longer compiles. Widening breaks readers at run
time, because they meet a value they do not handle, for example a new join alternative that an exhaustive switch does
not cover. So every change to an existing shape is breaking, in both directions: narrowing, widening, optional to
required and back, a changed scalar, kind or referenced type ID, a tuple with another arity, and a changed
capture location. Removals are breaking, referenced or not.

Only additions are compatible: a type, a rule, a struct field or a capture that an existing reader does not read. A
rule whose origin flips while its ID and canonical shape stay the same is compatible, because the generated type name
follows the ID. A rule's result always refers to its own inferred type, so a change in what a rule produces appears as
a change of that type.

An alternative that was considered is a class by value set, with narrowing compatible. It would pass a schema that
breaks the compilation of its consumers, so it was rejected.

The exit status of `zp schema diff` does not depend on the class; a script reads the class from the output.

## Implementation and Determinism

Build the export from the checked grammar/type information behind `zeroparse.Parser`, not generated source, reflection over an arbitrary result, or observed samples. Keep ordering explicit and encode recursive references by identity. Resolve schema options once in order; absent the all-rules option, use the configured start rule. Reject unresolved or unchecked internal shapes rather than emitting partial output. The tests check repeated exports and round trips, with semantic comparisons separate from exact-byte determinism. A future cross-grammar identity field requires a separately specified canonicalization and digest before it is added to the versioned format.

## Persistence

A compiled grammar holds only the bytecode of one mode ([Compiled Grammars](../../../spec/runtime/compiled-grammars.md)), so the schema is not persisted. A parser loaded from a compiled grammar returns an error matching `ErrSchemaUnavailable` that asks for the schema to be exported from the grammar source; a program that needs the schema next to a compiled grammar exports it with `zp schema` and ships the JSON file.

## Compatibility and Backend Support

Schema export is metadata on a compiled grammar and is independent of runtime backend: Closure, recursive bytecode, and iterative bytecode return the same schema, and the tests check this agreement. Generated Go and TypeScript typed values are already supported; the schema describes their grammar-level shapes without claiming target-language layout or serializer identity. `gen.Go`/`gen.TypeScript` are separate APIs. Grammar JSON and Node JSON are separate formats.

## Schema Consumers

Structural queries are checked against the schema. The query compiler validates a supplied schema before using it and checks nested and join patterns against it. Further schema consumers are described in [design 040](040-structural-queries-and-highlighting.md).

## Alternatives Considered

- **Export generated declarations:** backend-specific names/layout would omit CST, dynamic output, and source-level identity. Export checked grammar metadata instead.
- **Infer schema from parsed values:** one sample can hide join alternatives or incorrectly narrow `top`. Do not use observations as type evidence.
- **Reuse Node JSON:** it is a runtime value format and cannot express possible optional/list/join shapes or declared-versus-inferred provenance.
- **Promise Tree-sitter compatibility:** it would impose different node and source-retention semantics. A consumer-specific adapter can be specified separately.

## Testing and Performance

The tests cover the export, decoding and validation of the JSON contract, the semantic diff, the refusal of schema export by loaded compiled grammars and the backend agreement. They are in `schema/*_test.go` (decoding, validation, the diff and its classification), `internal/schema/*_test.go` (the export), `internal/engine/output_schema_*_test.go` and `internal/engine/compiled_schema_test.go`. Export time and allocations are measured separately from parse and code generation, with `internal/engine/output_schema_export_bench_test.go`, `internal/engine/output_schema_validate_bench_test.go` and `schema/diff_bench_test.go`.

## Performance and Results

The focused comparison used Go 1.27.1 on an Apple M3 Max
(darwin/arm64), with `GOMAXPROCS=4`, `GOGC=100`, no memory limit, and three
alternating pairs at 200 ms per sample. The baseline is the diff and decoder without the strict
version 2 contract and the memoized canonical diff. Both
variants used identical benchmark sources and fixture files with 16 or 128 declared
types plus the same number of inferred types and rules. Ratios below are
medians of paired candidate/baseline ratios, with the per-pair range shown
where useful.

| Operation | Fixture | Baseline → candidate ns/op | Paired time ratio (range) | B/op, baseline → candidate | Allocs/op, baseline → candidate |
|---|---|---:|---:|---:|---:|
| Validated JSON decode | 16 | 75,525 → 75,373 | 1.001 (0.957–1.007) | 67,395 → 34,111 | 246 → 231 |
| Validated JSON decode | 128 | 578,654 → 565,361 | 0.972 (0.952–1.020) | 543,778 → 280,796 | 1,637 → 1,617 |
| Compile query, known field | 16 | 8,774 → 9,249 | 1.054 (1.004–1.076) | 7,600 → 7,600 | 19 → 19 |
| Compile query, known field | 128 | 67,726 → 70,068 | 1.035 (1.019–1.053) | 57,872 → 57,872 | 25 → 25 |
| Diff, ordinary fixture, changed or unchanged | 16 | 74,962–78,186 → 48,959–53,375 | 0.668–0.683 (0.652–0.711) | 100,234–100,269 → 108,385–108,432 | 601–602 → 589–590 |
| Diff, ordinary fixture, changed or unchanged | 128 | 589,184–615,484 → 413,584–413,633 | 0.707–0.708 (0.651–0.731) | 795,463–795,490 → 865,603–865,651 | 4,634–4,635 → 4,203–4,204 |
| Diff, shared DAG, depth 4 | 4 | 97,875 → 9,795 | 0.106 (0.098–0.106) | 65,869 → 19,264 | 431 → 145 |
| Diff, shared DAG, depth 8 | 8 | 2,628,192 → 16,668 | 0.0064 (0.0049–0.0064) | 1,846,562 → 30,304 | 6,790 → 231 |

The JSON decode timing ratios include parity; cumulative allocation roughly
halved. Known-field query compilation was 3–5% slower with the same allocation
counts. Ordinary diff cases used less time and fewer allocations, with about
8–9% more cumulative bytes; shared-DAG cases used substantially less time and
allocated less. These are synthetic preparation/validation workloads, not
parser input-path measurements.

Retaining the `cst/List` identity is measured with `internal/engine/output_schema_export_bench_test.go`,
with Go 1.27.1 on an
Apple M3 Max (darwin/arm64), `GOMAXPROCS=4`, `GOGC=100`, no memory limit, and
three alternating serial pairs at 200 ms per sample. A reachable list
shape adds the reserved `cst/List` type and its generic-node CST closure. Against an export that omits the identity, small
repetition export rises from 1,368 to 2,681 ns/op and export-plus-JSON from
2,602 to 6,280 ns/op (serialized JSON size 392 to 1,015 bytes). For one list field, export
rises from 1,964 to 3,975 ns/op and export-plus-JSON from 3,741 to 8,509 ns/op
(serialized JSON size 588 to 1,288 bytes). With 128 list fields, export rises
from 18,012 to 21,643 ns/op and export-plus-JSON from 64,022 to 70,515 ns/op
(serialized JSON size 14,304 to 15,004 bytes; export allocation bytes 64,488
to 69,264 B/op, with 23 additional
allocations). The scalar control kept size and allocation counts unchanged,
while timing ranges crossed parity. This is a focused schema-export tradeoff
for retaining the runtime `List` identity, not a parser-path measurement.

## Limitations and Open Questions

The Go API uses a start-reachable default with an explicit all-rules option.
Cross-grammar schema identity is not defined until canonicalization is specified;
Traversal details used by queries follow [design 031](031-iterative-tree-traversal.md);
schema types are supplied explicitly to the programmatic query compiler described
in [design 040](040-structural-queries-and-highlighting.md).

## Implemented Go API

`schema.Export(program, start, allRules)` provides the schema version 2
implementation, exposed through `Parser.OutputSchema` and
`WithAllSchemaRules` in Go. It requires a checked grammar: a parser
loaded from a compiled grammar returns `ErrSchemaUnavailable`. It does not change parse results. The public types mirror the
versioned engine schema; the Go surface and JSON format are experimental.

The default selects the supplied start rule and its syntactic rule-call closure,
then follows exported shape references to declared and reserved CST types.
`allRules` includes every rule and type, including unreachable definitions.
Types and rules are sorted by identity/name, fields by name, and join
alternatives by their canonical JSON description. Identities use `declared/`,
`inferred/`, and `cst/` namespaces; even declared rule results retain a separate
rule-shape identity and declared provenance. Named recursive references remain
references. Scalar, optional, list, join, dynamic `top`, terminal, and CST/capture
categories are explicit. Ordinary CST children are conservatively described as
ordered optional node lists; their exact positional alternatives are not inferred.
A grammar with `#recover` or `#missing` exports `Error` or `Missing`, whose
nodes can replace any node or list. A generic node child, a terminal or a
dynamic `top` value retains all reserved CST identities in the reachable export,
so a schema-checked query can name an uncaptured `Match` below a `Seq`.

The envelope decoder rejects unsupported formats and versions, unknown fields,
incorrect field-name case, and duplicate keys at every JSON object level. Its
version 2 graph check requires unique nonempty type IDs and rule names, nonempty unique
field names, consistent type identities and origins, supported shape kinds and
scalar names, valid shape members for each kind, list/optional elements,
references that resolve to an ID in the schema's type table, and acyclic inline
shapes. It also requires `Start` to occur in `Rules`, every rule result to refer
to its matching `inferred/<rule-name>` type, and reserved `cst/` entries to
match built-in identities and shapes. Omitted, nil, and empty optional shape
slices are equivalent for reserved-shape validation. Shared acyclic inline
shapes are visited once; named-reference cycles are allowed. The same full
validation applies to in-memory schemas and to both operands of semantic diff; envelope metadata is not itself reported as a diff.
