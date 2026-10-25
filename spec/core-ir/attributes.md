# Attributes and Effects

Every node carries attributes. Some are set when the node is built; analysis computes the rest from the rule bodies and
recomputes them after a rewrite.

| Attribute | Set by | Meaning |
|:--|:--|:--|
| Want | Building | The node's value is used. A node with Want false builds no value |
| Shape | Building | The static [shape](modules.md#shape) of the node's value |
| Steps | Building | The number of work steps a work-controlled parse charges on entering the node: one for each grammar expression the node is built from |
| Nullable | Analysis | The node can succeed without consuming input |
| First | Analysis | The terminal the node must begin with, if it has one |
| Effects | Analysis | A summary of what the node can do |

## Nullability

Nullability is the least fixed point over the rules. In summary:

- `Lit` is nullable if its text is empty; classes, `Any` and `Scan` with a positive minimum are not. Anchors, `Cut`,
  `Succeed`, `Look`, `Opt`, `Pred`, `BindNode`, `Recover`, `Missing` and a `Scan` with minimum 0 are.
- `Seq` is nullable if every item is; `Choice` if any alternative is. `Repeat` is nullable if its minimum is 0, or is 1
  and its body is nullable.
- `Call` is nullable as the callee is; `Nest` as its body is. An `OperatorsRule` is nullable if an operand line's body
  is. `Text`, `Drop`, `Capture`, `Label`, `Terminal` and `Stream` are nullable as their bodies are.
- `FailNode` and `ClassRun` are not.

## First Terminals

The first terminal of a node is what its first character must be, if it has one: either the first code point of a
literal, either case of it if the literal is case-insensitive and the code point an ASCII letter, or membership in a
class. If the next character is not that, the node fails at once, recording only the expectation of the literal or the
class at the current position, after entering as many rule calls as the terminal counts as its depth.

The first terminal follows the first item of a sequence, the bodies of `Capture`, `Text`, `Drop`, `Nest` and
`Terminal`, the first class of a `ClassRun`, and calls of rules that are neither left-recursion leaders nor Operators
Rules, counting one level of depth for each call and `Nest` on the way. Every other node has none.

## Effects

An effect is one thing a node can do besides returning success or failure. A node's effect summary is the set of effects
it may have, with the capture slots it may write, in the order they are first written.

| Effect | The node may |
|:--|:--|
| Consume | Advance the position |
| Fail | Fail |
| Expect | Record expectations |
| Capture | Write capture slots of the enclosing scope |
| Bind | Bind a variable |
| Recover | Append recovered errors |
| Cut | Set the cut flag of the enclosing choice, option or repetition |
| Raise | End the parse with an error |
| Call | Call rules or enter a `Nest` |
| Commit | Deliver stream elements |

The summary of a node is the union of its children's, with these differences.

- Matching nodes. A non-empty literal, a class, `Any`, `ClassRun`: consume, fail and expect. An anchor: fail and expect.
  `FailNode`: fail. `Cut`: cut. `Succeed`: none.
- `Choice`, `Opt`, `Repeat` and `Look` do not pass on Cut. `Opt` does not pass on Fail, nor does `Repeat` with a
  minimum of 0. `Look` neither consumes nor records expectations, and can fail. A `Repeat` with a scope does not pass
  on the captures of its element.
- `Call` has the effects of the callee, except its captures, bindings and cut, which are the callee's own, and adds Call
  and Raise: a call can exceed the nesting limit. `Nest` is the same for its body. An `OperatorsRule` has consume, fail,
  expect, call and raise, and the effects of its lines.
- `Capture` adds Capture and its slot. `BindNode` is Bind. `Pred` is Fail. A `Pred`, a `BindNode`, a rule's action and
  an `OperatorsRule` line's action add Raise when their term can raise.
- `Recover` passes on Fail from neither body nor skip, and adds Recover. `Missing` is consume, expect, recover and raise:
  an insertion can exceed the limit of missing tokens. `Stream` adds Commit.
- `Scan` consumes and expects, and can fail if its minimum is positive.

A rule's own effects, as its callers see them, are those of its body without Capture, Bind and Cut.
