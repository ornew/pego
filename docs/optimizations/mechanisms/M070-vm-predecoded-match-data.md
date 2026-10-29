# M070: Predecoded VM Match Data

A VM module stores literal strings and class descriptions. Converting those
strings to runes/bytes or preparing ASCII class membership on every instruction
would repeat setup work.

## Mechanism and Invariants

`buildVMProgram` resolves the module's strings into reusable rune and byte literal
arrays and builds ASCII masks for its classes. Literal and class instructions
reference this prepared data. The original module and its descriptions still
determine diagnostics; non-ASCII classes retain exact range checks. Recursive
and iterative prepared programs are separate, so preparation is reusable within
each selected backend.

## Scope and State

Default setup for recursive and iterative VMs only. Closure captures its own
matcher data; generated parsers emit theirs. Their analogous representations are
covered by class/literal mechanisms, not this VM preparation step.

## Reference Control

No private same-source per-instruction-conversion reference is maintained. This
is setup work with matching consumers, unlike a parse-time optimization flag.

## Evidence and Implementation

Records 017 and 032 measure ASCII class testing and literal matching
respectively. They support the consumers but do not isolate predecoding itself
from a repeated-conversion reference. No separate effect estimate is available.

Evidence records:
[017](../017-ascii-bitmaps-for-character-classes-in-the-vms.md),
[032](../032-matching-literals-against-loaded-input-in-one-loop.md).

Source owners: [`buildVMProgram, vm.Program.runes, bytes and
ascii`](../../../internal/engine/vm/vm.go), [`iterative VM literal/class
execution`](../../../internal/engine/vm/ivm.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M020](M020-character-class-tests.md), [M030](M030-literal-matching.md), [G020](G020-lazy-backend-preparation.md).

[Optimization Catalog](../README.md)
