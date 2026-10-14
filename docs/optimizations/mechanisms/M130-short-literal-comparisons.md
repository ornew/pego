# M130: Short Literal Comparisons

Generated direct Go rules can match common punctuation without calling the
general literal helper on the successful path.

## Mechanism and Invariants

Literals of at most four code points compare against decoded input in place.
Failure or insufficient input goes through `matchLiteral`, preserving
matched-prefix position and expectation behavior. Bytes always uses the general
matcher. Unicode literals and one-to-five-point boundary tests prevent
accidentally treating byte length as character count.

## Scope and State

Default eligible generated Go Node and typed direct bodies in CodePoints. Longer
literals, fallback bodies, Bytes and other backends retain their literal route.

## Reference Control

No short-literal-only same-source control exists; broader direct-body controls
also change unrelated expression dispatch.

## Evidence and Implementation

Historical record 062 measures typed JSON 3.34 to 3.16 ms, XML 3.89 to 3.80 ms
and Outline 2.14 to 1.97 ms. These do not establish current Node, Bytes or
all-literal effects.

Evidence records:
[062](../062-comparing-short-literals-in-place-in-direct-rules.md).

Source owners: [`dgen literal comparison
emission`](../../../internal/engine/gen_direct.go),
[`parser.matchLiteral`](../../../internal/engine/genrt/runtime.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M080](M080-direct-code-point-access.md), [M030](M030-literal-matching.md).

[Optimization Catalog](../README.md)
