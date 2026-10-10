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

These core boundary checks occur during generation, decoding, or validation.
Runtime code keeps native arithmetic without a new architecture dispatch. Closure, recursive
VM, iterative VM and generated Go use their existing native-`int` behavior;
TypeScript keeps its signed 64-bit arithmetic on either Go host. A 32-bit
generation host accepts only source constants within its native range, but
the emitted parser can compute and retain larger signed 64-bit values.

### Ready-made parser behavior

The Python grammar uses separate native-integer columns and bounded block
depth packed with bracket and string state. [Design 026](026-portable-python-block-state.md)
records its representation and native-64 allocation tradeoff.

The Go grammar follows the executing target's `go/scanner` directive-number
conversion. Its ordinary line-number interval remains `1..2^30`; converting
an unsigned value above native `MaxInt` to a negative `int` also passes the
scanner's upper-bound check. The resulting wrapped intervals are
`2^31..2^32-1` on Go32 and `2^63..2^64-1` on Go64. A numeric middle field above
native `MaxUint` is treated as part of the filename; an invalid final field
rejects the directive. Module tests compare acceptance, complete ASTs,
positions, line tables and comments with the standard parser for the
executing target.

Generated Go computes the width predicate `2147483647 + 1` with native
arithmetic rather than folding it for the generator host. A parser generated
on a 64-bit host therefore retains Go32 behavior when run there. This
predicate is evaluated only at directive-number boundaries; matching adds no
architecture selector. Generated TypeScript keeps its signed 64-bit
arithmetic and Go64 intervals.

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

## Validation

The native 64-bit `ci/test.sh -count=1 -p=1` run passes, including vet, the
root module and all ten standalone parser modules. Targeted signed and
unsigned decoder boundaries, AST-omitted integer constants, wide repetition
bounds, `Document` reuse and TypeScript literal generation pass on both Go
widths. Strict TypeScript 5.9.3 checks compare 17,828 results from 194
grammars on Go64 and 17,060 results from 172 grammars on Go32. Regeneration
leaves all 17 tracked generated parsers unchanged.

The Linux/386 standalone parser-module matrix passes all ten modules. The
emulated Linux/386 root run passes every package except one `internal/lsp`
hostile-input check that exceeds its message deadline during the full run;
the same check passes when run alone. Treat that as an unresolved full-run
timing failure, not a passing root suite. The new workflow runs the same
serial test script on Linux `amd64` and `386`; remote CI results are not yet
available.

The Python grammar uses separate native-integer columns and bounded block
depth packed with bracket and string state; [design 026](026-portable-python-block-state.md)
records its implementation and allocation tradeoff. Its tests pass on both
widths, but adoption of the native-64 allocation cost remains pending. Go
directive tests on both widths compare acceptance, ASTs, positions, line
tables and comments with the executing standard parser. Generation-test JSON
oracles preserve the 100,000-rule-call fixtures without quadratic secondary
serialization of deeply nested trees.

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

- The complete Linux/386 root wrapper needs a clean pass; one LSP hostile-input
  deadline failed only in the full emulated run and passed in isolation. The
  standalone module matrix is green, but remote CI results are not yet
  available.
- Python's native-64 allocation tradeoff still needs acceptance before its
  change is integrated into `main`.
- Native `int` limits still constrain bounds and addressable input size on
  32-bit systems. The proposal preserves, rather than removes, those limits.
