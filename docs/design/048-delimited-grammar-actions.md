# 048. Delimited and Per-Alternative Grammar Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

Add a whitespace-separated braced local action form that can wrap a grammar sequence, including alternatives inside a nested choice or group. Braces make action boundaries visible in multiline grammars; local actions let a branch construct its own value without a helper rule.

```pego
type Expr = Number | Word
type Number terminal
type Word terminal

def expr: Expr =
    n:number { $n }
  / w:word   { $w }
  -> $1

def number: Number = @((?0-9)+)
def word: Word = @((?a-z)+)
def main: Expr = expr -> $1
```

The local `{ action }` postfix wraps the preceding grammar sequence and must be separated from it by whitespace. The existing whole-rule `-> action` spelling remains unchanged; `-> { action }` may be used as an explicitly delimited equivalent. No grammar-level `=>` is introduced: `=>` remains the action-language lambda arrow.

## Motivation

Today a rule has one optional action after its entire parsing expression: `def name = expression -> action`. The action documentation recommends separate rules when alternatives need different result values. This is clear but can add forwarding rules and captures solely to construct branch-specific values. An explicit delimiter also helps readers and tooling distinguish action arithmetic from the slash that begins PEG choice.

The grammar already uses `=>` inside action lambdas. A whitespace-separated brace after a grammar sequence supplies a distinct, visually clear local-action boundary while keeping that lambda syntax unchanged.

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

Keep `-> term` as the legacy whole-rule action. The optional `-> { term }` spelling is an equivalent explicitly delimited whole-rule action. Permit a local `{ term }` postfix after a grammar expression sequence:

```pego
def item: Item =
    name:identifier "=" value:number { new Binding{Name: $name, Value: $value} }
  / value:number                     { new NumberValue{Value: $value} }
```

Ordered choice first selects a branch by matching. If value-needed analysis requires its local action, that branch's action runs after the sequence has matched, then a required whole-rule action runs on the selected rule value. Thus `x { A } / y { B } -> C` is permitted: matching chooses `x` or `y`, the selected required `A` or `B` runs next, and `C` runs after it. On a rule whose body is a nonsequence choice, `$1` in `C` is the reduced selected branch value (node or nil), and `$0` is the singleton list containing that value. The self-contained example above uses `$1` to return the branch value as `Expr`.

An unused local action is skipped under the existing value-needed policy, including its otherwise-fatal error; it is not evaluated merely because its branch matched. If an action is required and fails, failure is fatal: ordered choice does not try another branch, and `#recover` does not absorb the error. A syntax failure before the wrapped sequence succeeds does not execute its action. Local actions replace the wrapped sequence's ordinary value. Their `$n` references count valued elements in that sequence, and named captures refer only to names in that sequence. The action can read parser variables visible at the containing rule, but cannot refer to sibling alternative captures or names scoped inside a nested child expression; a parent capture of a group receives only that group's final value. Its result follows the current action contract: a node value or nil, checked against the surrounding declared/inferred type. Repetition retains its current progress rule: the first successful empty iteration counts and contributes its value, then iteration stops and the minimum is checked. A minimum of one can succeed; a minimum of two fails after that empty iteration. An action's nil/value result does not change this rule.

Braces delimit one existing action term, not a statement block. The parser must consume its matching close brace, including nested struct-literal braces, lambda bodies, and parentheses. In `"a" { $x / 2 } / "b" { $y }`, slash inside braces is action division and slash after the close brace is PEG choice. At least one whitespace separator is required before a local-action brace: `x{n,m}` keeps its existing adjacent repetition meaning, while `x { term }` is the local-action form. Writing `x{ term }` does not form a local action and must be rejected as missing the separator. Empty braces such as `x {}` and missing or unmatched delimiters are targeted source errors. Do not allow multiple local actions at the same boundary as a chaining syntax.

A local action is a postfix on a grammar sequence, not a primary-expression suffix. If a sequence is intended to continue after an action, make that structure explicit as `(x { A }) y` where a grouped expression may be followed by another sequence element; do not silently reinterpret the action as applying to only the preceding primary.

The syntax may occur in a grouped choice and in other normal grammar contexts where an equivalent helper rule can be called. For example, `(x { A } / y { B }) { C }` first selects and evaluates the required local branch action, then applies a required action to the group result. Apply existing value-needed analysis in lookahead, value-discarding expressions, recovery and repetition. Like current value-free rule twins, unused wrapper actions are skipped; required actions execute after matching, and their failures remain fatal. Predicate dependencies still force evaluation of the actions that supply their values. Pratt operand/operator entries retain their existing specialized action syntax and scope; local brace actions do not redefine those entries. Lambda `=>` remains valid inside a braced action term.

### Implementation and invariants

Represent the new form as an `ActionExpr{Expr, Action}` wrapper around a complete grammar sequence, not as a primary-expression suffix. Local braces bind after that sequence and before its enclosing choice slash. Parentheses let a group carry inner branch actions or a local action on the group result. If a sequence continues after a locally acted-on group, write the boundary explicitly as `(x { A }) y` where grouped expressions may be sequenced. Validation ensures action captures refer only to the wrapper's expression scope and that the action result is valid in its parent context. The wrapper is an expression node, not a synthetic rule call: it adds no rule depth, memo key, trace event, variable-binding scope, or cut boundary. Its value and captures roll back through existing enclosing marks, while variable updates and cuts follow their current grammar-expression scope. Implement it directly in predicate, recovery, repeat, cut, memo and left-recursion paths.

Every backend must execute a value-needed action only after the wrapped expression has succeeded and before publishing its value. A constructor action's node range is the range of the wrapped expression; returning an existing node preserves that node's existing range. Backtracking must discard wrapped captures and values when the surrounding expression later fails. If a later sequence element fails, a successful action result must not leak through rollback. The wrapper must not introduce a new variable-binding restoration point, cut boundary, rule depth, memo entry, or trace event; memo and left-recursive execution must preserve existing enclosing rule ownership. The action must not run when its wrapped alternative fails.

Update action-use analysis, capture/type checking, visitors, grammar formatting, and grammar JSON serialization for the new wrapper. Format the new syntax with braces and preserve comments and line breaks. Compile the syntax into an explicitly versioned bytecode representation if bytecode instructions change; never reinterpret existing serialized bytecode as carrying nested actions.

### Compatibility and backend support

Legacy `-> term` remains accepted and formats as before; its parser, including existing slash-as-action-operator behavior, is unchanged. The local `{ term }` postfix is a new source form and must not change the interpretation of adjacent `x{n,m}` repetition or legacy whole-rule actions. The optional `-> { term }` spelling only delimits a whole-rule action; it does not create a new local-action meaning. A serialized `ActionExpr` introduces a new AST JSON node tag, so older strict readers must reject it as unknown; do not call that change backward-compatible or silently drop it. Strict validation must enforce that wrappers contain a valid expression and action. Any bytecode representation change needs a format-version gate.

All execution modes must support the same syntax and contract: closure, recursive bytecode, iterative bytecode, generated Go including typed output, and generated TypeScript. The language server must tokenize and recover from local-action braces, provide action-scope navigation for local captures, and recognize `=>` as the existing lambda arrow inside a braced action. Recognition retains its no-result contract and executes predicate-dependent/value-needed actions. It skips unused actions and their errors exactly as current recognition does; adding a wrapper does not make an otherwise-unused action observable.

### Implementation sequence

1. Add the whitespace-separated local-action syntax, shared braced-term parsing, optional braced whole-rule spelling, and ambiguity/formatting tests while preserving legacy unbraced parsing.
2. Add the generic action-expression wrapper and top-level choice actions in the closure reference backend.
3. Extend the same wrapper semantics to grouped and nested choices and their normal parser contexts. Compare returned values, action errors, and ranges with equivalent helper-rule controls; separately verify that the wrapper introduces no new rule, variable-binding, cut, memo, depth, or trace boundary.
4. Add recursive and iterative VM lowering with an explicit bytecode version decision, then generated Go/typed Go and TypeScript support.
5. Add LSP syntax, scope, and recovery support; update grammar/action specifications after all supported backends agree.

## Alternatives considered

- **Continue requiring helper rules.** This is already documented, unambiguous, and portable. It remains the workaround and reference behavior, but can create extra names and forwarding captures when a branch-specific constructor is the only purpose.
- **Lower each action expression to a synthetic helper rule.** This can reuse the current rule-action machinery, but adds rule depth, memo entries, trace events, and synthetic source/name behavior. Prefer an explicit wrapper unless measurements or implementation constraints justify the extra hidden rules.
- **Brace only the existing whole-rule action.** This improves visual delimiting but does not let each alternative select a different result, so it does not meet this proposal’s branch-local action goal.
- **Introduce unbraced `=> action` on each alternative.** This is concise, but the top-level slash may be either PEG choice or action division, and `=>` already denotes lambdas inside actions.
- **Use `=> { action }` on each alternative.** Braces make the slash boundary clear, but the extra arrow is redundant with the delimiter and still requires distinguishing grammar syntax from lambda arrows.
- **Use whitespace-separated `{ action }` after a sequence (selected).** Braces delimit one existing action term without adding a grammar-level arrow; the required whitespace preserves adjacent `x{n,m}` repetition.
- **Use `->` on every alternative.** This reuses the current whole-rule action arrow and makes local actions harder to distinguish from the action that follows the entire choice.
- **Restrict actions to top-level alternatives.** This simplifies an initial AST change but prevents a group from owning its result and makes the final scope inconsistent with ordinary expression composition. Implement top-level support first if useful, while retaining nested/group actions in the proposed contract.
- **Parse new local actions without braces.** The existing action term parser treats `/` as division while the grammar uses `/` for ordered choice. Braces establish an unambiguous boundary without changing old grammars.

## Testing

Test the self-contained `Expr`/`Number`/`Word` example, including local named captures and the whole-rule `$1` result. Verify `x { A } / y { B } -> C` selects by ordered choice, evaluates a required selected action only after its branch matches, then evaluates `C`; check `$1` as the reduced selected value and `$0` as its singleton list for a nonsequence choice. Also verify unused branch actions are skipped under value-needed analysis and required action errors are fatal without fallback or recovery. Preserve legacy `-> term` parsing/formatting, and test optional `-> { term }` only as a delimited whole-rule spelling.

Verify that slash inside braces is action division and slash after the matching brace is ordered choice, including nested struct/lambda braces and malformed delimiters. Check `x{n,m}` remains adjacent repetition, `x { term }` is the whitespace-separated local-action form, `x{ term }` is rejected as missing the separator, and `x {}` is rejected as an empty action. Verify that an expression continued after a local action requires an explicit group such as `(x { A }) y` where supported, and that multiple actions cannot be chained at one boundary. Check selected-branch values, local `$n` and named captures, outer parser variables, no sibling/internal capture leakage, parent group captures, constructor spans, returned-node spans, type errors, fatal action errors, and no action execution for a failed alternative.

Run differential tests across closure, both bytecode VMs, generated Go/typed Go, and TypeScript. Cover grammar JSON and bytecode round trips/version rejection, format-parse-format stability, comments/newlines, lambda arrows inside braces, and LSP completion/diagnostics/scope at malformed delimiters. Confirm `=>` remains a lambda arrow and is not introduced as a grammar action marker. Use equivalent helper-rule grammars only as controls for returned values, action errors, and spans. Separately test a grouped action followed by a failing sequence suffix, parent capture of the reduced group, variable assignment visibility/restoration, cuts, memoized calls, left recursion, lookahead, recovery, and repetitions to prove the wrapper does not add a hidden rule boundary. Compare unused failing actions with predicate-dependent actions in lookahead, value-discard and recognition contexts, using existing helper-rule controls. Cover empty iterations with minima zero, one and two and both nil and node results.

## Performance and results

No performance result is established. Compare the new action path with the helper-rule workaround on representative grammars, measuring parse time and allocations separately from grammar compilation and code generation. Also verify that grammars without per-alternative actions retain their current generated and runtime paths.

## Limitations and open questions

The proposed final scope includes grouped and nested choice alternatives, with local sequence captures, rule variables visible, no sibling leakage, and parent visibility only of the reduced group value. The wrapper must preserve the enclosing variable/cut/memo/trace scope rather than emulate a new rule boundary; helper rules are reference controls only for values, action errors, and spans. A top-level-only implementation stage is acceptable if it does not narrow the eventual contract. Pratt operand/operator actions remain governed by their existing specialized scope and are not implicitly changed by this syntax; whether the delimiter should later support Pratt entries requires a separate compatibility design.
