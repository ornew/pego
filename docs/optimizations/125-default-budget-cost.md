# 125. The Cost of the Default Budget

- **Status**: Measured; a cost accepted for safety
- **Date**: 2026-10-30

## Problem and Mechanism

A parse with a zero `Budget` used to run without a control. It now gets the default budget
`max(2^20, 1024 * n)` for an input of `n` units, so a parse with no limit and no context runs the
controlled form: the engine uses its metered closure graph or charging VM loop, and generated Go and
TypeScript parsers spend into a counter. Setting `Unlimited` with no context keeps the uncontrolled
form, which is the fast path measured before this change.

The default is resolved once per entry point. Resolving it costs one input length: a byte count for
`Bytes` or an input of at most 1024 units, and one pass over the string to count code points
otherwise. A stream, which has no length, starts at the floor and raises it to 1024 times the furthest position the
parser has demanded; the raise is a comparison on each demand.

## Results

Parse time with default options, before and after, medians of seven interleaved rounds. The engine
columns parse a 256 KiB JSON and XML, a YAML stream of similar size and 5000 CSV rows; the generated
column is a Go parser generated with budget controls from the same grammar:

| Input | Closure | Bytecode | Generated Go |
|:--|--:|--:|--:|
| JSON | 12.48 to 16.05 ms (1.29) | 15.56 to 19.29 ms (1.24) | 5.28 to 6.00 ms (1.14) |
| XML | 12.95 to 16.74 ms (1.29) | 15.69 to 19.39 ms (1.24) | 6.33 to 7.53 ms (1.19) |
| YAML | 39.99 to 50.39 ms (1.26) | 43.47 to 54.03 ms (1.24) | 16.16 to 20.24 ms (1.25) |
| CSV | 5.19 to 7.11 ms (1.37) | 6.84 to 8.60 ms (1.26) | 2.55 to 2.86 ms (1.12) |

The ratio is the cost of the controlled form that a parse with a limit already paid. A caller who
trusts the input and wants the earlier speed passes `Unlimited`.

## Headroom

The smallest budget that parses the JSON and XML inputs is about 8 units per input unit and the YAML
input about 24 (see [record 124](124-coarse-engine-budget-charges.md)), against a default of 1024
per unit.
