# M030: Literal Matching

Comparing a fully loaded literal in one loop avoids repeated input-loading
checks and examined-position bookkeeping for each character.

## Mechanism and Invariants

`matchLiteral` compares against loaded input, then updates position and examined
range. A mismatch still leaves position after the matched prefix and examines
the differing character. Lazy stream input keeps its reader-aware fallback. VM
literal arrays are prepared once; generated literal matchers use their runtime's
input representation. Short in-place Go comparisons and unboxed action text
comparison have independent eligibility. Case-insensitive literals have no in-place or
whole-input path: generated runtimes match them rune by rune through `Peek`
(`MatchLiteralFold`), and the VMs use the folded string instruction `STRF`. See record
119.

## Scope and State

Default loaded-input matching in closure, both VMs, generated Go Node/typed Go
and TypeScript, with representation-specific implementations. Stream fallback
remains necessary.

## Reference Control

No universal same-source per-character-bookkeeping reference control exists.

## Evidence and Implementation

Historical record 032 measures 1.5–7% faster sampled closure/recursive VM
workloads except unchanged CSV VM timing. It does not establish current
TypeScript, stream or all-literal benefits.

Evidence records:
[032](../032-matching-literals-against-loaded-input-in-one-loop.md).

Source owners: [`parser.matchLiteral`](../../../internal/engine/input.go),
[`generated literal matcher`](../../../runtime/runtime.go),
[`TypeScript literal matcher`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M070](M070-vm-predecoded-match-data.md), [M120](M120-unboxed-text-comparisons.md), [M130](M130-short-literal-comparisons.md).

[Optimization Catalog](../README.md)
