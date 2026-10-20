# G110: One-Pass Code-Point Preparation

Decoding, validating UTF-8 and building token-text offsets in separate passes
rereads the input. One preparation pass produces the decoded characters and
required mapping together.

## Mechanism and Invariants

Full Go input preparation emits code points, validity state and byte offsets in
one traversal. Malformed input preserves replacement semantics and the necessary
copy path. Core recognition keeps on-demand offsets when no text consumer needs
them; generated input uses its runtime's eager preparation. This does not change
the parser's selected position unit.

## Scope and State

Default applicable CodePoints preparation before core/generated Go matching;
TypeScript has its own combined decoding/offset initialization. Bytes input
needs no code-point mapping. Shared preparation is separate from matching-path
backend cells.

## Reference Control

No maintained same-source separate-pass reference control exists. Lazy
recognition offsets are an operation-specific policy, not a full-parse
reference.

## Evidence and Implementation

Records 040/041 historically report about 1–5% full-parse improvements on
selected Go workloads; generated CSV changes 4.6 to 4.2 ms in 040. These include
preparation cost and do not establish a pure matching improvement or current
TypeScript ratio.

Evidence records:
[040](../040-one-pass-to-prepare-code-point-input-in-generated-parsers.md),
[041](../041-building-the-offset-table-while-decoding-for-full-parses.md).

Source owners: [`newTextInputIn`](../../../internal/engine/input.go),
[`generated setSource`](../../../runtime/runtime.go), [`input
decode/offset preparation`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R080](R080-lazy-code-point-offsets.md), [A120](A120-source-token-text.md).

[Optimization Catalog](../README.md)
