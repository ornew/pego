# 031. Iterative Tree Traversal and JSON Output

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

PEGO exposes mutable parse-result `Node` values, but has no general-purpose traversal or range-navigation API. This proposal first makes Go JSON output iterative and adds a streaming writer, then adds an explicit-stack walker and cursor for `Node` graphs. It defines edge order, shared-node behavior, cycles, range units, empty spans, and ownership without changing the existing JSON representation.

The Walker, Cursor and Go JSON writer APIs below are proposals. An experimental
[Go JSON implementation](https://github.com/ornew/pego/commit/a1365b0019534567435fa4aa42b0587b43761988)
has focused correctness coverage but is not integrated into the default runtime.
Walker, cursor and additional TypeScript traversal stages remain unimplemented.

## Motivation

`internal/engine/node.go` and the generated Go runtime in `internal/engine/genrt/runtime.go` expose nodes with `Children` and `Fields`; fields may refer to nodes and are mutable through the Go representation. `Node.Clone` preserves shared subtrees. Go `Node.MarshalJSON` currently delegates to `json.Marshal` on a `nodeJSON` containing child nodes, and `Fields.MarshalJSON` calls `json.Marshal` for each field value. Those paths recursively marshal nested nodes. `Node.String` also descends through `writeNode`/`writeValue`.

The TypeScript runtime in `internal/engine/tsrt/runtime.ts` already formats `toString()` and exports `marshal(Node | null)` iteratively with a `Pending` stack. `marshal` preserves Go-compatible JSON field order, BigInt number formatting, null children, and escaping. Separately, `toJSON()` constructs nested objects and converts BigInt to `Number`; native `JSON.stringify` controls recursion on that object representation. There is no general walker or cursor in the engine API. `parsers/duckdb/walk.go` has a parser-specific `Walk(node any, fn func(node any) bool)` utility, but it is not a common Node API.

These are code-shape observations, not measurements of a particular stack limit or a claim that normal parser results fail. The mutable, exported Go node graph can also be hand-built with shared children or cycles; these cases need explicit API behavior rather than assumptions based on parser-produced trees. Design record 019 identifies traversal order, identity, cycles, scalar values, range units, and empty-node boundaries as contracts to settle.

## Goals

- Provide deterministic, non-recursive traversal over node, child, field, and list edges.
- Make visits edge-oriented by default so shared subtrees appear once per incoming edge.
- Detect cycles on the active path and return a stable error instead of looping.
- Provide range navigation in an explicit position unit, with specified half-open and zero-width behavior.
- Provide iterative Go JSON output that preserves existing acyclic JSON shape, field order, escaping, and ordinary encoding errors; allow writing to an `io.Writer` without requiring a complete output buffer.
- Preserve current Node mutability and ownership. A traversal observes the supplied graph and does not clone, freeze, or retain it after returning.

## Non-Goals

- Immutable or versioned snapshots, concurrent mutation safety, structural sharing, or edit notifications.
- Changing generated parser JSON fields, omitting rules/text/children/fields differently, or changing number representations.
- Making JavaScript's native `JSON.stringify` iterative or changing its global behavior.
- Defining a general serializer for arbitrary Go values stored inside `Fields`; the safe writer explicitly supports Nodes, nested `[]any`, `[]byte`, and scalar leaves.
- Replacing parser-specific walkers unless a later change demonstrates that sharing the API is useful.

## Design

### Public Behavior and Contracts

#### Walker

Add a stateless function with an explicit visit record and action:

```go
type WalkAction uint8

const (
    WalkContinue WalkAction = iota
    WalkSkip
    WalkStop
)

type WalkVisit struct {
    Value  any   // node, scalar, nil, or list value at this edge
    Node   *Node // set when Value is a non-nil *Node
    Parent *Node // nil for the root
    Depth  int
    Edge   WalkEdge
}

type WalkEdge struct {
    Kind       WalkEdgeKind // root, child, field, or list element
    ChildIndex int
    FieldName  string
    ListIndex  int
}

func Walk(root *Node, visit func(WalkVisit) (WalkAction, error)) error
```

`WalkEdge` identifies root, child index, field name, or list index. `Depth` counts incoming edges, with zero for the root; `Parent` is the nearest owning Node for scalar and list edges. A callback error stops traversal and is returned unchanged; `WalkStop` ends successfully; `WalkSkip` skips descendants of this visit; `WalkContinue` visits them. The callback is invoked for the root and for each child/field/list edge, including scalar and nil field/list values, so consumers can inspect values without treating them as nodes. Nil root is one root visit with `Value == nil`, `Node == nil`. Nil node/list values likewise produce an edge visit but no descendants.

Traversal is iterative and edge-oriented: a shared node is visited once per incoming edge and has that edge's parent. Do not include node-identity deduplication in the initial API; add it only for a concrete consumer. Cycle detection tracks the active path and returns a `CycleError` containing the edge path for a back-edge, while allowing shared DAGs. No traversal mutates the graph.

For deterministic order, visit a node before its outgoing edges; traverse `Children` in slice order, then fields in lexicographic field-name order, then nested `[]any` values in index order. Field ordering matches Go JSON output; child and list order matches serialized array order. A field value that is a `*Node` creates a node edge. A `[]any` value creates list-index edges recursively; scalar, nil, and other values are leaves. Map-valued fields are not traversed: the current grammar value model does not promise deterministic map ordering. Cycle tracking identifies active nonempty `[]any` containers by the address of their first element plus length; capacity is irrelevant because only the visible length is traversed. Empty slices have no active identity. Distinct shorter or overlapping views are not cycles merely because they share a start address. It detects a repeated active view that refers back to itself; the same container reached on separate non-active paths is visited again.

The engine's declared field values are nodes, integers, strings, booleans, or nil; the generated Go runtime uses `any`. The generated TypeScript runtime models values as nodes, numeric/bigint, strings, booleans, or null. The common walker should visit those declared values consistently; Go-only list support is a convenience for hand-built graphs and must be documented as such.

#### Cursor and Ranges

Add a `Cursor` or range-query helper layered on the walker. Each query requires a position unit: `CodePoints` or `Bytes`, matching parser options and the unit used to populate `Start`/`End`. It does not infer a unit from the node. The range query is half-open `[start,end)`. A non-empty node span `[Start,End)` matches a point cursor at `p` when `Start <= p < End`, and matches a non-empty query range when the spans overlap. A zero-width node at `p` matches a point cursor at `p`; in a non-empty range it matches when `start <= p < end`. An empty query range matches no spans; use the point-cursor operation for a caret position.

Range matching filters visits but does not prune solely from a parent's span: callers can construct Nodes whose parent and child spans are inconsistent. A later optimized cursor may prune only under a documented validated-tree invariant. Negative or reversed query ranges return an argument error. Positions outside a known source length are not rejected because a Node alone does not carry that length.

Range helpers report node spans only. Scalar fields have no independent source span and are visible through their owning node's field-edge callback. Lists likewise have no span of their own; their node elements are matched by their own ranges. Empty lists and nil list entries produce no node span.

#### Go JSON Writer

Add an iterative `WriteJSON(io.Writer) error` method and implement `MarshalJSON` using the same encoder. `(*Node)(nil).WriteJSON(w)` writes the JSON value `null` to `w`; `MarshalJSON` on a nil receiver returns `null` without error, matching the behavior of `json.Marshal((*Node)(nil))`. The writer emits the current object shape and field order: `type`, optional `rule`, `start`, `end`, optional `text`, optional `children`, optional `fields`; fields remain lexicographically sorted. Children remain array-ordered. JSON string escaping and scalar encoding continue to use `encoding/json` behavior, including errors for unsupported values and invalid numbers. The output bytes for acyclic values must remain byte-for-byte compatible with current `json.Marshal` output.

The implementation keeps an explicit stack of node, field, array, and `[]any` frames. It delegates scalar encoding and string/key escaping to `encoding/json`, while recognizing Node and nested `[]any` edges itself. `[]byte` is encoded through `encoding/json` to preserve its existing base64 string representation. It tracks active Node and `[]any` identities to reject cycles with a `json.UnsupportedValueError`-compatible error; shared values outside the current path are encoded again at each edge. The writer does not close or flush the caller's writer, and may leave a valid JSON prefix written when an error occurs. `MarshalJSON` buffers the same writer output and returns no bytes on error, as required by `json.Marshaler` convention.

This writer handles node graphs, nested `[]any`, `[]byte`, and declared scalar field values. Other container types, maps, and custom marshalers are delegated to `encoding/json` as leaves; nested Nodes inside those unsupported containers are outside the iterative guarantee. The implementation must not silently route a recognized Node or `[]any` back through recursive `json.Marshal`.

`Fields.MarshalJSON` uses the same iterative value encoder for each field while retaining its existing sorted-key object shape, so directly marshaling a `Fields` value also handles supported nested Node and `[]any` edges.

Today `Node.MarshalJSON` errors encountered while `encoding/json` marshals a node graph are wrapped by `encoding/json` in `*json.MarshalerError`; nested Node boundaries can add wrappers. The iterative implementation must preserve `errors.As` behavior and the underlying custom error identity. Whether exact wrapper nesting and error strings can remain identical depends on where `WriteJSON` returns errors and must be pinned by tests before making that stronger compatibility claim. Byte-for-byte compatibility applies to successful acyclic output.

#### TypeScript Output

Keep and test the existing exported `marshal(Node | null): string` API, which already iteratively emits Go-compatible JSON, including exact BigInt integer formatting, null children, field ordering, and Go string escaping. Preserve `toJSON()` and its object shape; it separately converts BigInt to `Number`, so native `JSON.stringify(node)` can be inexact for large integers and remains subject to native serializer recursion. The already-iterative `toString()` remains unchanged in output and quoting rules. Do not add another TypeScript JSON-string API in this proposal.

#### Ownership and Mutation

All APIs operate on the live Node graph. They take no ownership and retain no references after returning. Callers must not mutate any reachable node, child slice, or field slice concurrently with a walk or serialization. The APIs do not make a deep copy. This keeps the mutable Node and `Clone` contracts intact and leaves snapshots to the separate versioned-tree design.

### Implementation and Invariants

Use explicit frames containing the current node/value, next child/field/list index, parent edge, and enter/exit state. Cycle tracking uses an active-path set for Node pointers and supported `[]any` containers. State is per call and discarded on return. Fields are visited through a sorted view or index order computed without mutating `Fields`.

The JSON writer uses the same ordered edge model, but emits object delimiters and commas from frame state. Keep Node and field formatting centralized so `MarshalJSON` and `WriteJSON` cannot drift. Pin zero-value, nil, and error wrapping behavior before implementation. A cycle is a newly handled graph condition; ordinary encoding errors preserve their underlying error identity and `errors.As` behavior, while exact wrapping layers are a testable compatibility question.

Generated Go parsers embed `internal/engine/genrt/runtime.go`, so the core and generated runtime must implement identical behavior and be covered by generator parity tests. TypeScript is a separate compatibility surface and should not claim safe native `JSON.stringify` at arbitrary depth.

### Compatibility and Backend Support

The proposed Go traversal/cursor APIs will be tested against both `internal/engine` and generated Go runtime behavior. The proposed Go JSON stage must preserve successful acyclic output bytes in both `internal/engine` and the embedded `internal/engine/genrt/runtime.go`. TypeScript keeps the existing `marshal` and `toJSON` API contracts. No grammar syntax, parser execution, parse positions, mutable ownership, or generated parser API changes are required.

### Implementation Sequence

1. Pin current Go JSON bytes, field ordering, escaping, nil behavior, and error wrapping in core and generated runtime tests; cover the existing TypeScript `marshal` contract.
2. Add iterative Go `WriteJSON` for Nodes and nested `[]any`, then route `MarshalJSON` through it and add generated-runtime parity coverage.
3. Add stateless edge-oriented `Walk` with continue/skip/stop/error callbacks and active-path cycle errors.
4. Add point and half-open range queries with both position units and empty-span rules.
5. Add TypeScript traversal/range APIs only as a separate compatibility step; preserve existing `marshal` and `toJSON` behavior.

Each stage should be independently reviewable. A versioned output schema remains separate from this traversal contract.

## Alternatives Considered

- **Recursive visitor callbacks:** smaller implementation, but traversal depth follows input shape and cycle handling is harder to make uniform. Prefer explicit frames for the deep-tree use case; performance is unmeasured.
- **Unique-node traversal:** avoids repeated work on shared DAGs, but loses edge context and may omit a subtree's distinct parent/field relationship. Defer it until a concrete consumer requires graph identity semantics.
- **Reject all repeated node identities:** simple cycle policy, but incorrectly rejects valid shared subtrees. Detect only active-path back-edges.
- **Change Node to immutable snapshots first:** would simplify concurrent traversal and identity semantics, but changes ownership and public mutability. Defer to the snapshot design; do not make this API imply immutability.
- **Use only recursive `json.Marshal` / native `JSON.stringify`:** preserves familiar entry points, but retains recursive traversal. Add an iterative Go writer and preserve TypeScript's existing iterative `marshal`; native JavaScript serialization remains outside the depth guarantee.

## Testing

The proposed test plan covers:

- Deep generated and hand-built nodes for walker, `WriteJSON`, and `MarshalJSON`, without asserting a particular stack-overflow threshold.
- Child order, sorted field order, list order, scalar and nil field callbacks, empty child/field/list cases, and `__proto__` field names in TypeScript.
- Shared DAGs with multiple parents are visited per edge; cycles through children, fields, and nested `[]any` return deterministic errors.
- Cursor point and range boundaries for non-empty and zero-width nodes, empty query ranges, and both code-point and byte positions using multibyte text.
- Byte-for-byte JSON comparisons against existing acyclic output, including escaping, omitted optional members, numbers, nulls, nil versus empty `[]any`, field values, and nested nodes; standard-library-compatible errors for unsupported scalar values and cycles.
- Core versus generated Go parity, TypeScript `marshal` versus established engine JSON (including BigInt, null children, and escaping), and `toJSON`'s existing BigInt-to-Number behavior.
- Nil receiver behavior for `WriteJSON` and `MarshalJSON`, plus `errors.As`, underlying custom-error identity, and actual `json.MarshalerError` nesting when core nodes and generated nodes are marshaled through `encoding/json`.
- Mutation ownership: a walk reports the values present during the call and performs no writes; concurrent mutation is documented as unsupported rather than tested as safe.

The experimental Go JSON stage at `a1365b0` passed focused tests on Go 1.27.1: core and generated-runtime JSON cases, generated parser parity, nil values, nil versus empty `[]any`, shared and cyclic graphs, nested `[]any`, `[]byte`, scalar/custom errors, and 12,000-level direct writer calls. The same deep node passed `Node.WriteJSON` and direct `Node.MarshalJSON`, while `json.Marshal` returned the standard scanner error wrapped in `json.MarshalerError`. This does not validate the full engine suite or command output at that depth. No performance measurements were run.

## Performance and Results

The proposal adds per-call traversal frames and cycle bookkeeping. No timings, allocations, or memory-retention measurements are available. Before adding cursor indexes, measure representative parser trees separately from callback and serialization costs. A range query currently proposes filtering without parent-span pruning, so its initial cost is proportional to visited edges.

## Limitations and Open Questions

- Cycle detection is mandatory for supported graph edges. Consider a trusted fast path only if measurements establish a cost that matters to a concrete consumer.
- What non-Node Go container types, if any, should be traversed/encoded iteratively beyond nested `[]any` and `[]byte`? Keep the initial guarantee narrow and expand after a concrete need.
- Exact error wrapper nesting and cycle text remain unpinned; focused tests establish `errors.As` and underlying custom-error identity for the experimental paths.
- `Node.String` remains recursive in the JSON prototype; an explicit-stack formatting stage is still required. `encoding/json` validates `MarshalJSON` output with a 10,000-level nesting limit, so passing deep Nodes through `json.Marshal` or the CLI's `json.Encoder` is not depth-safe; use direct `Node.WriteJSON` for this stage's deep-output guarantee.
