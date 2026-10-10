# 025. Native-int Portability for Go

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-10

## Summary

This proposal keeps Go's existing implementation-`int` contract on both
32-bit and 64-bit targets. Architecture-dependent limits are checked while
decoding or validating compiled grammars, and platform-specific sentinels use
the native maximum integer. Matching code does not select behavior by
architecture. The proposal does not impose a 32-bit bound on 64-bit builds.

Whether PEGO will support 32-bit Go targets remains an open maintainer choice;
the evidence below makes the compatibility and performance tradeoff concrete.

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

- Allow the repository to compile for 32-bit Go targets if maintainers select
  that support policy.
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
format and behavior.

Architecture-sensitive comparisons use conversions or limits that are valid
for the target width. A `Document` no-edit sentinel is initialized from the
native maximum integer rather than a fixed 64-bit constant. TypeScript
generation compares Go `int` values after widening them to `int64`; this keeps
the existing JavaScript safe-integer decision valid on both Go widths.

These checks occur during generation, decoding, or validation. The generated
parser and matching loops have no architecture selector. Closure, recursive
VM, iterative VM and generated Go use their existing native-`int` behavior;
TypeScript output is affected only by its generator. The proposal does not
alter PEGO's TypeScript runtime representation.

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
  but leaves a source tree that does not compile for a common Go target. It is
  still a valid policy if the project does not intend to support 32-bit users;
  documentation and CI would need to state that clearly.
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

The candidate compiles the root module and engine test binary for
`GOOS=linux GOARCH=386`. Signed/unsigned decoder boundaries, AST-omitted
integer constants, externally constructed wide repetition bounds and
TypeScript literal generation pass on native darwin/arm64 and on linux/386
under the existing Docker QEMU environment. Both VMs retain representable
constants and bounds and reject values outside the native range.

The affected 64-bit compiled-grammar, Document and wide-repetition tests,
public API/CLI tests, all parser-module suites, vet and site checks pass.
Regenerating all 17 tracked parsers produces no changes. Broader 32-bit
Document and loader testing is ongoing. These are candidate checks, not a
completed target support matrix. The remaining validation should include:

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

A five-pair, 300 ms `BenchmarkDocumentReparse` comparison against revision
`476cff9` on Go 1.27.1, darwin/arm64, Apple M3 Max (16 CPUs)
reported candidate/baseline median ratios of 0.979/1.010 for closure,
1.004/1.010 for the recursive VM, and 1.009/1.012 for the iterative VM
(CodePoints/Bytes). Every paired range crossed parity; allocation counts and
the 99,999-record reuse count were unchanged. This does not establish a
64-bit cost or gain. The measurements isolate repetition resumption and do
not establish performance for all parser operations.

Reproduce by building engine test binaries from the baseline and candidate,
then alternating their invocation order over five pairs:

```sh
go test -c -o /tmp/pego-portability.test ./internal/engine
/tmp/pego-portability.test -test.run='^$' \
  -test.bench='^BenchmarkDocumentReparse/[^/]+/[^/]+/redundant=false$' \
  -test.benchtime=300ms -test.benchmem
```

## Limitations and open questions

- Maintainers have not selected 32-bit Go support. The proposed support matrix
  still needs broader runtime validation and architecture-aware test coverage.
- Native `int` limits still constrain bounds and addressable input size on
  32-bit systems. The proposal preserves, rather than removes, those limits.
- Cross-building does not establish that every test or parser module runs in
  a real 32-bit environment.
