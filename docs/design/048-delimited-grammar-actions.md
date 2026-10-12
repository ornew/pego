# 048. Delimited and Per-Alternative Grammar Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Add an explicit braced form for rule actions and an action form that can wrap a grammar expression, including any alternative sequence in a nested choice or group. Braces make action boundaries visible in multiline grammars; local actions let a branch construct its own value without a helper rule.

```pego
def expr: Expr =
    left:term "+" right:term => { new Add{Left: $left, Right: $right} }
  / value:term             => { new Value{Term: $value} }
```

The existing `-> action` spelling remains valid. The proposed `=> { action }` form wraps the nearest alternative sequence: it binds after the sequence, before the enclosing choice slash. Inside parentheses, it applies within that group's choice; after a parenthesized group as a whole, it applies to the group result.

## Motivation

Today a rule has one optional action after its entire parsing expression: `def name = expression -> action`. The action documentation recommends separate rules when alternatives need different result values. This is clear but can add forwarding rules and captures solely to construct branch-specific values. An explicit delimiter also helps readers and tooling distinguish action arithmetic from the slash that begins PEG choice.

The grammar already uses `=>` inside action lambdas, but grammar bodies and action terms are parsed in distinct contexts. A braced form can therefore be introduced without changing existing action-term syntax.

## Goals

- Preserve every existing rule action and grammar without edits.
- Allow an action to wrap a selected alternative sequence at any grammar nesting level, including inside groups.
- Make the action's value, capture scope, error behavior, and ordered-choice interaction explicit.
- Keep format/parse, type inference, serialization, generated Go, TypeScript, and VM behavior aligned.

## Non-goals

- Statements, local declarations, control-flow blocks, or a second action language.
- Changing existing predicate, recovery, Pratt, or stream behavior; preserve existing value-needed action analysis.
- Cross-file grammar composition or import syntax.

## Design

### Public behavior and contracts

Keep `-> term` as the legacy whole-rule action. Add `-> { term }` as an equivalent explicitly delimited whole-rule action. Permit `=> { term }` after a grammar expression sequence:

```pego
def item: Item =
    name:identifier "=" value:number => { new Binding{Name: $name, Value: $value} }
  / value:number                   => { new NumberValue{Value: $value} }
```

When value-needed analysis requires the action, it runs only after the expression it wraps has matched successfully. In value-discarding and recognition contexts, unused actions are omitted under the existing policy, including their otherwise-fatal errors. It replaces that expression's ordinary value; an expression without an action keeps its current value. Returning nil is still a successful match and does not change the consumed input. Its `$n` references count valued elements in the wrapped alternative sequence, and named captures refer only to names in that local sequence. The action can read parser variables visible at the containing rule, but cannot refer to a sibling alternative's captures or names scoped inside a nested child expression; a parent capture of a group receives only that group's final value. Its result follows the current action contract: a node value or nil, checked against the surrounding declared/inferred type. Repetition retains its current progress rule: the first successful empty iteration counts and contributes its value, then iteration stops and the minimum is checked. A minimum of one can succeed; a minimum of two fails after that empty iteration. An action's nil/value result does not change this rule.

If an evaluated action fails, the failure is fatal just like a failing rule action; ordered choice does not continue to a later alternative, and `#recover` does not absorb that fatal action error. A syntax failure before the wrapped expression succeeds does not execute its action. A whole-rule action (`-> ...`) may follow a rule body that contains nested actions: inner actions produce their local values, then the whole-rule action transforms the final rule value as it does today.

`=>` must be followed by a braced action. Braces delimit one existing action term, not a statement block. The brace parser must consume the matching close brace, including nested struct-literal braces, lambda bodies, and parentheses. This makes `"a" => { $x / 2 } / "b" => { $y }` unambiguous: slash inside the braces is action division; slash after the close brace is PEG choice. The legacy unbraced form is parsed exactly as before, including its current slash-as-action-operator behavior. Missing braces, an empty action, or multiple actions at one boundary is a targeted source error.

The syntax may occur in a grouped choice and in other normal grammar contexts where an equivalent helper rule can be called. Apply existing value-needed analysis in lookahead, value-discarding expressions, recovery and repetition. Like current value-free rule twins, unused wrapper actions are skipped; required actions execute after matching, and their failures remain fatal. Predicate dependencies still force evaluation of the actions that supply their values. Pratt operand/operator entries retain their existing specialized action syntax and scope; `=>` does not redefine those entries.

### Implementation and invariants

Represent the new form as a generic `ActionExpr{Expr, Action}` wrapper, so actions can attach to a sequence inside a choice, to a nested group expression, or to other supported grammar expressions without adding action arrays only to top-level `Choice`. Parse precedence so `e1 e2 => { ... } / e3` wraps the first sequence, while parentheses let a group carry its own inner or outer action. Validation ensures action captures refer only to the wrapper's expression scope and that the action result is valid in its parent context. The wrapper is an expression node, not a synthetic rule call: it adds no rule depth, memo key, trace event, variable-binding scope, or cut boundary. Its value and captures roll back through existing enclosing marks, while variable updates and cuts follow their current grammar-expression scope. Implement it directly in predicate, recovery, repeat, cut, memo and left-recursion paths.

Every backend must execute a value-needed action only after the wrapped expression has succeeded and before publishing its value. A constructor action's node range is the range of the wrapped expression; returning an existing node preserves that node's existing range. Backtracking must discard wrapped captures and values when the surrounding expression later fails. If a later sequence element fails, a successful action result must not leak through rollback. The wrapper must not introduce a new variable-binding restoration point, cut boundary, rule depth, memo entry, or trace event; memo and left-recursive execution must preserve existing enclosing rule ownership. The action must not run when its wrapped alternative fails.

Update action-use analysis, capture/type checking, visitors, grammar formatting, and grammar JSON serialization for the new wrapper. Format the new syntax with braces and preserve comments and line breaks. Compile the syntax into an explicitly versioned bytecode representation if bytecode instructions change; never reinterpret existing serialized bytecode as carrying nested actions.

### Compatibility and backend support

Legacy `-> term` remains accepted and formats as before. `-> { term }` and `=> { term }` are opt-in source forms and should be rejected clearly by older parsers rather than silently changing old grammar meaning. A serialized `ActionExpr` introduces a new AST JSON node tag, so older strict readers must reject it as unknown; do not call that change backward-compatible or silently drop it. Strict validation must enforce that wrappers contain a valid expression and action. Any bytecode representation change needs a format-version gate.

All execution modes must support the same syntax and contract: closure, recursive bytecode, iterative bytecode, generated Go including typed output, and generated TypeScript. The language server must tokenize and recover from braces and `=>`, provide action-scope navigation for local captures, and distinguish the action arrow from lambda arrows inside a braced action. Recognition retains its no-result contract and executes predicate-dependent/value-needed actions. It skips unused actions and their errors exactly as current recognition does; adding a wrapper does not make an otherwise-unused action observable.

### Implementation sequence

1. Add braced whole-rule actions, shared braced-term parsing, and ambiguity/formatting tests while preserving legacy unbraced parsing.
2. Add the generic action-expression wrapper and top-level choice actions in the closure reference backend.
3. Extend the same wrapper semantics to grouped and nested choices and their normal parser contexts. Compare returned values, action errors, and ranges with equivalent helper-rule controls; separately verify that the wrapper introduces no new rule, variable-binding, cut, memo, depth, or trace boundary.
4. Add recursive and iterative VM lowering with an explicit bytecode version decision, then generated Go/typed Go and TypeScript support.
5. Add LSP syntax, scope, and recovery support; update grammar/action specifications after all supported backends agree.

## Alternatives considered

- **Continue requiring helper rules.** This is already documented, unambiguous, and portable. It remains the workaround and reference behavior, but can create extra names and forwarding captures when a branch-specific constructor is the only purpose.
- **Lower each action expression to a synthetic helper rule.** This can reuse the current rule-action machinery, but adds rule depth, memo entries, trace events, and synthetic source/name behavior. Prefer an explicit wrapper unless measurements or implementation constraints justify the extra hidden rules.
- **Allow only braces around the existing whole-rule action.** This improves visual delimiting but does not let each alternative select a different result; retain it as a first implementation stage, not the final feature.
- **Use `->` on every alternative.** This reuses the current arrow but makes it harder to distinguish the action boundary from the existing whole-rule action. `=>` in expression position is distinguishable while the action is braced.
- **Restrict actions to top-level alternatives.** This simplifies an initial AST change but prevents a group from owning its result and makes the final scope inconsistent with ordinary expression composition. Implement top-level support first if useful, while retaining nested/group actions in the proposed contract.
- **Parse actions without braces.** The existing action term parser treats `/` as division while the grammar uses `/` for ordered choice. Braces establish an unambiguous boundary without changing old grammars.

## Testing

Test legacy unbraced action parsing and formatting unchanged, braced whole-rule actions, and actions on top-level and grouped alternatives. Verify that slash inside braces is division and slash after the matching brace is ordered choice, including nested struct/lambda braces and malformed delimiters. Check selected-branch values, local `$n` and named captures, outer parser variables, no sibling/internal capture leakage, parent group captures, constructor spans, returned-node spans, type errors, fatal action errors, and no action execution for a failed alternative.

Run differential tests across closure, both bytecode VMs, generated Go/typed Go, and TypeScript. Cover grammar JSON and bytecode round trips/version rejection, format-parse-format stability, comments/newlines, lambda arrows inside braces, and LSP completion/diagnostics/scope at malformed delimiters. Use equivalent helper-rule grammars only as controls for returned values, action errors, and spans. Separately test a grouped action followed by a failing sequence suffix, parent capture of the reduced group, variable assignment visibility/restoration, cuts, memoized calls, left recursion, lookahead, recovery, and repetitions to prove the wrapper does not add a hidden rule boundary. Verify errors from evaluated actions bypass ordered-choice fallback and `#recover`. Compare unused failing actions with predicate-dependent actions in lookahead, value-discard and recognition contexts, using existing helper-rule controls. Cover empty iterations with minima zero, one and two and both nil and node results.

## Performance and results

No performance result is established. Compare the new action path with the helper-rule workaround on representative grammars, measuring parse time and allocations separately from grammar compilation and code generation. Also verify that grammars without per-alternative actions retain their current generated and runtime paths.

## Limitations and open questions

The proposed final scope includes grouped and nested choice alternatives, with local sequence captures, rule variables visible, no sibling leakage, and parent visibility only of the reduced group value. The wrapper must preserve the enclosing variable/cut/memo/trace scope rather than emulate a new rule boundary; helper rules are reference controls only for values, action errors, and spans. A top-level-only implementation stage is acceptable if it does not narrow the eventual contract. Pratt operand/operator actions remain governed by their existing specialized scope and are not implicitly changed by this syntax; whether the delimiter should later support Pratt entries requires a separate compatibility design.
