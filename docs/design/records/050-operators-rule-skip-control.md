# 050. Controlling Implicit Operators Rule Whitespace Skips

- **Status**: Draft
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Investigate control over the implicit trivia skip that an Operators Rule declaration inserts before operands and operator parts. The current [Operators Rule specification](../../../spec/language/operators-rule.md#skip) documents one rule-level `skip` expression applied at both boundaries. An operator whose spelling is whitespace can therefore compete with ordinary skipped trivia, as in a whitespace-as-infix interpretation of `a b`.

This draft investigates three independent axes: whether to suppress only the implicit pre-entry skip or also skips inside an operator part; whether the control is local to an Operators Rule operator/expression or propagates into referenced rules; and whether its scope is per operator, a region (illustratively `(a b c) #noskip`), or a future global opt-in. The current explicit-trivia workaround is the control case. No scope, policy, or syntax is selected.

## Current Behavior

The specification says the rule's `skip` expression is inserted before each operand and each operator part, but not inside either part. The implementation in `internal/engine/operators_rule.go` calls `skipTrivia` before `longest` tests led operators and before prefix/operand parsing in `operatorsRuleNud`. Thus an operator part does not see the trivia consumed at the implicit entry boundary. Without a rule-level `skip`, authors can write trivia explicitly in ordinary operator parts.

## Design Questions

The axes must be specified independently. A pre-entry override changes whether leading trivia is consumed before an operator candidate; an internal-only mode could instead leave entry positioning unchanged and alter parsing within the operator part. Any whitespace operator must see raw gap text at the intended boundary. For a narrow operator-part override, ordinary RHS operand parsing resumes its normal skip. A broader expression region may intentionally continue suppressing skips in the RHS or nested Operators Rule matches; that is a separate scope choice and must define its boundaries and restoration on success, failure, cut, recovery, and backtracking. A local region does not affect a called rule: that rule applies its own skip configuration, including its own leading skip. An expression wrapper in ordinary PEG syntax has no implicit Operators Rule boundary to suppress; wrapping an Operators Rule-rule call without propagation leaves the callee behavior unchanged. A generic region that reaches Operators Rule boundaries therefore needs an explicit boundary model or a separately specified way to affect the relevant entry. A propagating mode must define nested-mode composition and restoration across rule calls.

One candidate evaluation model starts mixed candidates from the same unskipped entry cursor. Its ranking compares absolute candidate ends from that shared cursor, including trivia consumed by candidates using the normal skip, then applies existing declaration-order ties. Under that model, for `a   + b`, a `+` candidate that matches after skipped trivia can outrank a gap-only operator whose match ends earlier. This comparison is a proposal for evaluation, not a selected policy. The `$op` Match and captures belonging to an operator part represent only text matched by that part, excluding ordinary skipped trivia. The constructed operator node retains its existing contiguous range from the left operand or prefix start through the right operand or postfix end, including intervening gaps. Failed candidates must roll back candidate-local skip effects, captures, errors, cuts, and recovery state according to existing ownership rules.

The feature must preserve precedence selection, level restrictions, associativity, ordered operator ties, cut behavior, right-operand failure/backtracking, recovery ownership, and the zero-width progress rule. A local mode may allow calls to retain their own memo identity because the caller setting does not enter the callee, but this must be demonstrated rather than assumed. A propagated mode can make the same rule parse differently by context; define a correct memo/cache key and left-recursive evaluation context, or disable unsafe reuse without breaking left recursion. Generated parsers and serialized grammars must agree on any selected setting and on the unchanged default path.

## Alternatives Under Investigation

These are candidate combinations, not a ranking or selected design.

- **Per-Operators Rule-entry override.** The narrowest scope: one operator entry suppresses its implicit pre-operator skip while RHS operand parsing keeps the normal rule skip. The syntax and whether the override applies to prefix/postfix as well as infix entries remain open. A whitespace operator can then compete with skipped and raw entries under the shared-cursor ranking rule above. Decide separately whether the override suppresses only entry skip or internal skip, and whether any setting affects referenced rules.
- **Expression-region suppression.** A wrapper such as `(a b c) #noskip` could suppress implicit Operators Rule skips for a bounded expression. It can be more compositional than per-entry flags. A local wrapper would leave called rules to apply their own skip, including at their own entry; a propagating wrapper would change nested rule behavior and requires explicit memo/LR context. A narrow entry override would resume ordinary RHS operand skip after the operator; a broader region may intentionally suppress RHS or nested skips and must define those boundaries and restoration.
- **Global skip extension.** A future opt-in mode could expose or vary rule-level automatic skipping. It must not insert trivia within a token or lexeme, such as a literal, character class, or identifier match. A token/group repetition could still allow skip at selected boundaries between items; do not prohibit every repetition boundary. No global syntax or changed default is proposed, and `#atomic` must not be repurposed as whitespace control.
- **Explicit trivia in ordinary operator parts.** Omit the global `skip` and include whitespace explicitly in each ordinary operator. This is the current workaround and provides explicit local control, but duplicates trivia handling and does not offer independent automatic-skip suppression for selected candidates.

## Compatibility and Acceptance Gates

Keep the existing all-default Operators Rule path unchanged. Specify grammar validation, source locations, formatter/LSP behavior, JSON and bytecode compatibility, and parity across closure, both bytecode VMs, generated Go, and TypeScript before implementation. Add cases for raw whitespace operators, mixed skipped/raw candidates such as `a   + b`, `$op` and captures, nested rules, repeated calls under different modes, memoization and left recursion, cuts, recovery, failures, backtracking, precedence, ties, and zero-width progress.

The explicit-trivia grammar is the reference control. Verify every default-skip grammar retains identical trees, errors, and spans. After scope and semantics are selected, measure candidate-local overhead against the all-default and explicit-trivia controls with pinned revisions and inputs, serialized paired runs, and separate compilation/runtime costs. Report no performance result without measured evidence. This proposal does not authorize implementation or change the current specification.

## Limitations and Open Questions

This is a pre-proposal draft, not a recommended contract. Decide the suppression boundary, scope and propagation behavior independently before selecting syntax. A broader global extension may be investigated later, but must specify lexical boundaries and remain opt-in; preserve the current behavior until the choices and backend contracts are reviewed.
