# G120: LSP Position Checkpoints

An editor repeatedly converting positions on a long line should not scan that
line from the start each time. Checkpoints bound UTF-16/code-point conversion
work.

## Mechanism and Invariants

The language-server text index records cumulative UTF-16 and code-point counts
every 64 bytes. Conversions start at a suitable checkpoint and scan the
remaining local segment. UTF-8 boundaries, supplementary characters and
line/column behavior remain in the text-index contract.

## Scope and State

Default language-server position conversion only. Closure, VMs, generated Go and
TypeScript parser matching are not applicable; this is editor index work.

## Reference Control

No maintained same-source no-checkpoint comparator exists.

## Evidence and Implementation

Record 065 reports a one-megabyte single-line grammar analyzed/tokenized in
about 0.35 s versus over 10 s, while ordinary grammar analysis remains around 11
ms. The adversarial long-line result does not quantify parsing-runtime speed.

Evidence records: [065](../065-position-conversion-in-the-language-server.md).

Source owners: [`text index checkpoints and
conversions`](../../../internal/lsp/text.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M090](M090-diagnostic-line-index.md).

[Optimization Catalog](../README.md)
