# A120: Source Token Text

Matched text can often reference the original input range instead of re-encoding
decoded characters into a new string.

## Mechanism and Invariants

Valid whole-input Go text uses source substrings, with CodePoints-to-byte
offsets where needed. Malformed UTF-8 in CodePoints retains
replacement-character copying. Ordinary stream ranges use detached copying
unless the separate bounded snapshot mechanism applies. A substring keeps its
source owner alive; zero-copy does not mean zero retained input. TypeScript's
text route respects string/byte input normalization but has JavaScript ownership
costs.

## Scope and State

Default eligible token text in closure, both VMs, generated Go Node/typed Go and
TypeScript. Exact copying depends on input representation and unit.

## Reference Control

No universal substring/copy reference control exists.
`pego_reference_stream_text` controls bounded stream snapshots only.

## Evidence and Implementation

Record 010 combines token text and context reuse, reporting lower allocation
counts. Its result is historical and not a token-only ablation. It explicitly
records whole-input retention as a tradeoff.

Evidence records:
[010](../010-zero-copy-token-text-reusable-evaluation-context.md).

Source owners: [`input.text and buildOffs`](../../../internal/engine/input.go),
[`generated text extraction`](../../../internal/engine/genrt/runtime.go),
[`TypeScript text extraction`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R080](R080-lazy-code-point-offsets.md), [A210](A210-bounded-stream-text-snapshots.md).

[Optimization Catalog](../README.md)
