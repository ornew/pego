# 025. Native-int Portability for Go

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

This design adopts Go's existing implementation-`int` contract on both
32-bit and 64-bit targets. Architecture-dependent limits are checked while
decoding or validating compiled grammars, and platform-specific sentinels use
the native maximum integer. Matching code does not select behavior by
architecture. It does not impose a 32-bit bound on 64-bit builds.

## Motivation

The Go source fails to compile for `GOARCH=386` because several untyped
constants are compared with `int` or stored in `int` fields: the TypeScript
generator's safe-integer comparison, the compiled-module decoder's signed
32-bit check, and the `Document` repetition-resumption sentinel. These failures
were reproduced on the parent before wide repetition bounds, so they are
pre-existing portability gaps rather than a regression from that change.

Wide repetition bounds use the grammar's implementation-`int` range. The
compiled format can carry wider values than a 32-bit process can represent;
converting those values directly to `int` would truncate them. The 64-bit
contract must therefore remain intact while narrower targets reject values
they cannot represent.

## Goals

- Allow the repository to compile and run on supported 32-bit Go targets.
- Preserve the full implementation-`int` bound and constant range on 64-bit
  targets; do not apply a global `MaxInt32` cap.
- Reject out-of-range signed and unsigned compiled values before conversion to
  `int`, with an error rather than truncation.
- Replace architecture-sized sentinels and comparisons without adding a
  matching-time architecture branch or changing ordinary 64-bit matching.
- Validate the intended target support with cross-build and runtime evidence,
  and separately measure any effect on 64-bit `Document` reuse.

## Non-goals

- Replacing public `int` positions, bounds, or indexes with `int64`.
- Running inputs or allocating memory beyond a target's address-space and
  implementation-integer limits.
- Changing the compiled grammar format or widening every bytecode instruction.
- Claiming 32-bit support from a cross-build alone, or claiming a 64-bit
  performance benefit from sub-parity noisy results.

## Design

### Compatibility and backend support

On 64-bit targets, grammar bounds and action integer constants keep their
current native-`int` range. On 32-bit targets, the accepted native range is
`MinInt32` through `MaxInt32`. The grammar source and public Go API remain
unchanged.

Compiled modules are checked before narrowing: signed integers, unsigned
counts/indexes, wide repetition bounds and integer expression constants must
fit the target's `int`. The decoder reports an out-of-range value instead of
silently converting it. A compiled artifact created on a 64-bit host can
therefore be rejected by a 32-bit host when it contains a value that host
cannot represent. Artifacts whose values fit both targets keep the existing
format and decoded values. Arithmetic uses the executing Go target's native
width, so overflow can produce different results on the two widths.

Architecture-sensitive comparisons use conversions or limits that are valid
for the target width. A `Document` no-edit sentinel is initialized from the
native maximum integer rather than a fixed 64-bit constant. TypeScript
generation compares Go `int` values after widening them to `int64`; this keeps
the existing JavaScript safe-integer decision valid on both Go widths.

These checks occur during generation, decoding, or validation. The generated
parser and matching loops have no architecture selector. Closure, recursive
VM, iterative VM and generated Go use their existing native-`int` behavior;
TypeScript keeps its signed 64-bit arithmetic on either Go host. A 32-bit
generation host accepts only source constants within its native range, but
the emitted parser can compute and retain larger signed 64-bit values.

### Implementation and invariants

The narrow fix addresses the architecture-dependent boundaries without
changing the in-memory representation:

- Compare values against `int32` limits without expressing an unrepresentable
  positive limit as a 32-bit `int`.
- Check decoded signed and unsigned varints against the target's native range
  before converting them.
- Validate decoded integer expression constants and wide repetition operands
  before a VM can use them as `int` values.
- Build the `Document` no-edit sentinel from the target's native maximum.
- Widen Go `int` to `int64` for TypeScript safe-integer comparisons.

The decoder's errors should identify the offending value and preserve ordinary
malformed-file rejection. A narrow target must never reinterpret a large
positive value as negative or a large repetition maximum as unbounded.

## Alternatives considered

- **Document and enforce 64-bit-only support.** This avoids portability code
  but leaves a source tree that does not compile for a common Go target. The
  native-`int` support preserves the existing Go contract on both widths.
- **Use `int64` throughout positions and repetition state.** This provides a
  uniform wide representation but expands public/internal APIs and touches
  bytecode, input indexes, `Document`, and runtime state. It is not required to
  preserve the existing native-`int` contract.
- **Cap all bounds at `MaxInt32`.** This makes all targets agree but narrows
  the documented 64-bit grammar range and is not acceptable without an explicit
  language-contract change.
- **Add architecture checks in matching loops.** A runtime selector would
  affect hot paths even though a process's integer width is fixed. Compile-time
  constants and boundary validation avoid that recurring cost.

## Testing

Targeted signed and unsigned decoder boundaries, AST-omitted integer
constants, externally constructed wide repetition bounds and TypeScript
literal generation pass on native 64-bit Go and on linux/386 under Docker
QEMU. Representable constants and bounds remain valid; out-of-range values
are rejected. Native 64-bit root and all ten parser-module vet/test suites
pass. Final Go and TypeScript generation comparisons pass on both widths,
including strict TypeScript 5.9.3 checks: 17,828 TypeScript results from 194
grammars on Go64, and 17,060 results from 172 grammars on Go32. Wide source
literal fixtures are retained on Go64; Go32 separately verifies rejection of
unrepresentable constants and bounds. An independent generated TypeScript
test computes signed 64-bit boundaries from 32-bit source constants on
either host.

Generation-test JSON oracles encode full deep trees directly, retaining the
100,000-rule-call fixtures. Ordinary trees also cross-check public
`MarshalJSON`. The secondary standard encoder check is omitted above 1,000
tree levels because repeated subtree encoding is quadratic; production JSON
serialization is unchanged.

The Go32 engine suite passes, as do the other core package suites after
correcting sample-analysis length saturation. Two LSP wall-clock checks
exceeded their limits during the concurrent emulated run and pass when run
alone. Eight standalone parser-module vet/test suites pass on Go32: CEL,
CSV, CUE, DuckDB, JSON, TypeScript, XML and YAML.

The Python grammar's indentation state packs values using a 2⁴⁴ multiplier
and requires 64-bit Go arithmetic. The Go grammar also assumes the 64-bit
standard parser's acceptance of extremely large `//line` numbers; three
reference comparisons differ from the 32-bit standard parser. Ready-made
parser support and the architecture-aware CI scope remain unresolved, so
these results do not establish complete 32-bit support for every module.
Validation also covers:

- Cross-compile root and every parser module for `linux/386`; run the supported
  subset in a 32-bit environment and report skips explicitly.
- Test signed and unsigned decoder boundaries at `MinInt32`, `MaxInt32`, one
  below and one above, plus `MinInt64`/`MaxInt64` encodings where relevant.
- Load modules containing wide repetition bounds and integer constants on
  both architectures: retain full-width values on 64-bit and reject
  unrepresentable values on 32-bit without truncation.
- Exercise `Document` first parse, unchanged reparse, edits, and resumption in
  both position units and all three engine backends.
- Verify TypeScript generation at Go `int` boundaries and its existing
  JavaScript safe-integer boundary.
- Re-run native 64-bit engine, parser-module and site suites, plus generated
  parser regeneration, before declaring target support.

## Performance and results

Five alternating baseline/candidate pairs at 300 ms per case compared the
implementation with revision `8521c30` on 2026-10-11, using Go 1.27.1,
darwin/arm64 and an Apple M3 Max (16 CPUs). The Linux/386 test container was
paused during measurement to avoid CPU interference. Ratios below are the
median of each pair's candidate/baseline time ratio; the range contains all
five pairs.

| Workload | Time ratio | Paired range | Allocations, both versions |
| --- | ---: | ---: | ---: |
| Document reparse, closure, CodePoints | 0.972 | 0.956–0.977 | 11 |
| Document reparse, closure, Bytes | 0.971 | 0.925–0.982 | 11 |
| Document reparse, recursive VM, CodePoints | 1.020 | 0.987–1.051 | 44 |
| Document reparse, recursive VM, Bytes | 1.017 | 0.973–1.049 | 44 |
| Document reparse, iterative VM, CodePoints | 0.987 | 0.979–1.000 | 52 |
| Document reparse, iterative VM, Bytes | 1.000 | 0.961–1.040 | 52 |
| Compiled grammar load | 0.996 | 0.991–1.006 | 2,724 |

These measurements show no established regression in the measured 64-bit
workloads. VM and load ranges include parity; closure reparses measured lower
times. Each reparse reused 99,999
records. Allocated bytes stayed approximately 1.15 MB for closure reparses,
10.08 MB for VM reparses and 212 KB for loading, with only a few bytes of
sample variation. Allocation counts were unchanged. These workloads cover
repetition resumption and compiled-module validation, rather than every
parser operation. Emulated Linux/386 runs provide correctness evidence, not
32-bit performance measurements.

Native 64-bit compiler output eliminates the signed narrowing guard and
folds the unsigned limit into the existing comparison. The matching loops
have no architecture selector.

Reproduce by building engine test binaries from the baseline and candidate,
then alternating their invocation order over five pairs:

```sh
go test -c -o /tmp/pego-portability.test ./internal/engine
/tmp/pego-portability.test -test.run='^$' \
  -test.bench='^BenchmarkDocumentReparse/[^/]+/[^/]+/redundant=false$' \
  -test.benchtime=300ms -test.benchmem
(cd internal/engine && /tmp/pego-portability.test -test.run='^$' \
  -test.bench='^BenchmarkLoad/compiled$' -test.benchtime=300ms -test.benchmem)
```

## Limitations and remaining validation

- The core implementation is validated on both integer widths. Ready-made
  parser support and architecture-aware CI remain to be finalized before
  declaring the portability work complete.
- Native `int` limits still constrain bounds and addressable input size on
  32-bit systems. The proposal preserves, rather than removes, those limits.
- Cross-building alone does not establish that every test or parser module
  runs in a 32-bit environment; runtime coverage is required.
