# 031. Iterative Tree Traversal and JSON Output

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

PEGO exposes mutable parse-result `Node` values. The Go API now provides iterative JSON writing, edge-oriented traversal, and cursor-based range selection for core and generated Go nodes. These APIs define edge order, shared-node behavior, cycles, range units, empty spans, and ownership without changing the JSON representation. TypeScript traversal remains deferred; its existing iterative `marshal` and `toString` APIs are unchanged.

## Motivation

`internal/engine/node.go` and the generated Go runtime in `runtime/runtime.go` expose nodes with `Children` and `Fields`; fields may refer to nodes and are mutable through the Go representation. `Node.Clone` preserves shared subtrees. Go `Node.MarshalJSON` and `Fields.MarshalJSON` now use an iterative writer for nested node graphs; `Node.WriteJSON` also streams that representation to an `io.Writer`. `Node.String` still descends through `writeNode`/`writeValue` recursively.

The TypeScript runtime in `internal/engine/tsrt/runtime.ts` already formats `toString()` and exports `marshal(Node | null)` iteratively with a `Pending` stack. `marshal` preserves Go-compatible JSON field order, BigInt number formatting, null children, and escaping. Separately, `toJSON()` constructs nested objects and converts BigInt to `Number`; native `JSON.stringify` controls recursion on that object representation. The core and generated Go runtimes now provide a common node walker and cursor; the parser-specific `parsers/duckdb/walk.go` helper remains separate.

The mutable, exported Go node graph can be hand-built with shared children or cycles, so traversal and serialization define behavior for those cases rather than assuming parser-produced trees. Design record 019 identified traversal order, identity, cycles, scalar values, range units, and empty-node boundaries as contracts to settle.

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

The public Go API provides a stateless function with an explicit visit record and action:

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

The public Go `Cursor` layers point and range selection on the walker. Each query requires a position unit: `CodePoints` or `Bytes`, matching parser options and the unit used to populate `Start`/`End`. It does not infer a unit from the node. The range query is half-open `[start,end)`. A non-empty node span `[Start,End)` matches a point cursor at `p` when `Start <= p < End`, and matches a non-empty query range when the spans overlap. A zero-width node at `p` matches a point cursor at `p`; in a non-empty range it matches when `start <= p < end`. An empty query range matches no spans; use the point-cursor operation for a caret position.

Range matching filters visits but does not prune solely from a parent's span: callers can construct Nodes whose parent and child spans are inconsistent. A later optimized cursor may prune only under a documented validated-tree invariant. Negative or reversed query ranges return an argument error. Positions outside a known source length are not rejected because a Node alone does not carry that length.

Range helpers report node spans only. Scalar fields have no independent source span and are visible through their owning node's field-edge callback. Lists likewise have no span of their own; their node elements are matched by their own ranges. Empty lists and nil list entries produce no node span.

#### Go JSON Writer

The Go `Node.WriteJSON(io.Writer) error` method and `MarshalJSON` use the same iterative encoder. `(*Node)(nil).WriteJSON(w)` writes the JSON value `null` to `w`; `MarshalJSON` on a nil receiver returns `null` without error, matching the behavior of `json.Marshal((*Node)(nil))`. The writer emits the current object shape and field order: `type`, optional `rule`, `start`, `end`, optional `text`, optional `children`, optional `fields`; fields remain lexicographically sorted. Children remain array-ordered. JSON string escaping and scalar encoding continue to use `encoding/json` behavior, including errors for unsupported values and invalid numbers. The output bytes for acyclic values remain byte-for-byte compatible with the prior `json.Marshal` output.

The implementation keeps an explicit stack of node, field, array, and `[]any` frames. It delegates scalar encoding and string/key escaping to `encoding/json`, while recognizing Node and nested `[]any` edges itself. `[]byte` is encoded through `encoding/json` to preserve its existing base64 string representation. It tracks active Node and `[]any` identities to reject cycles with a `json.UnsupportedValueError`-compatible error; shared values outside the current path are encoded again at each edge. The writer does not close or flush the caller's writer, and may leave a valid JSON prefix written when an error occurs. `MarshalJSON` buffers the same writer output and returns no bytes on error, as required by `json.Marshaler` convention.

This writer handles node graphs, nested `[]any`, `[]byte`, and declared scalar field values. Other container types, maps, and custom marshalers are delegated to `encoding/json` as leaves; nested Nodes inside those unsupported containers are outside the iterative guarantee. The implementation must not silently route a recognized Node or `[]any` back through recursive `json.Marshal`.

`Fields.MarshalJSON` uses the same iterative value encoder for each field while retaining its existing sorted-key object shape, so directly marshaling a `Fields` value also handles supported nested Node and `[]any` edges.

When `encoding/json` marshals a node graph, errors from `Node.MarshalJSON` are wrapped in `*json.MarshalerError`; nested Node boundaries can add wrappers. The iterative writer preserves `errors.As` behavior and underlying custom error identity. Exact wrapper nesting and error strings are not a compatibility guarantee. Byte-for-byte compatibility applies to successful acyclic output.

#### TypeScript Output

The TypeScript runtime's existing exported `marshal(Node | null): string` iteratively emits Go-compatible JSON, including exact BigInt integer formatting, null children, field ordering, and Go string escaping. `toJSON()` retains its object shape and separately converts BigInt to `Number`, so native `JSON.stringify(node)` can be inexact for large integers and remains subject to native serializer recursion. The already-iterative `toString()` remains unchanged in output and quoting rules. General TypeScript traversal and range APIs are not implemented by this stage.

#### Ownership and Mutation

All APIs operate on the live Node graph. They take no ownership and retain no references after returning. Callers must not mutate any reachable node, child slice, or field slice concurrently with a walk or serialization. The APIs do not make a deep copy. This keeps the mutable Node and `Clone` contracts intact and leaves snapshots to the separate versioned-tree design.

### Implementation and Invariants

Use explicit frames containing the current node/value, next child/field/list index, parent edge, and enter/exit state. Cycle tracking uses an active-path set for Node pointers and supported `[]any` containers. State is per call and discarded on return. Fields are visited through a sorted view or index order computed without mutating `Fields`.

The JSON writer uses the same ordered edge model, emitting object delimiters and commas from frame state. Node and field formatting share the iterative value encoder so `MarshalJSON` and `WriteJSON` stay consistent. Nil behavior is tested. A cycle is a newly handled graph condition; ordinary encoding errors preserve their underlying error identity and `errors.As` behavior, while exact wrapping layers are not a compatibility guarantee.

Generated Go parsers embed `runtime/runtime.go`, so the core and generated runtime must implement identical behavior and be covered by generator parity tests. TypeScript is a separate compatibility surface and should not claim safe native `JSON.stringify` at arbitrary depth.

### Compatibility and Backend Support

The Go traversal/cursor APIs use `Walk` in the core and public `pego` package.
Standalone generated Go parsers expose the generic node walker as `WalkTree`,
so parser packages can retain their existing typed-AST `Walk` helper. Generated
`Cursor` calls `WalkTree`; all visit records, actions and edge semantics remain
the same. Generated typed grammar names reserve `WalkTree` and the traversal
types/constants to avoid declaration collisions.

The Go traversal/cursor APIs are available in `internal/engine`, the public `pego` package, and generated Go runtimes. Generated parsers expose the generic walker as `WalkTree` to avoid colliding with parser-specific `Walk` helpers. Core and generated Go JSON writers preserve successful acyclic output bytes. TypeScript keeps the existing `marshal` and `toJSON` contracts; general traversal remains deferred. No grammar syntax, parser execution, parse positions, or mutable ownership changes are required.

### Implementation Sequence

1. Completed: iterative Go `WriteJSON`, `MarshalJSON`, edge-oriented `Walk`, and point/range `Cursor` behavior in core and generated Go runtimes, with parity and cycle/error coverage.
2. Deferred: consider TypeScript traversal and range APIs as a separate compatibility step; preserve existing `marshal` and `toJSON` behavior.

A versioned output schema remains separate from this traversal contract.

## Alternatives Considered

- **Recursive visitor callbacks:** smaller implementation, but traversal depth follows input shape and cycle handling is harder to make uniform. Prefer explicit frames for the deep-tree use case; performance is unmeasured.
- **Unique-node traversal:** avoids repeated work on shared DAGs, but loses edge context and may omit a subtree's distinct parent/field relationship. Defer it until a concrete consumer requires graph identity semantics.
- **Reject all repeated node identities:** simple cycle policy, but incorrectly rejects valid shared subtrees. Detect only active-path back-edges.
- **Change Node to immutable snapshots first:** would simplify concurrent traversal and identity semantics, but changes ownership and public mutability. Defer to the snapshot design; do not make this API imply immutability.
- **Use only recursive `json.Marshal` / native `JSON.stringify`:** preserves familiar entry points, but retains recursive traversal. The selected Go writer is iterative and TypeScript's existing iterative `marshal` remains available; native JavaScript serialization remains outside the depth guarantee.

## Testing

Tests cover:

- Deep generated and hand-built nodes for walker, `WriteJSON`, and `MarshalJSON`, without asserting a particular stack-overflow threshold.
- Child order, sorted field order, list order, scalar and nil field callbacks, empty child/field/list cases, and `__proto__` field names in TypeScript.
- Shared DAGs with multiple parents are visited per edge; cycles through children, fields, and nested `[]any` return deterministic errors.
- Cursor point and range boundaries for non-empty and zero-width nodes, empty query ranges, and both code-point and byte positions using multibyte text.
- Byte-for-byte JSON comparisons against existing acyclic output, including escaping, omitted optional members, numbers, nulls, nil versus empty `[]any`, field values, and nested nodes; standard-library-compatible errors for unsupported scalar values and cycles.
- Core versus generated Go parity, TypeScript `marshal` versus established engine JSON (including BigInt, null children, and escaping), and `toJSON`'s existing BigInt-to-Number behavior.
- Nil receiver behavior for `WriteJSON` and `MarshalJSON`, plus `errors.As`, underlying custom-error identity, and actual `json.MarshalerError` nesting when core nodes and generated nodes are marshaled through `encoding/json`.
- Mutation ownership: a walk reports the values present during the call and performs no writes; concurrent mutation is documented as unsupported rather than tested as safe.

Focused core and generated-runtime tests cover JSON parity, nil values, nil versus empty `[]any`, shared and cyclic graphs, nested `[]any`, `[]byte`, scalar/custom errors, and 12,000-level direct writer calls. The same deep node passes `Node.WriteJSON` and direct `Node.MarshalJSON`, while `json.Marshal` returns the standard scanner error wrapped in `json.MarshalerError`. Walk and Cursor tests cover edge order, shared parents, cycles, traversal controls, deep trees, explicit units, boundaries, and inconsistent parent/child spans. The recorded Go JSON benchmark results follow.

## Performance and Results

The final Go JSON writer was compared with the same benchmark source at main `40ab3257` and the candidate revision. Measurements used Go 1.27.1 on Apple M3 Max (`darwin/arm64`, `GOMAXPROCS=16`), five alternating pairs, and a 200 ms benchtime. Both inputs are synthetic: an 8-child record and a 512-node chain.

| Workload | Main median | Candidate median | B/op, main → candidate | Allocs/op, main → candidate |
| --- | ---: | ---: | ---: | ---: |
| Shallow, 8 children | 6,341 ns/op | 5,320 ns/op (-16.1%) | 4,141 → 4,147 | 51 → 108 |
| Deep, 512-node chain | 16,127,307 ns/op | 199,777 ns/op (about 80.7× faster) | variable 9.9–25.3 MB → about 301,948 | variable 1,567–6,042 → 3,109 |

The deep baseline varied substantially between samples. These results cover only the two synthetic shapes and do not establish performance for representative parser workloads. Raw benchmark logs are kept outside the repository. Reproduce on each revision with the same `internal/engine/node_json_bench_test.go` source using `go test -run '^$' -bench '^BenchmarkNodeJSON$' -benchmem -benchtime=200ms -count=5 ./internal/engine`; alternate the main and candidate runs to match the paired method.

## Limitations and Open Questions

- Cycle detection is mandatory for supported graph edges. Consider a trusted fast path only if measurements establish a cost that matters to a concrete consumer.
- What non-Node Go container types, if any, should be traversed/encoded iteratively beyond nested `[]any` and `[]byte`? Keep the initial guarantee narrow and expand after a concrete need.
- Exact error wrapper nesting and cycle text are not pinned; focused tests establish `errors.As` and underlying custom-error identity for Go writer errors.
- `Node.String` remains recursive; an explicit-stack formatting stage is still required. `encoding/json` validates `MarshalJSON` output with a 10,000-level nesting limit, so passing deep Nodes through `json.Marshal` or the CLI's `json.Encoder` is not depth-safe; use direct `Node.WriteJSON` for deep output.
