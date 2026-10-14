# 034. Typed Output Liveness and Alias-Safe Cleanup

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Generated typed parsers allocate returned structs, terminals, matches and list backing cells in chunks. A small returned value can keep unused siblings in those chunks alive. This design considers preserving slab allocation while clearing only storage proven unreachable from the final returned value. Clearing must wait until parsing, recovery, conversion and publication have finished; clearing during backtracking or action evaluation is unsafe.

The current prototype has separate full-sweep and bounded-sweep generation controls. Both are private, generation-only and disabled by default. They apply to generated Go typed results on direct typed and conversion-based `ParseAST`/`ParseRuleAST` paths, including returned `Match` values and owned backing cells. Plain Node `Parse`, `Recognize`, the engine/VM backends and generated TypeScript do not use these controls. The full and bounded sweep controls remain default-off.
A separate per-owner allocation bootstrap is adopted in [optimization record 098](../optimizations/098-bootstrap-generated-typed-output-allocations.md). It changes the allocation pattern before slab growth rather than clearing unreachable cells; its measurements do not change the status of these sweep controls.

## Current Candidate Scope

Both variants run at the final typed-result publication boundary, after parsing and any Node-to-typed conversion have completed. The marker starts from the returned value and parse error, follows supported typed values, and identifies reachable objects and backing cells. A pointer to a scalar field can hide the slab object that owns that field. When such an interior pointer is found, the marker promotes the owning object to live and traces its references; it repeats promotion and tracing until the reachable set reaches a fixed point. Only then does it inspect every slot in each tracked backing block, including unused capacity, and zero slots that are not live. A live scalar-field reference therefore keeps its containing object and descendants reachable, while unrelated cells in the same block can still be cleared. The full-sweep variant has no explicit marker-work budget. The bounded variant preflights complete ownership directories and uses fixed work, key, pending-value, owner-record and slot budgets; parent promotion is charged to its work budget. If preflight, tracing or promotion exceeds a limit, it falls back atomically to keep-all before any cell is cleared. Ownership-directory collection during parsing is not capped.

Both variants must preserve pointer identity, aliases, cycles, contents, slice headers and every cell visible through a published slice capacity. Interior pointers into scalar fields require retaining the enclosing owner and tracing its descendants to a fixed point. Unknown aliases, unsupported dynamic values or otherwise unproven ownership must take the keep-all path. Cleanup must not run during backtracking, action evaluation, memo pruning, recovery or left-recursion/Operators Rule growth. The generated parser has no parser-time selector: the variant is selected only while generating code.

Focused correctness gates and full local validation passed for the measured candidate. Current direct and conversion measurements are summarized in [optimization record 094](../optimizations/094-generated-typed-output-liveness.md). Earlier prototype measurements came from other source revisions and are not repeated here. Measured small-input overhead, bounded-path allocation increases, and the absence of conversion-path retained-heap reduction leave both controls default-off.

## Motivation

Chunk allocation reduces allocation overhead but may retain unreachable siblings. Earlier full-sweep and bounded prototype measurements came from different source revisions and are historical observations, not evidence for the current candidate. Current measurements and their limits are collected in [optimization record 094](../optimizations/094-generated-typed-output-liveness.md). The candidate remains default-off because the measured small-input and allocation costs are material and conversion results show no retained-memory benefit.

## Goals

- Preserve identity, contents, slice headers and mutation behavior of every returned value and alias.
- Keep slab allocation and clear only owned cells proven unreachable from the final result.
- Cover direct and conversion results, named-rule results, Match values, objects and list backing cells; conservatively retain values when ownership cannot be proven.
- Measure retained and peak memory, allocation counts and latency on ordinary and discard-heavy workloads before any default change.

## Non-goals

- Changing grammar semantics, public APIs, result types or generated runtime selection.
- Clearing values during rollback, rule return, memo eviction, recovery, left-recursion/Operators Rule growth or failed alternatives.
- Adopting full sweeping or per-object allocation as the default.

## Design

### Proposed ownership boundary

Keep a per-invocation directory of allocated backing blocks. Both markers run only after parsing and conversion produce the final result, and preserve every cell visible through a published slice capacity. The bounded marker preflights complete owner directories and block sizes, then uses fixed budgets of 256 work items, 256 marked keys, 64 pending values, 64 owner records and 4,096 slots. Directory collection during parsing is uncapped. A pointer to a scalar field may hide its containing slab object; the marker promotes that owner and traces its references, repeating until no new owners are found. Promotion work consumes the same work budget. If preflight, graph tracing or promotion exceeds a budget, or ownership is unknown, keep all blocks and clear none; the fallback occurs before the sweep, never after partial clearing. On success the sweep examines every slot in every recorded block and zeros only unmarked cells, including unused capacity. Live cells and all cells visible through published slice capacity remain untouched.

Track public Match blocks as a separate ownership domain: a returned pointer may address a Match field embedded in a chunk element. Derive public pointers from the actual field, not an assumed struct offset. Keep parser scratch (Node, frames, memo, trail and capture storage) under existing recycle rules; it is not public output storage.

Traversal must preserve aliases and cycles. Mark pointer identity before descending; handle slices by backing-cell identity and the full reslice-visible range. When an interior scalar-field pointer is reached, promote its slab owner and trace the owner’s references until the live-owner set stops growing. Do not use arbitrary interface values as map keys. Unknown/open values, unsupported dynamic types and graphs whose ownership cannot be established take a conservative keep-all path. Complete tracing and all owner promotion before clearing any cell.

Run cleanup once at the final publication boundary, after conversion has allocated its result and before temporary ownership metadata is dropped. Direct, conversion and named-rule entries must use the selected result's ownership description. Errors and successfully recovered values follow the same rule as other returned results.

The full-sweep and bounded-sweep controls are private, independent generation-time options and both default off. Bounded-only generation emits bounded hooks; ordinary and full-reference output remain unchanged. No public selector or runtime flag is added.

## Alternatives considered

- **Keep all chunks unchanged.** This preserves current throughput and ownership behavior, but retains discarded siblings.
- **Allocate every value individually.** It releases siblings without graph traversal but may increase allocation and collector work; compare it only as a separate control, not as the proposed default mechanism.
- **Clear at rollback, action return or capture replacement.** Rejected because another field, list, union, Match, recovery result or caller-visible alias may still reference the value.
- **Clear without a complete graph walk.** Rejected because missing a pointer or list cell corrupts a returned result. Conservative keep-all is safer than partial cleanup.
- **Move or compact live values.** Rejected because it changes pointer identity and can invalidate aliases and slice views.

## Testing

Compare cleanup enabled and disabled variants for direct, conversion and named-rule outputs in both position units. Cover shared pointers and slices, overlapping views, cycles and DAGs, typed nils, optional values, empty lists, any, CST/open values, Error and recovered results, and returned Match values sharing blocks with discarded matches. Verify contents, identities, slice capacities and retained values across later parses and concurrent invocations. Unknown values must force conservative retention.

The retained-output fixture can be generated and tested with:

```sh
tmpdir=$(mktemp -d)
PEGO_TYPED_OUTPUT_RETENTION_DIR="$tmpdir" go test ./internal/engine -run '^TestGeneratedTypedOutputRetentionFixture$' -count=1
for variant in slabs objects slices individual sweep bounded; do
  (cd "$tmpdir/$variant" && go test -run '^$' -bench '^BenchmarkTypedOutputRetainedHeap$' -benchmem -benchtime=1x -count=1)
done
```

The same fixture exporter writes conversion variants with `-conversion` appended to each variant name. `convertTypes` is enabled during generation; these variants parse to Nodes and convert the returned value to the typed result before publication. Benchmark them separately from direct typed generation:

```sh
tmpdir=$(mktemp -d)
PEGO_TYPED_OUTPUT_RETENTION_DIR="$tmpdir" go test ./internal/engine -run '^TestGeneratedTypedOutputRetentionFixture$' -count=1
for variant in slabs-conversion objects-conversion slices-conversion individual-conversion sweep-conversion bounded-conversion; do
  (cd "$tmpdir/$variant" && go test -run '^$' -bench '^BenchmarkTypedOutputParse$' -benchmem -benchtime=500ms -count=1)
  (cd "$tmpdir/$variant" && go test -run '^$' -bench '^BenchmarkTypedOutputRetainedHeap$' -benchmem -benchtime=1x -count=1)
done
```

The ordinary exporter writes seven generated parsers and a checked benchmark overlay for `ref`, `sweep` and `bounded`. The overlay adds byte/code-point AST adapters without editing the committed benchmark harness or generated parsers:

```sh
tmpdir=$(mktemp -d)
PEGO_TYPED_OUTPUT_SWEEP_EXPORT_DIR="$tmpdir" go test ./internal/engine -run '^TestExportOrdinaryTypedSweepBenchmarks$' -count=1
for variant in ref sweep bounded; do
  go test -overlay "$tmpdir/$variant/go-overlay.json" ./bench -run '^TestWorkloads$' -count=1
  go test -overlay "$tmpdir/$variant/go-overlay.json" ./bench -run '^$' \
    -bench '^BenchmarkParse$/^(JSON|CSV|XML|Arith_OperatorsRule|Arith_LeftRec|Minilang|Recovery|Outline)$/^generated_ast$/^(codepoints|bytes)$' \
    -benchmem -benchtime=500ms -count=1
done
```

The comparison protocol and measured phases are recorded in [optimization record 094](../optimizations/094-generated-typed-output-liveness.md). Keep the exact-parent and same-source reference comparisons distinct. Report latency, allocation, peak process RSS and post-GC retained heap separately; a retained-heap reduction alone is insufficient evidence for adoption.

## Limitations and Open Questions

Both variants remain proposed and default-off. The current sweep examines every slot in tracked blocks and clears only unmarked cells, including unused capacity; cells reachable through interior pointers keep their promoted owner and descendants live. Bounded work exhaustion falls back to keeping all storage before clearing. Ownership-directory collection remains uncapped. Reconsider the default only if measured costs become acceptable and meaningful retained-memory benefits hold for direct and conversion results.
