# 114. No Comments on Unexported Generated Code

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Generated Go source can explain itself: every rule method can carry a comment naming the
rule and the route that made it, every typed constructor and helper a doc comment, and
the file section banners. Nobody reads generated code, and these comments are about 4%
of its bytes.

After formatting, the generator removes every comment except those that matter outside
the file: the `Code generated` header, the package comment, compiler directives
(`//go:`, `// +build`), lint directives (`//nolint`), the comments of exported declarations (the API documentation, including
the comments inside their bodies), and one marker, `// rule <name>`, in front of the
call method of each rule so that a reader can find the code of a rule. Only Go output
is affected; the exported API and its documentation are unchanged.

The tests that read the route comments of the generated source as markers generate
with an unexported option that keeps them (`gentest.GenerateCode`, `gentest.GeneratePackageIn`);
`TestGenerateTrimsComments` pins that `Generate` trims by default, and
`TestTrimComments` pins what is kept.

## Results

`parser.go` with `-typed-only`, bytes and lines before and after:

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| duckdb | 2,190,528 | 2,100,950 | 112,902 | 111,520 |
| typescript | 1,340,814 | 1,286,152 | 67,245 | 66,535 |
| python | 798,059 | 767,020 | 38,136 | 37,675 |
| golang | 698,572 | 671,638 | 35,990 | 35,579 |
| cue | 486,676 | 467,239 | 23,672 | 23,367 |
| cel | 145,241 | 139,200 | 7,289 | 7,195 |

The duckdb test binary has the same size (19,234,994 bytes) and the same text and data
sizes; only the line tables differ. Its cold compile time (best of three, clean build
cache) is 7.8 s before and 7.7 s after. Machine code is unchanged, so parse speed is
not affected.

## Decision and Limits

Adopted. The marker is a `// rule` line, kept by the trimmer when a rule's call method
is emitted; rules that have no call method (plain-only rules) carry no marker.
