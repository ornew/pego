# Nodes

A rule's body is a tree of nodes. Each node stands for an expression of the language, which the
[Grammar Language Specification](../language/parser-expressions.md) defines: what it matches, the value it builds and how
it fails. This chapter names the nodes and links each to its definition; it states only what Core IR adds, which is
the expectation a node records, the capture slots it writes and its effects (see
[Attributes and Effects](attributes.md)).

## Matching Nodes

| Node | Meaning |
|:--|:--|
| `Lit` | A [literal](../language/parser-expressions.md#literals). It records its expectation on failure. With Fold, each ASCII letter of the input matches either case of the same letter of the text, which is written as the grammar wrote it |
| `ClassNode` | A [character class](../language/parser-expressions.md#character-classes). It records the class's expectation on failure |
| `Any` | [Any character](../language/parser-expressions.md#any-character). It records the expectation of any character on failure |
| `Anchor` | An [anchor](../language/parser-expressions.md#anchors): the beginning or end of the input or of a line. It records the anchor's expectation on failure |
| `Succeed` | Always succeeds, consuming nothing. It has no source form |
| `FailNode` | Always fails. It has no source form |

## Combining Nodes

| Node | Meaning |
|:--|:--|
| `Seq` | A [sequence](../language/parser-expressions.md#sequence) |
| `Choice` | An [ordered choice](../language/parser-expressions.md#ordered-choice). An alternative can carry a guard (see below) |
| `Opt` | An [optional](../language/parser-expressions.md#optional) |
| `Repeat` | A [repetition](../language/parser-expressions.md#repetition) with the bounds Min and Max; a negative Max is unbounded. With a Scope, each iteration has fresh capture slots, which the element's value carries. With Project, the element builds no value and the repetition gathers the named capture of each element |
| `Look` | A [lookahead](../language/parser-expressions.md#lookahead), positive or negative. Its body runs in a silent scope, so it neither consumes nor records expectations |
| `Cut` | [`#cut`](../language/parser-expressions.md#cut). It sets the cut flag of the enclosing choice, option or repetition |

A guard on an alternative is the first terminal of its body (see [Attributes and Effects](attributes.md#first-terminals)).
The alternative is skipped, recording the guard's expectation, when the next character is not the one the guard
requires and the nesting limit allows the depth the guard counts. An alternative marked as a guarded class begins with
the guard's class, so that a guard that already read the character lets the class only consume it.

## Rule Nodes

| Node | Meaning |
|:--|:--|
| `Call` | A [rule call](../language/parser-expressions.md#rule-calls), run through the runtime with the rule's policy. Level is the minimum binding level for a call of an Operators Rule: the call applies only the operators whose level is greater, those of the level the reference names and of the tighter levels after it; 0 applies every operator |
| `Nest` | An inlined call of a rule, which has no source form: it has the scope of a rule call but is not memoized. It is a cut barrier and a variable scope with the captures of the rule's scope; on failure it removes the errors recovered inside it, and it counts one nesting level |
| `OperatorsRule` | The whole body of an [Operators Rule](../language/operators-rule.md): the runtime's loop over its lines. The lines are an optional skip line, the operand lines and the operator lines. Each line has a body, a scope of its own and an optional action; an operator line has an ID, a kind, an associativity and a level counted from 1, the loosest |

## Value Nodes

| Node | Meaning |
|:--|:--|
| `Text` | [`#atomic`](../language/pragmas.md#atomic): the matched text is the value. The body builds no value |
| `Drop` | A [discard](../language/parser-expressions.md#discard). The body builds no value |
| `Capture` | A [capture](../language/parser-expressions.md#capture): it stores the value of its body in a slot of the enclosing scope. With Project, the body is a repetition and the capture gathers the named field of its elements |
| `Pred` | A [Predicate Action](../language/predicates.md#predicate-actions): it fails unless its term is true |
| `BindNode` | Binds a [variable](../language/predicates.md#variables) to the value of a term |

## Diagnostic and Recovery Nodes

| Node | Meaning |
|:--|:--|
| `Label` | [`@error` or `@diagnostic`](../language/attributes.md): on failure of its body, the expectations recorded inside are replaced by the message (`@error`) or renamed to it (`@diagnostic`) |
| `Recover` | [`#recover`](../language/pragmas.md#recover). The skip expression builds no value |
| `Missing` | [`#missing`](../language/pragmas.md#missing): it matches its literal or, while missing-token insertion is active, inserts it, which can exceed the limit of missing tokens |
| `Stream` | The [`#stream`](../language/pragmas.md#stream) repetition of a start rule: in a stream parse, the invocation of the rule that starts the parse delivers each element as it completes. Elsewhere it is its repetition |

## Optimized Forms

Three nodes stand for expressions that a target can run more simply. Each is value-free and consumes the same input and
records the same expectations at the same positions as the expression it replaces.

| Node | Stands for |
|:--|:--|
| `Scan` | A repetition, with bounds, of one character of a class (any character if the class is absent) |
| `ClassRun` | A sequence of single characters of classes, in order |
| `Terminal` | A region of literals, classes, sequences, choices, options, negative lookaheads, `Text` and `Drop`, which a target may run as one table or program |

A `Scan` records the expectation of the character where it stops, and fails only if it matched fewer than its minimum.
No Core node charges a budget step on entry: a generated rule charges one budget unit when it is entered, a left-recursion leader charges one before each evaluation of its body and a repetition charges one on each iteration, while a `Scan` and a `ClassRun` charge one unit for each character they read.
The reference engine charges the unit of a leader after each evaluation of its body instead: a parse that completes charges the same total, but a budget can run out at a different point of the parse than in a generated parser, as budget units may differ between backends.
