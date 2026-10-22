# 107. Shared Shapers and a Compact Rule Table

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Two parts of a generated parser were written out once per rule or per site although
they carry little information.

**Shapers.** Every site that makes a value from the matches of an expression
(`ShapeSingle`, `ShapeCapture`, `ScopeValue`) and every rule of the table carried the
literal `Shaper{Elems: .., K: .., Mode: .., Tuple: .., Shared: ..}`. A large grammar
has about two thousand such literals but only a few dozen distinct shapers (22 in
the duckdb grammar). The generator now declares each distinct shaper once as a
package variable and the sites name it.

**Rule table.** `init` built the rule table with one composite literal per rule
(`{Id: .., Name: .., Scope: []string{..}, Value: Shaper{..}, Memo: .., ...}`) and then
the typed table with one more literal per rule. In the duckdb parser the code that
did this was 485 KB of machine code. The generator now emits the table as static
data in a compact form: a `RuleSpec` per rule (name, a pointer to its shaper, a flag
set, the memo index, and indexes into a shared list of capture-name and variable
lists), plus a short list of the rare fields (`Where`, `NodeType`, `OpNodeType`).
`NewRules` builds the `Rule` values of the table from it in one allocation, and
`NewTRules` builds the typed rules that share them. The id of a rule is its position
in the table, which the generator checks.

## Results

Bytes of `parser.go` with `-typed-only`:

| Parser | Before | After |
|:--|--:|--:|
| duckdb | 2.98 MB | 2.72 MB |
| typescript | 1.94 MB | 1.83 MB |
| python | 1.07 MB | 0.99 MB |
| golang | 1.02 MB | 0.94 MB |
| cue | 0.74 MB | 0.68 MB |
| cel | 0.20 MB | 0.19 MB |

Test binaries of the parser modules (`go test -c -trimpath`):

| Parser | Before | After |
|:--|--:|--:|
| duckdb | 18.09 MB | 17.76 MB |
| typescript | 11.91 MB | 11.78 MB |
| python | 13.36 MB | 13.23 MB |
| golang | 12.69 MB | 12.59 MB |
| cue | 12.08 MB | 12.02 MB |
| cel | 10.32 MB | 10.30 MB |

The duckdb `init` function shrinks from 485 KB of code to 73 KB; the table itself is
57 KB of data. Making the table the literal of a package variable instead (the full
`Rule` struct as static data) was tried and was larger in the binary, since a `Rule`
is about 300 bytes. Parse speed (six alternating rounds, unchanged yaml parser as the
noise reference) is within noise: language-module geometric means change by -0.1%
to +1.4% while the unchanged yaml parser shows -0.8%.

## Correctness and Tradeoffs

The shapers are values, so a site copies the package variable where it used to build
the literal. A failed direct compilation rolls back the shapers it declared, like the
character-class tables. A rule with no captures or variables has a nil `Scope` or
`Vars` instead of an empty slice, which no reader distinguishes. The engine tests
(including the checks that read the generated rule table), the root suite and
`parsers/test.sh` pass.

## Decision and Limits

Adopted for Go parsers. The per-rule `Body`, `Action` and `Entry` assignments of the
typed rules and the lines of Operators Rules are still statements of `init`.
