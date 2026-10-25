# Verification and Integration

Start with the affected package or focused semantic test, then run the required
broader checks for the changed surface. [Development testing](../../../../docs/development.md#testing)
explains the test layers. The root Go module, ready-made parsers and site are separate
modules: root tests cannot stand in for `parsers/test.sh` or site checks. Generator or
runtime changes require regeneration and checks of affected generated consumers;
`AGENTS.md` gives the current required commands. `go generate ./spec/language` applies when
the generated grammar-syntax page changes. Check the diff after generation so output
and its source stay together.

For behavior changes, compare the observable result across applicable backends and
position units, including failures and recovery. Include Document edits and streams
when their shared paths change. For a parser module, inspect its module tests and the
main-module parser goldens; use [parser documentation](../../../../parsers/README.md)
for its own generation and conformance conventions. For site work, follow
[site maintenance](../../../../docs/site.md).

For optimization work, read the [performance skill](../../performance/SKILL.md)
and [measurement method](../../../../docs/performance.md#how-to-measure) before
timing. Check correctness first; use comparable baseline/candidate inputs and
interleaved samples, with `B/op` and `allocs/op` alongside time. Choose workloads
by affected path: generated Go runtime changes need generated batch `Parse`
measurements; measure generated Document only when its path changes, using a
focused workload if needed.
Engine shared runtime changes need applicable incremental and stream workloads
as well as batch workloads. Generated Go currently has no stream API. A full
benchmark report is a broad snapshot; focused comparisons support attribution.
Record backend and position unit, toolchain, configuration and hardware, and keep
unmeasured paths explicit.

Before integration, inspect the actual diff, generated artifacts and relevant docs;
state which local checks ran and what they cover. Verify live branch and remote state
before claiming a commit, merge, or backlog completion. The
[backlog skill](../../backlog/SKILL.md) defines Issue #1 updates; do not treat a
passing local test, an accepted proposal, or a draft change as integrated work.
