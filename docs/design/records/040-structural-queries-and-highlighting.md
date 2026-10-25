# 040. Structural Queries and Target-Language Highlighting

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add a declarative query API over PEGO Node results for matching node kinds, rules, fields, child structure, and bounded text predicates, with named captures usable for syntactic highlighting. Type and field checks use an explicitly supplied schema where available. The programmatic Go subset described under Implemented Go API is implemented; the illustrative text syntax, text predicates, highlighting, and TypeScript support remain deferred.

## Motivation

The public Go API provides programmatic structural queries over `pego.Node`; generated TypeScript `Node` does not yet have a query API. Both represent CST and action-built values with ordered children and fields that may contain nodes, scalars, or nil. Actions can discard punctuation and comments, so queries cannot promise lossless tokenization. The edge, order, range, and cycle contract follows [design 031](031-iterative-tree-traversal.md); schema validation depends on [design 038](038-structured-output-schema.md). Grammar LSP analysis and semantic name resolution remain separate.

## Goals and Non-Goals

- Match node type/rule, named fields, ordered children, and node or scalar captures.
- Type-check known patterns against an explicit schema; retain dynamic `top` as runtime-checked.
- Define deterministic match/capture order, parent paths, shared-edge behavior, and cycle errors.
- Bound traversal, predicates, and result construction with cancellation and work limits.
- Do not promise Tree-sitter query compatibility, semantic resolution, or punctuation/comment visibility absent from results.

## Proposed API and Pattern Model

The following text-query API is illustrative and is not implemented. The implemented Go API compiles a `QueryPattern` programmatically, as described under Implemented Go API below.

```go
type QueryOption func(*queryOptions)
type Query struct { /* compiled pattern */ }
type QueryMatch struct { Captures []QueryCapture }
func CompileQuery(source string, schema *OutputSchema) (*Query, error)
func (q *Query) Matches(root *Node, opts ...QueryOption) ([]QueryMatch, error)
func WithQueryUnit(unit Unit) QueryOption
func WithQueryContext(ctx context.Context) QueryOption
func WithQueryWorkLimits(limits WorkLimits) QueryOption
```

The implemented Go API accepts context and step-limit options. Structural matching inspects no source text, so it consumes no input-work units; its step budget covers traversal and result construction. Cancellation returns the context error, exhaustion matches `ErrWorkLimit`, and evaluation returns no partial result. Position predicates and highlight/range interpretation remain deferred; any future position-based behavior must require an explicit valid Unit rather than infer one from spans.

An illustrative pattern is:

```text
(Identifier @name)
(Binary left: (Identifier @lhs) right: (Identifier @rhs))
```

Patterns constrain a node by type or rule, require child/field structure, and bind captures. Initial syntax should avoid arbitrary host callbacks, unbounded regex, or other operations without a charging rule. The exact grammar and text-predicate operators remain proposed. Text predicates inspect only Node text or a caller-supplied source snapshot; they cannot see discarded punctuation/comments. A TypeScript counterpart could expose `compileQuery(source, schema)` and `query.matches(root, options)`, with position unit, safe-integer work limits, and an optional structural cancellation signal following design 033.

Compilation may receive an `OutputSchema`. The implemented Go subset performs a narrow scalar check when a query names a declared struct field directly and its shape is scalar or optional scalar: for example, `Value *string` permits `string` and `nil` categories and rejects other known categories. It follows declared type-alias chains that resolve to those shapes. Nested patterns are checked against the shapes of the values they apply to; a join needs one alternative that the pattern can match. Unresolved references remain runtime-checked. Dynamic or unknown types remain runtime-checked. Since schema v1 has no cross-grammar digest, the compiled query is tied to the schema content supplied at compilation; it does not claim portable identity across grammars. Without a schema, type and field checks are deferred to evaluation. Captures are always checked against their runtime value categories, even with a schema: mutable Nodes carry no certification that they came from that grammar or still conform to its shape. Mismatches are ordinary nonmatches or explicit query errors according to the operator contract, never unsafe type assertions.

A capture stores its value category, value, immediate parent node, and incoming edge path. Scalar fields are scalar captures, not synthetic Nodes. Optional/nil-capable schema fields do not imply a separate runtime-presence bit. Lists are traversed in index order; empty lists have no elements. A root has no parent. A shared Node is evaluated once per incoming edge so captures retain parent/path context. Cycles on the active path return an error; valid shared DAG paths are allowed.

Enumeration follows the traversal contract: visit a node before its outgoing edges, children by index, fields by lexicographic name, and nested list values by index. Match order is edge order, not span sorting; hand-built Nodes can have equal, empty, or inconsistent spans. Results borrow the supplied graph's Node/scalar values and may retain references after evaluation. Callers must not mutate the graph concurrently or expect results from a live Document Node to remain stable across a later parse. The initial Matches signature accepts only *Node. A snapshot consumer must first Materialize an independently mutable graph and keep that copy stable; include that copying cost in pipeline measurements. Direct SnapshotNode evaluation requires a later read-only adapter and is not supported by this signature.

A highlight configuration maps captures to target-language token classes and priority. It describes syntactic coloring only; it does not resolve symbols or define an editor protocol. Missing punctuation and trivia require a separate lossless-token retention contract.

## Compatibility and Backend Support

The implemented query operates on Go `Node` values returned by the core parser or generated Go Node parsers. Generated TypeScript parsers expose their own `Node` values, but a TypeScript query implementation remains deferred; equivalent results are a future parity goal. Generated typed Go and typed TypeScript values are not direct query inputs in this design; their adapters require a separate value/identity contract. Traversal scope is represented by design 031; schema checking follows design 038. Document change/version integration is optional and follows design 039.

## Remaining Implementation Sequence

The programmatic Go API and Node traversal contract in design 031 are implemented. Remaining work is to specify a declarative pattern grammar and stable diagnostics, add TypeScript parity, define bounded text predicates, and design highlight configuration around the retained-source limitations.

## Alternatives Considered

- **Copy Tree-sitter query syntax:** it implies different field, node, and lossless-source semantics. Consider an adapter only after PEGO contracts are stable.
- **Query serialized JSON:** it couples matching to Node serialization and allocates an intermediate tree. Evaluate graph values directly.
- **Allow arbitrary Go callbacks:** these are unportable and cannot be bounded reliably. Keep the initial query declarative.
- **Use a lexer for every highlight:** it can recover punctuation but duplicates grammar behavior. Keep it as a consumer choice when actions discard needed tokens.
- **Deduplicate by Node identity:** it erases different parents and capture paths in shared graphs. Preserve edge-oriented evaluation.

## Testing and Performance Plan

Test pattern diagnostics, known/unknown schema fields, scalar/list/join/dynamic captures, nil children, empty lists, shared DAG paths, cycles, ordering, schema mismatch, cancellation, exact budget thresholds, both position units, bounded text predicates, and discarded trivia. Differentially compare Go and TypeScript query results with a simple reference evaluator. Measure compilation and evaluation separately across graph depth, sharing, fields, predicate text, and match count; track time, allocations, and retained results. No performance results are available.

## Limitations and Open Questions

The syntax and initial text-predicate set need implementation review; recommendation: a small declarative grammar, materialized all-or-error results under design-033 limits, and text predicates after structural matching is stable. Borrowed result values remain tied to the supplied graph's ownership. Punctuation/comments discarded by actions cannot be highlighted from AST Nodes alone.

## Implemented Go API

`CompileStructuralQuery(QueryPattern, *OutputSchema)` accepts a programmatic
pattern rather than adopting the illustrative text syntax. Validating the schema
dominates its cost, so `PrepareQuerySchema` validates a schema once into an
immutable handle that owns a deep copy, keeping inline DAGs shared, and indexes
rules and types by name and ID; its `CompileStructuralQuery` method performs the
same pattern checks without validating again. Patterns
may constrain type, producing rule, runtime category, named fields, and an
ordered child prefix. Required fields must exist; extra fields and children are
allowed. A capture binds the value at that pattern. Node patterns always check
the mutable runtime graph even when a schema was supplied. Known type/rule names
and impossible fields on declared structs are rejected during compilation. For
a directly named struct field with an optional scalar shape (for example,
`Value *string`), the scalar category and `nil` are accepted, while other known
categories are rejected. Declared aliases are followed when they resolve to a
scalar or optional scalar shape; unresolved references remain conservative.
Nested field and child patterns are checked against the shape of the value
they apply to, existentially: a join or optional shape accepts a pattern that
one alternative can match, and compilation rejects only patterns that no value
of the shape can match. Requiring every alternative to match was rejected
because it would refuse queries that match real trees. A differential test
derives patterns from the values of parsed corpus grammars and requires them to
compile against those grammars' schemas, including recovered parses. `Error`
and `Missing` recovery nodes can replace any node or list; their fields
(`message`, and `expected` for `Missing`) are known to the check although the
schema's reserved shapes do not list them. A failing join reports only its
first alternative's reason, so error text stays linear in the pattern depth.
The runtime value is still checked because a `Node` may have changed since it was
parsed. Dynamic CST/`top` fields remain runtime checked. Pattern copies are independent of caller mutation. Compilation rejects
nesting at 256 levels, including cyclic programmatic pattern containers.

`StructuralQuery.Matches(root, QueryOptions)` returns all results or an error.
Evaluation is iterative and visits each node occurrence before its outgoing
edges: children in index order, then fields in lexicographic order, then nested
`[]any` elements in index order. Pattern captures follow preorder with children
before sorted fields. An optional child can be captured as nil. Scalar categories
are `int`, `string`, and `bool`; `list` identifies an explicit `[]any` container,
while ordinary parser repetition results remain `Node` values of type `List`.
Unsupported runtime values return an error. Active-path node and list cycles
return `ErrQueryCycle`, and shared DAG nodes are evaluated per incoming edge.
Nonempty list identity uses its first element address and visible length; slice
capacity does not affect cycle detection.
The immediate parent is the containing Node even through nested list containers;
a root has no parent. Each capture owns its root-relative edge-path slice but
borrows its value and parent. Concurrent graph mutation is unsupported.

Cancellation uses the supplied context error. Step exhaustion uses the existing
`WorkLimitError`/`ErrWorkLimit`, and zero limits are unlimited. Structural queries
consume no input-work units because they inspect no source text. Steps charge
visited values, pattern checks, outgoing edge/field discovery, field indexing,
path construction/copying, captures, and matches. Cancellation is checked at every
reservation and before returning results. Field sorting is preceded by a linear
reservation; the budget is a deterministic abstract-work bound, not a count of
sort comparisons. Full edge paths currently incur quadratic copying on a deep
chain, and these copies are charged before construction. No speed or allocation
claims are made.

The public Go facade exposes `CompileStructuralQuery`, `PrepareQuerySchema`,
`QueryPattern`, capture/path types, `StructuralQuery.Matches`, and
context/work-limit options. It preserves per-edge results and all-or-error
behavior. The implemented pattern language is
programmatic; text syntax, text predicates, position/range queries, highlighting,
TypeScript parity, and direct snapshot evaluation remain deferred. Discarded
punctuation and comments cannot be recovered from Node values.
