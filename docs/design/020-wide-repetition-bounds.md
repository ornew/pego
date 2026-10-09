# 020. Wide Repetition Bounds

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-09

## Summary

The bytecode backends preserve the repetition bounds accepted by the grammar
compiler, including values above 2³¹−1 on 64-bit implementations. Large bounds
use two additional matching instructions that reference full-width integer
constants. Ordinary instructions retain their 16-byte Go representation.

For example, `def main="a"{4294967296}` fails on empty input on every engine,
and `def main=@(?a){0,4294967296} $$` accepts `a`. Saving and loading either
grammar preserves that behavior.

## Motivation

The grammar AST uses `int` for repetition bounds, but the bytecode compiler
narrowed them to `int32`. A large minimum could become zero or negative, and
a finite maximum could become zero or unbounded. Some compiled files then
failed validation when reloaded. These were incorrect executions of accepted
grammars, independent of whether inputs actually needed billions of elements.

The existing lexical contract permits integers in the implementation's range.
Preserving that contract avoids imposing an arbitrary grammar limit to match
one backend's operand representation.

## Design

`REPEATW` stores expression-code indices for the minimum and maximum and keeps
the existing scope operand. `SCANW` keeps the character-class operand and stores
expression-code indices for its bounds. Each index references an `EINT`
instruction, whose two signed words already encode a complete 64-bit integer.
The VM reads the constants directly; no action evaluation, value allocation or
additional match-code instruction is needed.

The wide repetition still immediately precedes `ITER`. Backtracking, finite
maximum checks, streaming and Document repetition resumption use the same
integer state and control flow as ordinary repetition. Both recursive and
iterative VMs share the implementation. Projected repetitions also use wide
bounds. Closure and generated Go retain their existing implementation-int
loops; generated TypeScript is checked on the same boundary grammars.

Loading verifies that both references point to `EINT`, that each value fits
the runtime's implementation `int`, and that the minimum and maximum form a
valid range. A 32-bit runtime rejects a wider compiled bound rather than
silently truncating it.

### Compatibility

The `.pegoc` format remains version 2. Instruction set 4 adds `REPEATW` and
`SCANW`; files using them require that version. Modules without wide bounds
continue to declare instruction set 3, preserving compatibility with earlier
runtimes. Earlier instruction sets still load. A file falsely declaring an
older instruction set while using wide bounds is rejected.

Files compiled before this correction must be rebuilt if they contain large
bounds. Their stored matching code already lost the original values; a load
does not recompile that code from its optional AST.

## Alternatives considered

- **Cap all bounds at 2³¹−1.** Simple validation would prevent truncation, but
  would reject grammars the current specification permits. This changes the
  language contract and was rejected.
- **Widen every instruction operand.** This directly represents large bounds
  but doubles the Go instruction size from 16 to 32 bytes on 64-bit hosts,
  including grammars that never need wide bounds. Avoiding that representation
  cost does not itself establish a measured throughput gain.
- **Append a payload instruction after each wide repetition.** This keeps most
  instructions small but changes repetition layout and requires payload-aware
  jump validation, instruction stepping and resumption. Existing `EINT`
  constants provide exact values without those changes.

## Testing

Tiny-input regressions cover 2³¹, 2³² and the 64-bit maximum, including exact
and unbounded minima, finite maxima, class/any scans, and projected values.
Differential checks cover both position units, memo modes, recognition,
original programs, AST/bare compiled files, generated Go Node/typed parsers
and TypeScript. Streams and edited Documents exercise the wide repetition
state and its resumption metadata. Loader tests reject missing or noninteger
bound constants, negative minima, invalid maxima, reversed bounds and false
instruction-set declarations. Disassembly displays complete bound values.

## Performance and limitations

Common instruction sizes and ordinary compiled-file instruction-set versions
are unchanged. Each wide repetition adds two integer-constant instructions;
it does not reserve storage proportional to the bound. Representative source
compilation, saved-grammar loading and batch parsing are measured against the
parent commit. Because this is a correctness change, measured impacts and
rerun conditions are recorded in its commit message; raw results stay local.

Generated TypeScript uses JavaScript `number` for repetition counters and
bounds. Values through 2⁵³−1 are exact; larger bounds may round. The 2³¹ and
2³² boundaries are exact. The tiny finite-input cases at the 64-bit maximum
check acceptance equivalence, not execution of that many iterations. Exact
TypeScript counting beyond its safe-integer range remains a separate runtime
representation question.

The reference repository currently assumes 64-bit Go in other components:
the TypeScript generator's safe-integer comparisons, the instruction operand
decoder and Document resumption contain constants that prevent a linux/386
build. That failure also occurs at the parent revision. This change specifies
host-int range validation for wide bounds; it does not establish 32-bit Go
support for the repository.
