# Feature feasibility notes

These notes record open questions and implementation boundaries. They do not select syntax or change PEGO's public contract.

## Grammar composition

The current public grammar AST represents one grammar as an ordered flat list of type and rule definitions (`grammar/ast.go`). Compilation resolves those definitions within one grammar (`internal/engine/compile.go`, `internal/engine/check.go`). Compiled parser files also combine bytecode, an optional source AST, and a start rule (`pego.go`).

A composition design would need to settle type identity and collisions, rule visibility, start-rule selection, cross-file source diagnostics, deterministic linking, and source retention. No implementation slice is justified until a concrete consumer scenario determines which of those are required. A useful first design input would be two small modules with an explicit expected linking result and diagnostic locations.

## Implicit whitespace around Operators Rule entries

The current parser skips trivia before testing led operators and before testing prefixes or operands (`internal/engine/operators_rule.go`). `longest` tries candidates from a common cursor after that skip and selects the greatest end position, preserving declaration order on ties.

Two candidate models can be compared without choosing syntax:

1. **Candidate-local entry policy.** Start every operator candidate at the same unskipped cursor. A candidate using normal behavior silently skips trivia before matching; a raw candidate matches at the cursor. Compare absolute candidate ends from the shared cursor and preserve declaration-order ties. Apply only the winning candidate's state. Resume ordinary skip for the RHS operand.
2. **Scoped skip context.** A region establishes a skip mode consulted by nested Operators Rule calls. This is more compositional, but a called rule can parse differently depending on its caller. Memoization and left-recursive evaluation would then need to account for that context, and the region must restore correctly across failure, cut, recovery, and backtracking.

The candidate-local model is the smaller first investigation because it bounds the change to operator entry. It remains only a candidate: semantics and syntax require review before implementation.

Focused comparison cases should include raw whitespace operators; mixed raw and ordinary operators such as `a   + b`; equal-end ties; leading trivia before prefixes and operands; ordinary RHS skip; `$op` and captures excluding skipped text while the operator node retains its full span; rollback after failed candidates, cuts, and recovery; nested rule calls; repeated calls under different modes; memoization and left recursion; and zero-width progress. Compare with explicit-trivia grammars and the unchanged default behavior. Measure only after semantics are selected.

## Primitive-valued lists

The current type contract describes `[]T` as a node-backed `List`; action list helpers also construct `List` nodes (`spec/types.md`, `spec/actions.md`). Generated Go typed output maps `[]T` to Go slices, while CST `List` values remain nodes (`internal/engine/gen_types.go`). Node JSON represents children and fields (`internal/engine/node.go`).

“Primitive list” could mean either a list value outside the CST or primitive payloads inside a CST `List`. These have different effects on action return values, typed conversion, and the serialized Node shape. Before designing syntax or storage, provide a concrete grammar and consumer showing why existing nodes, struct fields, or typed-boundary conversion are insufficient. Also verify that the type checker can produce the proposed element values. Until then, no new syntax or storage model is recommended.
