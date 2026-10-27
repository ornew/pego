# 117. Straight-Line Actions and No Unread Variables in Generated Parsers

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

A parser generated without work controls (`NoWorkControls`, the form of the checked-in
parsers) has nothing to charge steps to. Emitting the work-controlled shape and cutting
the charges out of the text leaves residue in every action:

- blank lines inside function bodies and `{ ; return ...` statements;
- every term of an action wrapped in `func() any { c.P.StepWork(); return E }()`, which
  becomes `func() any { ; return E }()` after the cut;
- `New` of a typed struct as a closure that assigns the fields to temporaries and then
  calls the constructor, and constructors with a `StepWork` per field;
- variables that are assigned and never read, silenced with `_, _ = x5, x8`, and counters
  that are incremented and never read.

The generator knows that it writes no work controls (`generator.noWork`: no
`WorkControls`, and the work specialization is not disabled) and emits no step charges at
all: not the `StepWork` and `InputWork` statements of rules, constructors and list
converters, and not the closures around terms and operands, so a term is the expression
itself. Work-controlled output keeps its charges.

A typed `New` whose fields are written in declaration order, the common case, is a direct
call of the constructor, `tmk_T(c, final, A, B)`; Go evaluates the arguments from left to
right, which is the order written. When the fields are written out of order the
temporaries stay, assigned in the order written, because an error in one field has to win
over the next.

The unread variables are decided when a function is printed from Flow IR. A variable is unread when
no statement that stays reads it: the printer records each assignment and increment of a variable
(`Set`, `Inc`, `Peek`) apart from the reads, and settles them once the function is complete. An
assignment whose targets are all unread variables and whose values have no effect and cannot panic
(a selector of a receiver, a constant, an arithmetic or comparison of such) is left out; the
variable is not declared. An assignment whose value can have an effect, a call or an operation
that can panic (an interface comparison, a shift, a division, a conversion), stays with the blank
identifier as the target of each unread variable. Leaving an assignment out leaves the variables it read unread, so
the decision repeats until nothing changes. A read at a point the printer finds unreachable does
not count, since that code is not printed. A variable that an `if` initialization statement assigns
cannot be left out, and one written opposite an untyped `nil` in an assignment of several values
cannot take the blank identifier, so each stays declared and is assigned to the blank identifier
on the first line when nothing reads it.

## Results

`parser.go` with `-typed-only`, bytes and lines before (runtime alias, record 116) and
after:

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| duckdb | 1,786,254 | 1,652,196 | 88,299 | 81,091 |
| typescript | 1,073,422 | 991,752 | 51,783 | 47,148 |
| python | 715,196 | 629,922 | 32,706 | 28,833 |
| golang | 600,447 | 542,999 | 29,591 | 26,780 |
| cue | 440,813 | 411,243 | 20,678 | 19,133 |
| cel | 124,782 | 118,278 | 6,012 | 5,599 |

No function other than the table initialisation functions contains a blank line.

Test binaries shrink: duckdb 18,972,866 to 18,686,210 bytes (-1.5%; text 11,223,040 to
11,042,816), typescript -1.6%, python -2.3%, golang -1.7%, cue -0.7%, cel -0.1%. Cold
compile of the duckdb test binary (best of three, clean build cache, two rounds) is 7.3 s
and 7.4 s before, 7.1 s and 7.4 s after: no difference beyond the noise.

Parse speed, minimum of 30 interleaved runs against the build without the change with an
identical-copy control: duckdb +0.7% (control +0.1%), typescript -1.5% (control -0.8%),
cel +0.9% (control 0.0%); the differences to the control are inside the noise.

## Tests

`TestGenerateStraightLineActions` pins the generated form (no blank lines inside
functions, no `{ ;`, no step charges, the constructor call for
fields in declaration order and the temporaries for fields out of order; work-controlled
output still charges steps). `TestGeneratedTypedConstructorEvaluationOrder` compares the
generated parser with the engine for constructors with the fields in and out of
declaration order, where the two fields fail with different errors, so a swapped
evaluation order fails the test.

`TestFlowVars*` test the decision on assignments formed by hand: the cascade of an
assignment whose reader is left out, an effect that stays with the blank identifier, a variable that
stays silenced, a pair of variables that only read each other (they stay), and a variable read
only at an unreachable point. `TestGeneratedLocalVariables` type-checks the output for every test
grammar in the forms with and without work controls, with Documents and as Nodes, fails on any
generation error and reports any type error, which includes an unused variable and a use of one that
was not declared; `TestLocalErrorsDetectsUnusedVariable` is its negative control, and
`TestFlowVarsMutantFailsToCompile` shows that treating every variable as unread fails the check. The
same test checks that the forms without work controls have no blank line inside a function.

The engine suite passes in both fixture modes (`PEGO_TEST_ORDINARY=1` generates without
work controls, which the default run does not exercise).

## Decision and Limits

Adopted. Closures remain where they are needed: lazy operands of `&&`/`||` (`c.Or`),
lambdas of list operations and statement blocks that build a list. The decision takes a
selector of a receiver (`p.Pos`) to be free of effects; the generated functions never
pass a nil receiver. The variables of a Flow IR function have names that are unique in it, so a
local that has the name of a package-level declaration is treated like any other.
