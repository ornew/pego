# Core and Flow IR

Core IR and Flow IR are the two internal representations that sit between a checked grammar and the code a backend
emits. This page describes them as implemented: their nodes, effects, invariants, passes, text forms and the targets
that consume them. Read it before you change `internal/engine/core/`, `internal/engine/flow/` or the generator code
that builds and prints them. The reasons behind the design are in
[record 062](records/062-backend-independent-optimization-ir.md); the whole system is in
[Architecture](architecture.md).

Both packages are internal. Nothing about them is a public API, a file format or a contract of the
[specification](../../spec/README.md).

## Overview

| | Core IR | Flow IR |
|:--|:--|:--|
| Package | `internal/engine/core` | `internal/engine/flow` |
| Unit | A `Module`: all rules of one program variant | A `Func`: one rule body, or one framed body |
| Shape | A tree of expression `Node`s with `Attrs`, and `Term`s for actions | Variables, labeled blocks, loops and primitive operations |
| Built by | `buildCore` and `buildCoreFrom` (`internal/engine/core_build.go`) | `flow.Lower` and `flow.LowerFrame` (`internal/engine/flow/lower.go`) |
| Checked by | `core.Verify` (`core/verify.go`), once after the passes | `flow.Verify` (`flow/verify.go`), in tests only |
| Printed as text by | `core.Print`, `core.PrintNode` (`core/print.go`) | `flow.String` (`flow/print.go`) |
| Printed as code by | Nothing: it is lowered | The generator's `flowPrinter` (`gen_flow.go`, `gen_flow_recognizer.go`) |

```text
compiled Program ──buildCore──▶ Core IR ──passes──▶ Core IR ──Verify
                                                       │
                                                       ▼
                                     flow.Lower / flow.LowerFrame
                                                       │
                                                       ▼
                                                    Flow IR ──flowPrinter──▶ Go source
```

## Core IR

### Modules and Rules

A `core.Module` holds one program variant: the typed table of generated Go, or the recognizer table of a generated
Node parser. It is built from the compiled `Program` (rules, twins, analysis results, checked types), not directly from
the grammar syntax tree.

| Field | Meaning |
|:--|:--|
| `Rules` | The rules by ID, value-free twins included, each at the position of its ID |
| `Starts` | IDs of the start rules |
| `Exps` | Interned expectation strings, addressed by `ExpID`. The fixed expectations of the runtime come first, so `Any` and the anchors, which name none, record the expectation of the same index |
| `Classes` | Interned character classes: equal classes are one `*Class`, so classes compare by pointer |
| `Types`, `Vars` | Named types of the grammar and declared variables (`VarDecl`) |
| `Calls`, `SCC`, `Cyclic` | The call graph: callees of each rule (`Call` and `Nest` nodes, Operators Rule lines included), the strongly connected component of each rule and whether a component is on a cycle (`CallGraph`, `core/graph.go`). Passes that add or remove calls run `CallGraph` again |
| `Typed` | The module carries checked types: every term but a lambda, every capture slot and every rule value has one. `Type` is an opaque handle; `core` compares handles by identity only |

A `core.Rule` has the following fields.

| Field | Meaning |
|:--|:--|
| `ID`, `Name`, `Where` | Identity, and the name used in the message of a failed action |
| `Type` | Checked type of the rule value; nil for a value-free twin |
| `Body` | The expression, or an `*OperatorsRule` for an Operators Rule |
| `Want` | The body's value is used. False for a value-free twin and for a rule whose action reads only captures |
| `Scope` | Capture slots (`Slot{Name, Type}`) in slot order |
| `Finish` | How the rule makes its value (below) |
| `Policy` | How the rule is called (below) |
| `Twin` | The value-free twin of the rule, if it has one |

`Finish.Kind` is one of `FinishNone` (a twin: the value is nil), `FinishAction` (the value is the `Action` term),
`FinishSplice` (the body's value, not wrapped) and `FinishScope` (the scope value of the body with the captures
attached, shaped by `Finish.Shape`). `Finish.NodeType`, when set, converts the value to that named node type.

`Policy` records the call decisions that the compiler made: `Memo`, `Transient` (memoized only by Documents), `Plain`
(never memoized outside Documents and no captures), `Leader` (a left-recursion leader whose growth the runtime owns),
`Splice` (makes no node of its own), `NoValue` (a twin), `Seen` (the index among rules memoized from their second call,
or -1) and `Vars` (variables the rule or its callees read).

### Nodes

Every node embeds `Attrs` (see [Attributes](#attributes)). `Children(n)` returns the direct subexpressions; `Walk`
visits in preorder including Operators Rule lines; `Rewrite` replaces nodes bottom up (`core/node.go`).

| Group | Node | Fields and meaning |
|:--|:--|:--|
| Terminals | `Lit` | `Text`, `Fold` (ASCII case-insensitive), `Exp`. The empty literal always matches |
| | `ClassNode` | `Class`: one character of the class |
| | `Any`, `Anchor` | Any one character; `BeginInput`, `EndInput`, `BeginLine`, `EndLine` |
| Control | `Seq` | `Items` |
| | `Choice` | `Alts []*Alt`; an `Alt` has `Body`, `Guard *First` and `GuardedClass` |
| | `Opt`, `Repeat` | `Repeat` has `Min`, `Max` (negative is unbounded), `Scope` (fresh captures per element) and `Project` (the capture whose values the repetition gathers) |
| | `Look` | `Body`, `Neg`: a lookahead, a silent scope that restores the position |
| | `Cut`, `Succeed`, `FailNode` | `#cut`, the empty match, never matches |
| Rules | `Call` | `Rule`, `Level`: a call through the runtime (memo, left recursion, depth, trace). `Level` is the minimum binding level of an Operators Rule call, 0 for every operator |
| | `Nest` | `Rule`, `Body`: an inlined call with the scope of a rule call and no memoization |
| | `OperatorsRule` | `Skip`, `Operands`, `Ops` (`Operator{ID, Kind, Assoc, Level, Line}`); each `OpLine` has `Body`, `Scope`, `Action` and `Shape`. The runtime owns the loop |
| Values | `Text`, `Drop` | The matched text as the value; no value |
| | `Capture` | `Slot`, `Name`, `Body`, `Project`: stores the body's value in a slot of the enclosing scope |
| | `Pred`, `BindNode` | Predicate Action, variable binding, each with a `Term` |
| Annotations | `Label` | `Exp`, `Diagnostic`: `@error` replaces and `@diagnostic` renames the expectations recorded inside |
| | `Recover`, `Missing` | `#recover` with `Skip`; `#missing` with its `Lit` |
| | `Stream` | The `#stream` repetition at the top of a start rule; elsewhere a plain `Repeat` |
| Value-free forms | `Scan` | `Class` (nil means any character), `Min`, `Max` |
| | `ClassRun` | `Classes`: consecutive single characters |
| | `Terminal` | `Body`: a terminal region that a target may run as one program |

### Terms

Actions, predicates and bindings are terms (`core/term.go`). Names are resolved to capture slots (`SlotRef`), lambda
parameters and Operators Rule locals (`Local`, kinds `LocalParam`, `LocalLhs`, `LocalRhs`, `LocalOp`) and variables
(`VarRef`). Each term carries its checked type (`TypeOf`, `SetType`); a lambda has none. `Raises(t)` reports whether
evaluating a term can end the parse with an error: constants, slot, local and variable reads and conversions that the
checker proved cannot; everything else can.

| Term | Meaning |
|:--|:--|
| `Const`, `SlotRef`, `Local`, `VarRef` | Constants and reads |
| `Member`, `New`, `Convert`, `Tuple`, `Cast` | Member reads, struct construction (`Final`, `InOrder`), node-type conversion (`Proven`), tuples, casts |
| `Builtin`, `Lambda`, `Binary`, `Unary` | Built-in calls (`len`, `text`, `foldl`, `foldr`, `map`, `filter`, `list`, `concat`), lambdas and operators |
| `TextEq`, `FieldCompare`, `TextIn`, `ConcatFused`, `ProjectedMap` | Forms made by `RewriteTerms`: unboxed text comparison, unboxed field comparison, text membership in literals, fused `concat`, `map` over a projected repetition |

### Attributes

`Attrs` is set in two stages. The builder sets `Want`, `Shape` and `Steps`; `Analyze` computes the rest.

| Attribute | Meaning |
|:--|:--|
| `Want` | The node's value is used. A node with `Want` false builds no value |
| `Shape` | The static shape of the node's value: `Elems` (a tuple of elements), `K` (valued elements), `Mode` (`SingleNone`, `SinglePass`, `SingleTuple`: how one element is made), `Tuple`, `Shared` |
| `Steps` | Work steps a work-controlled parse charges on entering the node: one per grammar expression the node is built from |
| `Nullable` | The node can succeed without consuming input |
| `First` | The terminal the node must begin with: a literal (`Rune`, `Fold`) or a `Class`, its expectation `Exp`, and `Depth`, the number of calls and `Nest` nodes entered on the way |
| `Eff` | The effect summary (below) |

### Effects

`Eff.Set` is a bit set of the following effects; `Eff.Slots` lists the capture slots of the enclosing scope that the
node may write, in the order they are first written.

| Effect | Meaning | Introduced by | Removed or not passed on by |
|:--|:--|:--|:--|
| `EffConsume` | May advance the position | A non-empty `Lit`, `ClassNode`, `Any`, `Scan`, `ClassRun`, `Missing`, `OperatorsRule` | `Look` |
| `EffFail` | May fail | A non-empty `Lit`, `ClassNode`, `Any`, `Anchor`, `FailNode`, `Pred`, `Look`, `ClassRun`, `Scan` with `Min` > 0, `OperatorsRule` | `Opt`, `Repeat` with `Min` 0, `Recover` |
| `EffExpect` | May record expectations | A non-empty `Lit`, `ClassNode`, `Any`, `Anchor`, `Scan`, `ClassRun`, `Missing`, `OperatorsRule` | `Look` |
| `EffCapture` | May write capture slots | `Capture` | `Repeat` with its own `Scope`; `Nest`, `OperatorsRule` lines and rule calls do not pass it on |
| `EffBind` | May bind a variable | `BindNode` | `Nest`, `OperatorsRule` lines and rule calls do not pass it on |
| `EffRecover` | May append recovered errors | `Recover`, `Missing` | |
| `EffCut` | May set the cut flag of the enclosing choice, option or repetition | `Cut` | `Choice`, `Opt`, `Repeat`, `Look`; `Nest`, `OperatorsRule` lines and rule calls do not pass it on |
| `EffRaise` | May end the parse with an error | `Call`, `Nest`, `OperatorsRule`, `Missing`; a `Pred`, `BindNode`, rule action or line action whose term `Raises` | |
| `EffCall` | Calls rules or enters a `Nest` | `Call`, `Nest`, `OperatorsRule` | |
| `EffCommit` | May deliver stream elements | `Stream` | |

Every other node (`Seq`, `Choice`, `Text`, `Drop`, `Label`, `Terminal` and the like) has the union of its children's
effects, less what the table removes. `Succeed` has none.

The effects of a call are the least fixed point over the rules, without the local effects (`EffCapture`, `EffBind`,
`EffCut`). `Nullable` is computed in the same fixed point.

### Where Each Concern Lives

| Concern | Representation |
|:--|:--|
| Input consumption | `EffConsume`, `Nullable`; the primitives `Lit`, `ClassNode`, `Any`, `Anchor`, `Scan`, `ClassRun`, `Terminal`. Positions are opaque: primitives advance by characters, never by a unit count fixed at build time |
| Expectations | A primitive and the expectation it records on failure are one node (`Lit.Exp`, `Class.Exp`). Scope nodes decide how recorded expectations combine: `Look` is silent, `Label` replaces or renames, `Recover` isolates, and `Call` isolates through the runtime. Choice guards record `First.Exp` when they skip an alternative |
| Values and the syntax tree | `Want`, `Shape`, `Rule.Finish`, `Capture`, `Text`, `Drop`, `Repeat.Project`, `Repeat.Scope`; terms for actions. Value-free twins (`Rule.Twin`, `Policy.NoValue`) are separate rules that call sites use where no value is wanted |
| Recovery | `Recover`, `Missing`, `EffRecover`. A module with a `Recover` makes backtracking points save the number of recovered errors (`flow.Recovers`) |
| Memoization and left recursion | `Policy.Memo`, `Transient`, `Plain`, `Leader`, `Seen`, `Splice`; `Call` goes through the runtime, which owns memo tables and leader growth. `CallGraph` supplies the components that decide cycles |
| Work charging | `Attrs.Steps` on every node; `flow.Options.Work` turns the charges into `Step` statements. Input work is charged by `Peek` |
| Stream commit | `Stream` and `EffCommit` |

## Verifier Invariants

`core.Verify` returns the first violation among the following. It runs on an analyzed module.

- Every rule has its index as ID and a body. Start rules, `Call` and `Nest` name existing rules. A twin is value-free
  (`Policy.NoValue`, `Want` false). A rule with `FinishAction` has an action. In a typed module a rule that is not a
  twin has a type.
- The call graph (`Calls`, `SCC`, `Cyclic`) equals the one computed from the bodies.
- A `Nest` of rule r appears only in rules outside the component of r, and never inside another `Nest` of that
  component.
- An `OperatorsRule` is the whole body of its rule. A `Stream` is at the top of a start rule: its body, an item of its
  body sequence or the body of a capture that is one of these, or the body of labels at one of these places.
- Every `Capture` and `SlotRef` names a slot of its enclosing scope by index and name. Scopes are the rule's scope, a
  `Repeat` scope, an Operators Rule line scope, or the scope of the rule of a `Nest`.
- The bodies of a negative `Look`, `Text`, `Drop` and the skip of a `Recover` want no value and write no capture of
  the enclosing scope. The body of a positive `Look` wants a value only where it captures.
- Expectations are expectations of the module; classes are the module's classes, each interned once.
- A choice guard equals the `First` of its alternative. An alternative marked `GuardedClass` begins with the guard's
  class, at depth 0.
- Repetition and `Scan` bounds are ordered; an Operators Rule operator has a level of at least 1; `Missing` has a
  non-empty literal.
- A `Terminal` contains only literals, classes, sequences, choices without guards, options, negative lookaheads,
  `Text` and `Drop`, and wants no value. `Scan` and `ClassRun` want no value.
- In a typed module every term but a lambda, every `FieldCompare` operand, every slot and every rule value that is not
  a twin has a checked type.

## Core Passes

Passes are plain functions over a module (`core/passes.go`, `core/attrs.go`, `core/terms.go`). Each states a
precondition and leaves `Analyze` to be run again.

| Pass | Reads | Changes | Precondition |
|:--|:--|:--|:--|
| `CallGraph` | Bodies | `Calls`, `SCC`, `Cyclic` | None. Run by the builder |
| `Analyze` | Bodies, rule finish, policy | `Nullable`, `First`, `Eff` of every node | Rule references are valid |
| `RewriteTerms` | Terms | Terms: `TextEq`, `FieldCompare` (option), `TextIn` (option), `ConcatFused`, `ProjectedMap`, `New.InOrder` | None. Results and the order of errors stay the same |
| `Guards` | `First` | `Alt.Guard`, `Alt.GuardedClass` (with `classes`) | `Analyze` has run. Terminals get no guards |
| `Scans` | `Want`, `Repeat` | A value-free `Repeat` of a class or `Any` without `Project` or `Scope` becomes a `Scan` | `Want` set |
| `ClassRuns` | `Want`, `GuardedClass` | Two or more consecutive classes of a value-free `Seq` become a `ClassRun`, except the first class of a `GuardedClass` alternative and anything inside a `Terminal` | `Guards` has run |
| `Terminals` | `Want`, `TerminalLeaves` | The outermost value-free terminal form that has a choice and at least `MinTerminalLeaves` (16) literals and classes becomes a `Terminal` | `Want` set; no guards yet. Case-insensitive literals are not terminal leaves |

A `Terminal` charges the steps of its entry only. A `ClassRun` charges none on entry. `Scan`, `ClassRun` and `Terminal`
consume the same input and record the same expectations at the same positions as the expressions they replace.

## Printed Text Form

`core.Print(m, attrs)` writes one line per rule and per node, children indented by two spaces. With `attrs`, each node
line ends with `[...]`: `want(shape)`, `nullable`, `first=...` (a class, or a quoted rune, with `+n` for depth `n`)
and the effects, where `capture[...]` lists slots.

The example grammar is the following.

```pego
package list

type Item = { Name node[string] }

@start
rule main = items:item+ $$

rule item Item = ws n:name ("," / ";")? => Item{Name: $n}

rule name node[string] = [a-z]+ #atomic

rule ws = [ \t]*
```

The Core IR as built, before the passes:

```text
rule 0 main want memo transient scope=[items] scope-value(elems,k=1,mode=2,tuple)
  seq
    capture 0 items
      repeat 1 -1
        call 1:item
    anchor end-input
rule 1 item memo transient scope=[n] => Item{Name: $0:n}!
  seq
    call 4:ws
    capture 0 n
      call 2:name
    opt
      choice
        alt
          lit ","
        alt
          lit ";"
rule 2 name want memo transient plain scope-value(k=1,mode=1)
  text
    repeat 1 -1
      class [a-z]
rule 3 ws want memo transient plain twin=4 scope-value(k=1,mode=1)
  repeat 0 -1
    class [\u0020\u0009]
rule 4 ws memo transient plain novalue
  repeat 0 -1
    class [\u0020\u0009]
```

Rule 1 calls rule 4, the value-free twin of `ws`, because it wants no value there. The action's
`$0:n` is slot 0, named `n`; the final `!` marks the term that makes the rule's own value.

After `RewriteTerms`, `Analyze`, `Guards`, `Scans` and `Analyze`, printed with attributes (rules 0 to 2):

```text
rule 0 main want memo transient scope=[items] scope-value(elems,k=1,mode=2,tuple)
  seq [want(elems,k=1,mode=2,tuple) consume,fail,expect,capture[0],raise,call]
    capture 0 items [want(k=1,mode=1,tuple) consume,fail,expect,capture[0],raise,call]
      repeat 1 -1 [want(k=1,mode=1) consume,fail,expect,raise,call]
        call 1:item [want(k=1,mode=1) consume,fail,expect,raise,call]
    anchor end-input [want(k=0,mode=0,tuple) nullable fail,expect]
rule 1 item memo transient scope=[n] => Item{Name: $0:n}!
  seq [consume,fail,expect,capture[0],raise,call]
    call 4:ws [nullable consume,expect,raise,call]
    capture 0 n [consume,fail,expect,capture[0],raise,call]
      call 2:name [want(k=1,mode=1) consume,fail,expect,raise,call]
    opt [nullable consume,expect]
      choice [consume,fail,expect]
        alt guard=','
          lit "," [first=',' consume,fail,expect]
        alt guard=';'
          lit ";" [first=';' consume,fail,expect]
rule 2 name want memo transient plain scope-value(k=1,mode=1)
  text [want(k=1,mode=1) consume,fail,expect]
    scan [a-z] 1 -1 [consume,fail,expect]
```

`name` shows a `Scan` in place of the value-free repetition under `#atomic`; `item` shows the guards of the two
alternatives. The twin `ws` (rule 4) becomes `scan [\u0020\u0009] 0 -1`.

## Flow IR

A `flow.Func` is the body of a rule variant with explicit parser state and structured control (`flow/flow.go`). It is
the input of the Go printer; it contains no host-language text.

A function is local or framed.

| | Local (`flow.Lower`) | Framed (`flow.LowerFrame`) |
|:--|:--|:--|
| Lowers | A whole rule: `LocalRule(r)` | A body whose captures the runtime keeps in frames: the body of a left-recursion leader or of a rule with `#recover`, and each line of an Operators Rule (`FramedBody`) |
| Captures | Variables of the function | Slots of the current runtime frame (`SetCapture`, `FrameVal`, `NewFrame`) |
| Success | Running to the end, with the value in `Result` | The value `Func.Value`; the runtime owns the frame, the rule's value and its finish |
| Failure | `Exit` to `Fail` | `Exit` to `Fail` |
| Entry variables | `Start`, `Result`, `Ctx` | None. `Cut`, if the body cuts, is the flag the runtime gave the outermost scope, read from `RegCut` on entry and published on both returns |

`LocalRule` is false for a leader, an Operators Rule, a rule containing `Recover`, and a rule containing `Nest`.
`FramedBody` is false for a body containing `Nest`, `OperatorsRule` or `Terminal`. Lowering `Nest` is not implemented:
the lowerer panics on it, and no pass produces it.

### Statements

| Statement | Meaning |
|:--|:--|
| `Block`, `Loop`, `If` | Structured control. `Loop` may have a `Counter` with `Max`, or a `While` condition |
| `Exit`, `Continue` | Leave a block, loop or the function's `Fail` label; start the next iteration |
| `Set`, `Inc` | Assignment to variables, registers (`RegPos`, `RegEnv`, `RegFrame`, `RegCut`) or `Discard` |
| `Step`, `Silent` | Charge one work step; add to the silent depth (no expectations while positive) |
| `Peek`, `Advance` | Read the next character without consuming; move the position |
| `Expect`, `ObserveSkip` | Record an expectation (optionally as an `@error` message); report a skipped guarded alternative |
| `Restore`, `Reset`, `DropTrail` | Go back to a position and recovered-error count; go back to a whole-state mark; drop the capture trail since a mark |
| `MergeExpected`, `MergeRenamed` | Merge expectations of an isolated scope into the enclosing one, as is or renamed (`@diagnostic`) |
| `PushKid`, `DropKids`, `Append` | Gather values |
| `SetCapture`, `ClearVals`, `FreeFrame`, `DeclareNames` | Frame operations |
| `SetWhere`, `DropCreated`, `Unfresh`, `AppendError`, `Comment` | Action bookkeeping, node ownership, recovered errors, comments |

### Expressions

Expressions are primitive operations that mirror runtime helpers: reads (`Use`, `Read`, `RecLen`, `KidsLen`),
constants and logic (`Nil`, `Int`, `Bool`, `Not`, `Or`, `Cmp`), matching (`HasLit`, `MatchLit`, `MatchClass`,
`InClass`, `GuardFails`, `AtAnchor`, `SkipTerminal`, `SkipClasses`), calls (`CallRule`), expectation scopes
(`Isolate`, `Unisolate`, `Keep`, `MakeError`), value construction (`Text`, `LiteralText`, `NewMatch`, `NewNode`,
`NewVals`, `Kids`, `ShapeSingle`, `ShapeCapture`, `ScopeValue`, `AttachCaptures`, `FreshNode`, `ConvertResult`,
`ErrorValue`), state marks (`MarkState`, `MarkPos`) and action evaluation (`Pred`, `Bind`, `Eval`, `UseCtx`,
`CtxFinish`, `Captures`). `Results(x)` is 2 for `MatchLit`, `CallRule`, `Unisolate` and `ShapeSingle`, 1 otherwise.

Variable types (`flow.Type`) are `int`, `bool`, `rune`, `value`, `env`, `expmark`, `expids`, `vals`, `ctx`, `mark`,
`frame` and `error`. Values are all of type `value`; the checked types of Core IR do not reach Flow IR.

### Options

`flow.Options` selects the forms the lowering writes.

| Option | Effect |
|:--|:--|
| `Work` | Charge a step on entering each node and on saving or restoring each capture slot; charge the input work of each character a `Peek` reads |
| `Recovers` | Backtracking points save the number of recovered errors |
| `ClassTables` | Match a class of more than four ranges through its membership table |
| `GuardedClasses` | Keep the size of the character that a choice's guards read, for classes that begin `GuardedClass` alternatives |
| `InlineLiterals` | Compare literals of at most this many code points in place first (`HasLit`) |
| `Observe` | Report alternatives that guards skip (`ObserveSkip`) |

### Flow Invariants

`flow.Verify` checks the following.

- An `Exit` names an enclosing block, the innermost enclosing loop or the function's `Fail` label. A `Continue` names
  the innermost enclosing loop. Every block and loop has a label of its own.
- Every variable read or assigned is an entry variable or one of `Func.Vars`, each listed once. A local function has
  the three entry variables, a framed one none. A framed function's `Cut` is one of its variables; a local function
  has neither `Value` nor `Cut`.
- A `Set` has as many places as values, or one value with that many results. A `Peek` has an `OK` variable.
- A loop without a bound can be left.

Variables are declared for the whole function. Statements after an `Exit` up to the end of the exited-to block are
unreachable; Flow IR keeps them and a printer leaves them out.

## Lowering Core to Flow

Lowering threads a failure label through each node: a node's statements leave the block of that label when it fails and
run to their end when it succeeds, leaving its value in a variable.

| Core node | Flow form |
|:--|:--|
| `Lit` | `HasLit`/`Advance` for short literals, then `MatchLit` |
| `ClassNode` | `Peek` and `InClass` (or `MatchClass` through a table), `Expect` on failure |
| `Call` | `CallRule`; the value-free twin is called where no value is wanted |
| `Seq` | The items in turn, each failing to the same label |
| `Choice` | One block per alternative; a failing alternative restores the saved state and falls to the next. A guard is `Peek` once, then `GuardFails` per alternative, with `Expect` and `ObserveSkip` |
| `Opt`, `Repeat` | A backtracking block; a `Loop` whose element failure restores and leaves the loop; an iteration that consumes nothing exits |
| `Scan`, `ClassRun`, `Terminal` | A loop of `Peek`/`Advance`; `SkipClasses`; `SkipTerminal` |
| `Look` | A backtracking point under `Silent`; restores the position |
| `Text`, `Drop`, `Capture` | `NewMatch` over the text; no value; a capture variable (local) or `SetCapture` (framed) |
| `Pred`, `BindNode` | `Pred`, `Bind` with the captures the term reads |
| `Label` | `Isolate`, `Unisolate`, `MergeExpected` or `MergeRenamed` |
| `Recover`, `Missing` | `Recover` in framed functions only; `Missing` lowers as its literal |
| `Cut` | A flag of the innermost backtracking point; cuts are discarded at a rule's top level and inside lookaheads |
| `Stream` | Its repetition |
| `Nest`, `OperatorsRule` | Not lowered: no local function, and no framed function for `Nest`. An Operators Rule body is lowered one line at a time as framed functions |

A backtracking point saves the position, the number of recovered errors if the module can recover, the environment if
its body binds variables, and the capture slots its body may write (`Effects.Slots`). In a framed function it marks and
resets the whole parser state instead. The rule's value is made last (`finish`): `Eval` for an action, `ScopeValue` and
`AttachCaptures` for a scope, `ConvertResult` for a node type.

The lowering follows reachability: a node that starts at an unreachable point is not lowered, nor are the alternatives
after one that cannot fail, nor the failure path of a point whose body cannot fail.

Example: the local function of rule `item` above, lowered with `Work` off, `GuardedClasses` on and `InlineLiterals`
4, as `flow.String` prints it.

```text
func item
  var ok bool
  var k1 value
  var x2 int
  var v3 value
  var x4 int
  var x7 int
  var x9 rune
  var x10 bool
  if _, ok = call(4, 0); !ok {
    exit fail
  }
  x2 = pos
  if v3, ok = call(2, 0); !ok {
    exit fail
  }
  k1 = v3
  x4 = pos
  block L6 {
    block L5 {
      x7 = pos
      block L8 {
        peek x9, _, x10
        block L11 {
          if guardfails(#5, x10, x9, depth 0) {
            expect #5 at pos
            exit L11
          }
          block L12 {
            if haslit(",") {
              advance 1
            } else {
              if _, ok = matchlit(","); !ok {
                exit L12
              }
            }
            exit L8
          }
          pos = x7
        }
        block L13 {
          if guardfails(#6, x10, x9, depth 0) {
            expect #6 at pos
            exit L13
          }
          block L14 {
            if haslit(";") {
              advance 1
            } else {
              if _, ok = matchlit(";"); !ok {
                exit L14
              }
            }
            exit L8
          }
          pos = x7
        }
        exit L5
      }
      exit L6
    }
    pos = x4
  }
  c = ctx(start)
  where "item"
  v = eval(c)[n=k1]
```

The optional `;`/`,` is a block that restores the position when neither alternative matches (`pos = x4`) and
continues; the rule fails only through `exit fail`.

The value-free twin of `ws` lowers to a loop without value construction:

```text
func ws
  var ch rune
  var size int
  var ok bool
  loop loop1 {
    peek ch, size, ok
    if (!ok || !inclass(#8, ch)) {
      expect #8 at pos
      exit loop1
    }
    advance size
  }
  v = nil
```

## Running the Passes Today

No pass manager exists. The passes are called in a fixed order by two functions, and generator fields choose which
run.

### Call Sites

| Call site | Table | Order |
|:--|:--|:--|
| `generator.typedCore` (`core_build.go`) | Typed Go rules | `buildCoreFrom`; `Terminals`; `RewriteTerms`; `Analyze`; `Guards`; `Scans`; `ClassRuns`; `Analyze`; `Verify` |
| `generator.startRecFlow` (`gen_flow_recognizer.go`) | Recognizer of a Node parser | `buildCoreFrom`; `RewriteTerms`; `Analyze`; `Guards`; `Scans`; `Analyze`; `Verify` |

`Guards` reads the first terminals that the first `Analyze` computes, so `Analyze` runs before it; a missing or
reordered `Analyze` leaves alternatives unguarded without an error. The recognizer omits `Terminals` and `ClassRuns`:
its printer writes terminals and classes as inline tests. `startRecFlow` also keeps the origin of each repetition on the
`Scan` that replaces it (`followRepeats`). `Verify` runs once, after the last pass.

`flow.Lower` and `flow.LowerFrame` are called while the typed table is generated (`flowDirectRule`, `flowFrameBody`,
`flowOperatorsRuleBody` in `gen_flow.go`) and for each eligible recognizer rule (`flowRecDirect`). In tests,
`flow.Verify` checks every lowered function.

### Switches

| Field | Set from | Effect on the passes |
|:--|:--|:--|
| `generator.lean` | No work controls and no Documents (`!WorkControls && !Document`) | `Guards` marks `GuardedClass`; `Terminals` and `ClassRuns` run only when it is set (as `leanTyped`, with `goWorkControls`); `flow.Options.GuardedClasses`; `ClassTables` follows `leanTyped` |
| `generator.goWorkControls` | Always true for Go output | Part of `leanTyped` |
| `generator.noWork` | `!WorkControls && !disableOrdinaryWorkSpecialization` | `flow.Options.Work` is `!noWork` |
| `generator.observability` | `GenOptions.Observability` | Skips `Terminals`; `flow.Options.Observe` |
| `generator.flowRecognizer` | `!directRecognizer && !WorkControls && !Document && !Observability` | Whether the recognizer table is printed from Flow IR (and `startRecFlow` runs) |
| `disableTerminalPrograms` | Tests, same-generator measurements | Skips `Terminals` |
| `disableGuardedClasses` | Tests | `Guards` is called with `classes` false; `flow.Options.GuardedClasses` false |
| `disableLiteralTextDispatch` | Tests | `RewriteTerms` without `TextDispatch` |
| `boxedComparisons` | Tests | `RewriteTerms` without `FieldCompare` |
| `directRecognizer` | The parity test only | The recognizer is printed by the direct printer, not Flow IR |

`flow.Options` for the typed table is `{Work: !noWork, Recovers: flow.Recovers(m), ClassTables: leanTyped,
GuardedClasses: lean && !disableGuardedClasses, InlineLiterals: 4, Observe: observability}` (`flowOptions`). The
recognizer's options are the same without `ClassTables`.

### What Skipping a Pass Does

| Pass | If skipped | Evidence |
|:--|:--|:--|
| `Analyze` | Not skippable. `Guards`, `Verify` and the lowering read `Nullable`, `First` and `Eff` | |
| `Terminals` | Terminal regions lower node by node. Results are the engine's | `disableTerminalPrograms` and `TestTerminalProgramDisabledParity` compare the parsers with and without programs, and their errors with the engine's, in both position units |
| `RewriteTerms` | Core terms keep their plain forms. No printer reads the rewritten terms, so no output changes | The printers format actions and predicates from the grammar terms (`generator.term`); `core_terms_test.go` and `TestCoreMatchesGenerator` compare the Core forms with the generator's decisions. The switches `disableLiteralTextDispatch` and `boxedComparisons` also steer the generator and have parity tests (`gen_literal_text_dispatch_test.go`, `text_compare_test.go`) |
| `Guards` with `classes` false | Guards stay; classes read and test the character again | `TestGuardedClassDisabledParity` |
| `Guards` | Alternatives are tried without a first-character test | No switch skips it and no test runs without it. A choice's expectations at a skipped alternative are meant to equal what the alternative records on failing, which is unverified |
| `Scans` | A value-free repetition lowers as a loop | No switch and no test skips it |
| `ClassRuns` | Classes lower one by one | Skipped already outside lean parsers, which the tests cover |

`TestCoreMatchesGenerator` and its siblings (`core_passes_test.go`, `core_gen_test.go`) compare the decisions of
each pass with the decisions of the generator's own lowering over the grammar corpus, in the modes `lean`, `lean
observable`, `controlled` and `lean boxed`. They compare decisions, not results; result equality with the engine is
checked by the generated-parser tests.

## How Targets Consume the IRs

| Target | Source of its code today |
|:--|:--|
| Typed Go (`WithTypes`) | Core IR through `typedCore`, then Flow IR. Every typed rule and every Operators Rule line is printed from Flow IR; generation fails on a rule that Flow IR does not print (`failTyped`). Terminal programs are Core `Terminal` nodes printed with `terminalProgram` (`gen_terminal.go`) |
| Untyped recognizer of a generated Node parser | Flow IR for the rules `recFlowEligible` accepts (a plain, lean, value-free rule that is not a left-recursion leader, whose body has no `Capture`, `Predicate`, `Cut`, Operators Rule or `#recover`), in ordinary builds. The direct printer (`dgen`) prints the other rules and every rule of builds with work controls, Documents or observability |
| Untyped Node table of generated Go | The direct printer, `dgen` (`gen_direct.go`, `gen_direct_node.go`), with `generator.expr` (`gen.go`) as the general fallback |
| Generated TypeScript | `tsGen.expr` and helpers in `gen_ts.go`, from the grammar |
| Closure backend | `compiler.expr` in `compile.go`, from the grammar |
| Bytecode and the VMs | `bcompiler` in `bcompile.go`, from the compiled rules |
| Actions and predicates | `generator.term` (Go), `tsGen` (TypeScript), `eval.go` (closures), `bcompile.go` (bytecode). The Flow printer formats them from the grammar terms recorded for each Core node |

The Flow printer asks the generator for the grammar expression behind a Core node (`flowState.origin`), so Core nodes
that passes create (`Scan`, `Terminal`, `ClassRun`) are printed with the generator's helpers for those forms.

## Related Topics

- [Architecture](architecture.md): the pipeline from grammar to parser, and where each mechanism lives.
- [Record 062](records/062-backend-independent-optimization-ir.md): the decision, alternatives and the planned end
  state of both representations.
- [Optimization catalog](../optimizations/README.md): measured mechanisms, several of which are Core passes.
- [Bytecode specification](../../spec/bytecode.md): the instruction set that the VMs run.
