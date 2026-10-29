# R080: Lazy Code-Point Offsets

Recognition often needs decoded characters but no token text. Deferring
code-point-to-byte offsets avoids an extra table build until text or a position
consumer needs it.

## Mechanism and Invariants

`input.buildOffs` creates the mapping on demand where it was not prepared during
decoding. Full core parses eagerly build it; Documents retain tables built
during parsing so a copy of parser input does not discard the cache. Invalid
UTF-8 retains normalized text behavior. Piece-backed Document input has its own
indexed mapping and need not use the old flat table.

## Scope and State

Default core closure/VM CodePoints recognition preparation, and flat Document
table ownership where applicable. Generated Go full input prepares offsets
eagerly; TypeScript uses its own decode/offset setup. This is not a common
lazy-offset policy for all targets.

## Reference Control

No same-source lazy/eager offset control is maintained. Requesting token text
changes the observed work, not the implementation selection.

## Evidence and Implementation

Record 030 reports lower recognition timing and about 1 MB less allocation on
selected JSON/CSV inputs; text-reading XML recognition is unchanged. It also
records and fixes the Document table-loss regression. Those historical effects
do not describe current piece-table input or every recognition grammar.

Evidence records:
[030](../030-building-the-code-point-offset-table-on-demand.md).

Source owners: [`input.buildOffs and text`](../../../internal/engine/core/input.go),
[`Document input-table retention`](../../../internal/engine/document.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G110](G110-one-pass-code-point-preparation.md), [A120](A120-source-token-text.md).

[Optimization Catalog](../README.md)
