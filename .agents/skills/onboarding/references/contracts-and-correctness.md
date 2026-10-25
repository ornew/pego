# Contracts and Correctness

The [specification](../../../../spec/README.md) defines supported behavior;
[parsing](../../../../spec/runtime/parsing.md) covers result and position units, errors,
Documents and backend equivalence. [Rule expressions](../../../../spec/language/parser-expressions.md)
defines ordered choice, greedy repetition, scoped cut, capture rollback and left
recursion. Check these contracts before changing a parser path: a local speedup can
alter a distant parse result, expectation or recovered error.

The closure engine is the reference execution path. The bytecode VMs and generated
Go/TypeScript parsers implement the same grammar semantics, but have distinct code
paths. Relevant engine tests compare backends, both position units, memoization on
and off, recognition and tracing; generated output has additional tests. Read
[development testing](../../../../docs/development.md#testing) and the affected
tests to choose cases that expose value, position, error and interruption changes.
For Document or stream changes, include edit/reparse or commit behavior; a batch
parse alone does not exercise those paths.

Do not infer implementation from a design-record title. Check the record's status,
the [development status](../../../../docs/development.md#language-feature-status),
the relevant spec and code. If the contract is unspecified, resolve the semantic
choice with the owner before dependent implementation. For an accepted change,
update the applicable contract and guide with the implementation; the
[documentation skill](../../documentation/SKILL.md) gives the document roles.

Generated source is downstream of generator/runtime templates. Locate the source of
generation and regenerate affected consumers instead of patching generated output
alone; check the current instructions in [development](../../../../docs/development.md#testing).
