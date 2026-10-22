# 106. Dropping What Only Non-Start Rule Entries Reach

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

A generated typed parser registered, for every rule of the grammar, the method that
calls the rule (`trules[N].Entry = u<N>` or `s<N>`). The runtime calls an entry only
to run a start rule; every other rule is called through the generated methods that
name it. The registrations still referenced the methods, so every rule's value-free
and standalone methods stayed in the source, including those no call site names
because the grammar uses the rule only through its value form or only inside other
rules' inlined bodies.

The generator now drops the `Entry` registrations of rules that no function outside
`init` names by index (the start rules, which `ParseAST` and `ParseRuleAST` name),
and then drops the unexported functions and package variables that nothing
reachable names. A function counts as naming another when it contains the
identifier, which keeps a little more than needed and never less. If anything reads
the table other than by a constant index, nothing is dropped.

## Results

Bytes of `parser.go` with `-typed-only`:

| Parser | Before | After |
|:--|--:|--:|
| duckdb | 5.12 MB | 2.98 MB |
| typescript | 2.56 MB | 1.94 MB |
| python | 1.57 MB | 1.07 MB |
| golang | 1.43 MB | 1.02 MB |
| cue | 1.07 MB | 0.74 MB |
| cel | 0.28 MB | 0.20 MB |

Test binaries of the parser modules (`go test -c -trimpath`), which the linker
already pruned of unreferenced functions only where nothing registered them:

| Parser | Before | After |
|:--|--:|--:|
| duckdb | 22.3 MB | 18.1 MB |
| typescript | 13.4 MB | 11.9 MB |
| python | 14.1 MB | 13.4 MB |
| golang | 13.7 MB | 12.7 MB |
| cue | 12.6 MB | 12.1 MB |
| cel | 10.5 MB | 10.3 MB |

The removed functions are not called by any parse. Language-module benchmarks (six
alternating rounds) change by +0.5% to +2.3% in the geometric mean, the unchanged
yaml parser by +2.2%, so they are within noise; the `./bench` generated parsers
show +0.35%. Cold-compile times of one run each were within the noise of a shared
machine and are not reported.

## Correctness and Tradeoffs

A unit test prunes synthetic source (start rule kept, unreachable function and
variable dropped, a read of the whole table keeps everything, pruning is
idempotent). The engine tests, the root suite and `parsers/test.sh` pass with
pruned output.

## Decision and Limits

Adopted for Go parsers with typed values. The runtime uses an entry only to
enter a start rule, so only start rules keep theirs; every other rule is called
through its body, which `p.Call` reaches.
