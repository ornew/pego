# 040. Structural Queries and Target-Language Highlighting

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add a declarative query API over PEGO Node results for matching node kinds, rules, fields, child structure, and bounded text predicates, with named captures usable for syntactic highlighting. Type and field checks use an explicitly supplied schema where available. The signatures and syntax below are illustrative proposals, not accepted APIs.

## Motivation

The public Go `pego.Node` and generated TypeScript `Node` represent CST and action-built values; children are ordered, while fields may contain nodes, scalars, or nil. Neither surface has a general query API. Actions can discard punctuation and comments, so AST queries cannot promise lossless tokenization. The edge, order, range, and cycle contract follows [design 031](031-iterative-tree-traversal.md); schema validation depends on [design 038](038-structured-output-schema.md). Grammar LSP analysis and semantic name resolution remain separate.

## Goals and non-goals

- Match node type/rule, named fields, ordered children, and node or scalar captures.
- Type-check known patterns against an explicit schema; retain dynamic `any` as runtime-checked.
- Define deterministic match/capture order, parent paths, shared-edge behavior, and cycle errors.
- Bound traversal, predicates, and result construction with cancellation and work limits.
- Do not promise Tree-sitter query compatibility, semantic resolution, or punctuation/comment visibility absent from results.

## Proposed API and pattern model

The Go surface could be:

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

`WorkLimits`, cancellation errors, and snapshot ownership should reuse [design 033](033-cancellable-parses-and-work-budgets.md) and [design 032](032-versioned-document-snapshots.md); these names are sketches pending those designs. Charge graph edges and result construction to `MaxSteps`, and inspected source text to `MaxInputWork`, following design 033. Cancellation returns the observed context error; exhaustion returns the shared `WorkLimitError` matching `ErrWorkLimit`. Both stop before publishing results. Zero limits follow design 033. Pure structural matching requires no source unit. Position predicates and highlight/range interpretation require an explicitly supplied valid Unit; they never infer a unit from spans.

An illustrative pattern is:

```text
(Identifier @name)
(Binary left: (Identifier @lhs) right: (Identifier @rhs))
```

Patterns constrain a node by type or rule, require child/field structure, and bind captures. Initial syntax should avoid arbitrary host callbacks, unbounded regex, or other operations without a charging rule. The exact grammar and text-predicate operators remain proposed. Text predicates inspect only Node text or a caller-supplied source snapshot; they cannot see discarded punctuation/comments. A TypeScript counterpart could expose `compileQuery(source, schema)` and `query.matches(root, options)`, with position unit, safe-integer work limits, and an optional structural cancellation signal following design 033.

Compilation may receive an `OutputSchema`. Known impossible type/field patterns and incompatible capture categories are compile errors. Dynamic or unknown types remain runtime-checked. Since schema v1 has no cross-grammar digest, the compiled query is tied to the schema content supplied at compilation; it does not claim portable identity across grammars. Without a schema, type and field checks are deferred to evaluation. Captures are always checked against their runtime value categories, even with a schema: mutable Nodes carry no certification that they came from that grammar or still conform to its shape. Mismatches are ordinary nonmatches or explicit query errors according to the operator contract, never unsafe type assertions.

A capture stores its value category, value, immediate parent node, and incoming edge path. Scalar fields are scalar captures, not synthetic Nodes. Optional/nil-capable schema fields do not imply a separate runtime-presence bit. Lists are traversed in index order; empty lists have no elements. A root has no parent. A shared Node is evaluated once per incoming edge so captures retain parent/path context. Cycles on the active path return an error; valid shared DAG paths are allowed.

Enumeration follows the traversal contract: visit a node before its outgoing edges, children by index, fields by lexicographic name, and nested list values by index. Match order is edge order, not span sorting; hand-built Nodes can have equal, empty, or inconsistent spans. Results borrow the supplied graph's Node/scalar values and may retain references after evaluation. Callers must not mutate the graph concurrently or expect results from a live Document Node to remain stable across a later parse. The initial Matches signature accepts only *Node. A snapshot consumer must first Materialize an independently mutable graph and keep that copy stable; include that copying cost in pipeline measurements. Direct SnapshotNode evaluation requires a later read-only adapter and is not supported by this signature.

A highlight configuration maps captures to target-language token classes and priority. It describes syntactic coloring only; it does not resolve symbols or define an editor protocol. Missing punctuation and trivia require a separate lossless-token retention contract.

## Compatibility and backend support

The query operates on common Node values. Core Go, generated Go Node parsers, and generated TypeScript Node parsers are intended implementations and must agree on equivalent inputs. The TypeScript runtime already exports `Node`; ordinary generated TypeScript parsing can feed a TypeScript query implementation. Generated typed Go and typed TypeScript values are not direct query inputs in this design; their adapters require a separate value/identity contract. Traversal scope is represented by design 031; schema checking follows design 038. Document change/version integration is optional and follows design 039.

## Implementation sequence

1. Settle the Node edge/order/range contract in design 031 and schema contract in design 038.
2. Specify the declarative pattern grammar, capture categories, and stable diagnostics.
3. Implement Go compilation/evaluation with functional options, limits, cancellation, and all-or-error results.
4. Implement the equivalent evaluator for TypeScript `Node`; compare results with the Go reference.
5. Add text predicates and highlight configuration, validating their retained-source limitations on real grammars.

## Alternatives considered

- **Copy Tree-sitter query syntax:** it implies different field, node, and lossless-source semantics. Consider an adapter only after PEGO contracts are stable.
- **Query serialized JSON:** it couples matching to Node serialization and allocates an intermediate tree. Evaluate graph values directly.
- **Allow arbitrary Go callbacks:** these are unportable and cannot be bounded reliably. Keep the initial query declarative.
- **Use a lexer for every highlight:** it can recover punctuation but duplicates grammar behavior. Keep it as a consumer choice when actions discard needed tokens.
- **Deduplicate by Node identity:** it erases different parents and capture paths in shared graphs. Preserve edge-oriented evaluation.

## Testing and performance plan

Test pattern diagnostics, known/unknown schema fields, scalar/list/union/dynamic captures, nil children, empty lists, shared DAG paths, cycles, ordering, schema mismatch, cancellation, exact budget thresholds, both position units, bounded text predicates, and discarded trivia. Differentially compare Go and TypeScript query results with a simple reference evaluator. Measure compilation and evaluation separately across graph depth, sharing, fields, predicate text, and match count; track time, allocations, and retained results. No performance results are available.

## Limitations and open questions

The syntax and initial text-predicate set need implementation review; recommendation: a small declarative grammar, materialized all-or-error results under design-033 limits, and text predicates after structural matching is stable. Borrowed result values remain tied to the supplied graph's ownership. Punctuation/comments discarded by actions cannot be highlighted from AST Nodes alone.
