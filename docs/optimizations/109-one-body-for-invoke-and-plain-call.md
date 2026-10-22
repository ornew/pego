# 109. One Body for the Invocation and the Plain Call

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

A memoized rule of the typed runtime without captures was generated twice: as the
plain method (`v<id>`, called as `invokePlain` does), and as the invocation
(`i<id>`, what the memo and Documents run). The two bodies of a direct rule differ
only in the rollback of the position and the recovered errors to the start when the
body fails; the plain method does it, the invocation leaves it to its caller.

In generations without Documents, the invocation of such a rule is now the plain
method. The callers of an invocation (the call method after a memo miss, and the
memo runtime) repeat the rollback anyway, so doing it once more on failure is
harmless, and the second copy of the body is no longer generated. A rule with captures
never had a plain method: it has the call (`u<id>`) and the invocation (`i<id>`),
which owns a capture frame, so it is unchanged.

Generations with Documents keep both methods. A Document reads the position that the
invocation leaves at its failure to find the frontier it examined (the trace exit's
`Examined`, the evaluation count and the reuse statistics); after the rollback of a
merged body that position is the start, and the frontier would shrink and disagree
with the Node route. `TestGeneratedDocumentFrontierAfterFailure` pins this on a rule
that fails after its callee matched.

## Results

Bytes of `parser.go` with `-typed-only` (the checked-in parsers have no Documents):

| Parser | Before | After |
|:--|--:|--:|
| duckdb | 2.53 MB | 2.34 MB |
| typescript | 1.70 MB | 1.43 MB |
| python | 0.92 MB | 0.85 MB |
| golang | 0.88 MB | 0.78 MB |
| cue | 0.64 MB | 0.52 MB |
| cel | 0.18 MB | 0.16 MB |

The duckdb test binary, built from each commit with one toolchain, shrinks from
18.26 MB to 17.66 MB (-3.3%). Parse speed (30 interleaved rounds, minimum, with an
identical copy as the noise control) is within noise: python +1.7% (control +1.4%),
cue +0.1% (control +0.2%), duckdb +0.5% (control -0.1%), cel -0.4% (control -1.4%),
at most 1.0 point from the control.

## Decision and Limits

Adopted for generations without Documents. The extra work is a position store and a
length store on the failure path of a memo miss, which the call method did already.

The rollback in the plain method is redundant for the consumers in the runtime that
read a failed call without Documents: every one of them (choices, repetitions,
optionals, predicates, the memo and the top level, which reports the farthest failure)
restores the position itself or does not read it. No test observes it; removing it is
a further size step that needs its own measurement and review, and is not part of
this change.
