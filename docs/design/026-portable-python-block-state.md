# 026. Portable Python Block State

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

The Python grammar needs two indentation columns and a nesting depth while
remaining within Go's native `int` range on both 32-bit and 64-bit targets. This
proposal keeps the visual and alternate columns in `indCol` and `indAlt`, and
stores block depth in the high part of the existing `lv` parser-state value.
The lower part of `lv` continues to encode bracket and f-/t-string state. The
grammar syntax and public APIs do not change.

## Motivation

The previous single `ind` value encoded indentation column, alternate column
and block depth as `col + 2^22*altcol + 2^44*depth`. The depth term exceeds
`MaxInt32`, so arithmetic overflows on 32-bit Go and breaks Python indentation
goldens even though the generated parser compiles.

Using three separate indentation variables is simpler, but the Python module's
`ParseModule` benchmark showed a native-64 cost. Five alternating 1-second
pairs against `0f828a5` on Go 1.27.1, darwin/arm64, Apple M3 Max measured the
three-variable candidate at about 3–4% more time and 8–11% more allocated
bytes on `_pydecimal.py` and `typing.py`. This motivates storing bounded block
depth in `lv`, which already participates in parser state, rather than adding
a third indentation variable.

## Goals

- Preserve the Python grammar's tab-expanded column and alternate-column
  checks, including detection of inconsistent tabs and spaces.
- Parse the same accepted inputs and produce the same ASTs and errors on
  32-bit and 64-bit Go.
- Keep all state arithmetic within the native `int` range on 32-bit targets.
- Avoid a per-parse architecture check or a new runtime/API option.
- Measure native-64 time and allocation changes before adopting the layout.

## Non-goals

- Changing Python syntax, indentation semantics, AST types or the public parser
  API.
- Removing the existing nesting limits or increasing the supported format
  string nesting depth.
- Making every ready-made parser portable as part of this state-layout design;
  the Go grammar's directive-number policy is a separate compatibility item.
- Claiming that native-64 allocation changes imply retained-heap changes.

## Design

### State representation

`indCol` stores the visual indentation column, with tabs advancing to the next
multiple of eight and form feeds resetting the column. `indAlt` stores the
alternate column with tabs counted as one; comparing both values preserves
CPython's `TabError` behavior.

`lv` retains its existing low-order grammar state: bracket depth in the
`lv % 512` region, quote and raw-string flags below 8192, and format-spec
nesting above that. Block depth is stored in multiples of 32768. The lower
state remains below 32768; at most 99 enclosing blocks therefore keep `lv`
below `100 * 32768` (3,276,800), safely within `MaxInt32`.

Entering an indented block checks the existing depth limit, updates
`indCol`/`indAlt`, and adds 32768 to `lv`. PEG rule scopes restore all three
values when the block returns, so sibling blocks resume with the enclosing
indentation and string/bracket state. The f-string and t-string state updates
that replace low-order state must preserve the depth prefix. Existing guards
continue to reject excessive bracket, format-string and indentation depth
before arithmetic can exceed the representation.

The representation uses ordinary native `int` operations and introduces no
architecture-dependent parse branch. The indentation values remain local to
the grammar; no Python AST or public API representation changes and no compiled-format change.

### Compatibility and backend support

The accepted grammar and output contract remain unchanged. Closure, recursive
VM, iterative VM, generated Go, and generated typed Go consume the same
grammar. Generated TypeScript uses the same layout with its signed 64-bit
arithmetic; Python-specific TypeScript output has not been separately tested.
Saved compiled grammars need no format change because this design
changes only the Python grammar's actions and predicates.

The supported 32-bit contract still has native-`int` limits. This design
ensures the grammar's own state fits those limits; it does not make a 32-bit
process able to parse inputs or allocate ASTs beyond its address space.

## Alternatives considered

- **Keep the `2^44` packed depth.** It minimizes grammar variables but
  overflows native `int` on 32-bit Go.
- **Keep indentation column, alternate column and depth as three separate
  variables.** This is straightforward, but the measured prototype increased
  `_pydecimal.py`/`typing.py` `ParseModule` time by roughly 3–4% and allocated
  bytes by 8–11% on native 64-bit. Those measurements motivate this proposal;
  they do not prove that this encoding removes all cost.
- **Use `int64` for indentation state.** This avoids native-width overflow but
  changes the grammar's integer-state contract and adds wider arithmetic to
  the parser. It is unnecessary for a depth bounded to 99.
- **Use a smaller fixed radix for all indentation fields.** This risks
  imposing new limits on indentation columns or nesting. The proposal instead
  gives block depth a bound already enforced by the grammar.
- **Store indentation in an object or composite value.** Grammar variables accept
  only `int`, `string` and `bool`; object-valued state would require a language
  change and new memoization semantics.
- **Maintain architecture-specific grammars or generated files.** Duplicate
  sources would add maintenance and conformance risk; target width can be
  handled without changing the grammar's syntax or generated-runtime API.

## Testing

The focused grammar cases cover 99 nested blocks and rejection at the 100th,
tab/space alternate-column behavior, form-feed resets, sibling blocks,
decorators and `match` cases, and nested f-/t-strings including raw strings and
the existing excessive-format-depth error. Check acceptance through all four parser entry points in CodePoints and Bytes
on native 64-bit and linux/386. The module goldens compare trees, and the
CPython reference corpus compares AST dumps and positions. Regenerate the
module and check generated-parser freshness.

The 16-case indentation suite passes on both widths, including deep blocks and
f-/t-strings. Root's three-backend Python goldens and generated-parser
freshness checks pass on native 64-bit and linux/386. The native-64 Python
module passes with CPython 3.14 and vendored references; the linux/386 module
and vendored references pass, but its environment lacks the CPython executable
so that external comparison was skipped. These results cover the Python
module, not the complete repository-wide 32-bit matrix.

## Performance and results

After moving bounded block depth into `lv`, five alternating 1-second
`BenchmarkParseModule` pairs against baseline `0f828a5` on Go 1.27.1,
darwin/arm64, Apple M3 Max measured candidate/baseline time ratios of 1.0223
for `_pydecimal.py` (paired range 0.9895–1.0520) and 1.0196 for `typing.py`
(0.9885–1.0499). Both ranges include parity, so these runs establish neither
a time regression nor a speedup. Allocated bytes per operation increased by
3.93% and 5.93%, respectively; this is cumulative allocation, not retained
heap. The measured operation is `ParseModule` only and does not establish the
cost for other modules or parser APIs.

A separate five-pair, 300 ms, 10-case snapshot of ParseAST, ParseASTBytes,
ParseModule, Parse and Recognize on `_pydecimal.py` and `typing.py` measured
time-ratio medians from 0.997 to 1.049, with every paired range including
parity. Allocated bytes increased by 0.7–5.9%, while allocation counts fell
by 1.5–4%. This shorter cross-API run is a scope check; the longer ParseModule
pairs above remain the primary operation-specific comparison.

For reproduction, build one test binary from baseline `0f828a5` and one from
the candidate, then alternate their invocation order for five pairs. From
`parsers/python` in each checkout:

```sh
go test -c -o /tmp/python-parsemodule.test .
/tmp/python-parsemodule.test -test.run='^$' \
  -test.bench='^BenchmarkParseModule$' -test.benchtime=1s -test.benchmem
```

The benchmark fixture includes `_pydecimal.py` and `typing.py`. Keep raw
paired logs local; the measurements support a bounded-state tradeoff, not a
general performance claim.

## Limitations and open questions

- The Python module passes its Linux/386 tests, but the full repository-wide
  32-bit matrix and CPython 3.14 comparison on linux/386 remain incomplete.
- The 64-bit measurements show additional allocated bytes, despite no
  established timing regression. Maintainer acceptance of that tradeoff is
  still required before landing the Python portion of native-int portability.
- The indentation column values themselves remain Go `int`; extremely large
  columns are still constrained by native integer and input-size limits.
