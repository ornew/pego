# Actions

PEGO has two executable action forms. A **Predicate Action** appears in braces
where a Rule Expression atom is expected. It evaluates without consuming
input or producing a value. A **Value Action** follows `=>` and computes a
rule's value from the values produced by its grammar body. An ordinary rule may
have a whole-rule Value Action after its complete expression:
`rule name [Type] = expression => term`. A Value Action may also stand inside
parentheses as an inline Value Action, `(expression => term)`, which is
described [below](#inline-value-actions). An Operators Rule has specialized
Value Actions on its operand and operator entries, and its declaration-level
action is a [whole-rule action](operators-rule.md#whole-rule-action) that runs
once per call on the folded value, `$value`.

A Predicate Action block may contain several statements; its semantics are
defined in [Predicate Actions](predicates.md#predicate-actions).

Value Action blocks (`=> { ... }`), whole-rule or inline, remain unsupported and
are rejected during compilation; their statements do not execute. PEGO has no
rule-initialization action block.

```pego
type Pair = { Left node[string]; Right node[string] }
rule pair Pair = (l:. r:. => Pair{Left: $l, Right: $r})
```

```pego
type KV = { Key node[string]; Op node[string]; Value node[string] }
rule key_value KV = (k:[a-z]+ #atomic op:"=" v:[0-9]+ #atomic) => KV{Key: $k, Op: $op, Value: $v}
```

A Value Action runs after the complete body succeeds. Its result is a value: a node
(a string, tuple, list or struct value; see [Nodes](../runtime/nodes.md#node-model)), an int, a string, a bool or `nil`, and must fit the declared or inferred rule type. It applies to the
whole body, including every alternative. Predicate Actions use the same value
operators and built-in functions described below, but their result controls
whether the predicate succeeds; see [Predicate Actions](predicates.md#predicate-actions).

## Whole-Rule Value Actions

A single-term Value Action follows `=>` after the complete rule expression.
When a grammar expression spans multiple lines, parenthesize that expression;
a line break inside a braced action block is local to the block.

## Inline Value Actions

An inline Value Action has the form `(expression => action)`. It behaves exactly
as a call of a hidden rule whose body is `expression` and whose
whole-rule Value Action is `action`, written at that position:
`(e => A)` in a rule `r` means the same as a call of the hidden rule `_r_n = e => A`,
and with `rule a = _ => A` and `rule b = a => B`, the rule `b` is the same as
`rule b = (_ => A) => B`. Every property below follows from that.

- **Scope.** The captures made inside `expression` are visible to `action`, as in a rule body, and
  nothing else is: the captures of the enclosing rule are not. The captures made inside
  the parentheses end at the closing parenthesis. The captures made before
  the parentheses stay visible to the expressions after them and to the
  whole-rule action of the enclosing rule.
- **Value.** The inline action is one element of the enclosing sequence, like a
  reference to a rule, and its value is the result of `action`. A label may name it
  (`r:(e => A)`). The value, the typed value, the AST and the schema treat it as the
  value of a rule whose type is the type of `action`. The result is a node or `nil`
  and must fit the type the action is checked against; `expression => ...` selects the
  default value of `expression`.
- **Matching.** `action` runs after `expression` matches. If `expression` fails,
  the action does not run and the failure, the expectations and the error
  position are those of the failing rule. The inline boundary is a rule call: a cut inside the
  parentheses commits only within them, a recovery inside them is that of the
  hidden rule, left recursion through them is left recursion through a rule,
  and the call counts against the maximum call depth. An action
  error is fatal to the parse: it does not retry another alternative or enter recovery.
- **Source positions.** The node of the hidden rule spans the text that `expression` matched.
- **Unused values.** An inline action in a lookahead (`&`, `!`) or a discard (`-`),
  which make no value, is a compile-time error.
- **Name.** The hidden rule is named `_r_n` for the nth inline action of the rule
  `r` in source order, inner actions first, with a trailing underscore added while
  a declared rule has that name. A reference to a rule resolves against the rules
  declared in the grammar as written, so a generated name satisfies no reference. Trace
  events, profiles and source information show the name of the hidden rule; a compile-time
  error and the text of a failed action describe the rule as
  `the inline action in rule r` (a failed action also names its number, as in
  `r (inline action 2)`).

```pego
type Add = { Left Expr; Right node[string] }
type Num = { Text node[string] }
type Expr ~ Add | Num
rule expr Expr = (l:expr "+" r:[0-9]+ #atomic => Add{Left: $l, Right: $r}) / (t:[0-9]+ #atomic => Num{Text: $t})
```

## Hidden Rules

A rule whose name begins with `_` is hidden. A hidden rule is a rule at run time:
it is memoized, may recur (also on the left), counts against the maximum call
depth, and a cut or a recovery inside it is scoped to the rule. It differs from
other rules in three ways.

- **Not an entry point.** A hidden rule MUST NOT be marked
  [`@start`](grammar-files.md#start-rules), which is a grammar error at the
  attribute, so it is not a start rule of a grammar, a compiled grammar, a schema
  or a generated parser. It cannot be parsed by name
  (`ParseRule`, `Parse(name, ...)`, `WithStart`, `parseRule` and `parseRuleWithOptions` of a
  TypeScript parser); each of these is an error that
  says the rule is hidden. The rules a parser lists do not include hidden rules.
- **No node of its own.** A hidden rule without an action, not even `=> ...`,
  makes no node. A call of it contributes what its expression contributes written
  in its place as a group (see [Nodes](../runtime/nodes.md#node-model)): the elements of a sequence
  in its body are elements of the sequence that calls it. The captures made inside it
  end with the rule and are not visible to the caller. A hidden rule with a Value
  Action yields the value of the action, as any rule does. A hidden rule without an
  action MUST NOT declare a result type, and a hidden rule whose expression is an
  Operators Rule makes its own node.
- **No type of its own.** A hidden rule without an action adds no type to the output
  schema, the AST or the typed value, except where a recursive call of it is an
  element of its own (a call in a choice, an option, a repetition or a capture),
  which has the type of the group.

A hidden rule without an action that contains a call of itself in its sequence of
elements, directly or through other such rules (`rule _a = "x" _a`), has no fixed number
of elements and is a compile-time error.

## Capture References

`$label` refers to a named capture visible to the action's expression scope.
Captures are referred to by name only: an element is labeled (`label:expr`) to
be used in an action. A capture is a name for an element of the rule's tuple, so
its value is the value of that element: a string node for a literal or class, a
list node for a repetition, a tuple node for a parenthesized group with several
valued elements. A `$` followed by a digit is a syntax error.

Captures are visible only in their enclosing grammar-expression scope. A
capture that did not match has value `nil`.

## Variable References

An action can read the parser variables that the grammar declares. Lookup uses
the nearest binding: a rule can read values bound by its callers, while
bindings in called rules are not visible to their callers. A variable that
nothing has bound has its initial value. Reading a name that is not declared is
a compile-time error. A read has the declared type of the variable. Variables
are `int`, `string` or `bool`; bindings shadow rather than mutate an existing
value. Declarations, scope and rollback are specified in [Variables](predicates.md#variables).
Inside a lambda body, the lambda's parameters are variables too and are read
the same way; see [Operators and Functions](#operators-and-functions).

```pego
type Item = { Depth int; Name node[string] }
var depth int
rule item Item = {depth := 1} n:name => Item{Depth: depth, Name: $n}
```

## Values and Constructors

Action Expressions operate on nodes, integers, strings, booleans, `nil`, and
restricted lambda functions. String and integer literals use the same escapes
and decimal syntax as grammar literals. `T{Field: value, ...}` constructs a
node of struct type `T`; unset fields read as `nil`. `N{value}` constructs a named node type `N`
from exactly one positional argument, converted as described in
[Named Node Types](types.md#named-node-types). A typed node-list literal
`[]T{value, ...}` constructs a list value whose element Type is explicitly `T`.
These forms never stringify or filter values implicitly.

```pego
type NameNode = { Name string }
rule name NameNode = v1:[a-z]+ => NameNode{Name: text($v1)}

rule twoNames []NameNode = a:name "," b:name => []NameNode{$a, $b}
rule noNames []NameNode = "" => []NameNode{}

type Identifier = { Text string }
rule identifier Identifier = t:([a-z]+) #atomic => Identifier{Text: text($t)}
rule normalized Identifier = n:identifier => Identifier{Text: $n.Text}
```

A typed node-list literal has the form `[]T{value, ...}`. Every element must
have a known static Type assignable to `T`; nested literals are checked
recursively, and an unknown element Type is an error even when the target is a
concrete node list. An empty literal such as `[]NameNode{}` has Type
`[]NameNode`. Nil is accepted only when the element Type is optional, as in
`[]*NameNode{nil}`. Elements are evaluated once, left to right; no element is
dropped implicitly. The elements may be of any type, as in `[]int{1, 2}`.

A tuple literal is written like a tuple type: `()` is the unit, `(x,)` has one
element and `(x, y)` two, and a trailing comma is optional after two or more
elements. Parentheses around one expression without a comma are a group. A
tuple literal constructs a tuple value of its elements, which are evaluated once,
left to right, and keep their kinds (`nil` stays `nil`; functions are not
values). Its Type is the tuple of the element Types, so it is assignable to a
declared tuple Type of the same arity whose elements admit those Types, as in
`rule pair (node[string], int) = a:"a" => ($a, 1)`. The value has no range, like
a list value, and `x[i]` reads its element `i`.

```pego
type Entry = { Key (int, string) }
rule entry Entry = "a" => Entry{Key: (1, "a")}
rule pair = a:"a" b:"b" => ($a, $b)
```


A struct field value must fit the declared field Type. A value whose Type the checker does not
know well enough, such as a result of type `_`, or a list with elements of such a Type, must be
[cast](#casts) to it first. A cast checks the value against the Type when it runs, after the value is
evaluated:

- `int`, `string` and `bool` admit only values of that kind, and `nil` only `nil`;
- an optional Type `*T` admits `nil` and the values `T` admits;
- a join admits the values any of its alternatives admits;
- a struct Type admits only values of that Type;
- `node[T]` admits a node whose value `T` admits: `node[string]` every string
  node, `node[(A, B)]` a tuple node of two elements and `node[[]A]` a list
  node; `node[_]` admits every node, including every named node type;
- a named node type `N` admits only a node whose run-time type name is `N`.
- `(A, B, ...)` admits a tuple value of as many elements, each admitted by its
  Type;
- `[]T` admits a list value whose every element `T` admits; `[]_` and `[]any`
  admit every list;
- `error` admits error values from error recovery and `missing` values.

Conversion happens only in the two forms above. A field declared with a named node type `N`, and an element of
`[]N{...}`, accept only values assignable to `N`, so a `node[string]` value there is an error; write `N{value}`
instead.

A mismatch is a compile-time error such as
`cannot use node[string] as Ident in field Name of Decl`. A value whose Type the checker
does not know, such as a rule value of Type `_`, is rejected too, with a hint to
convert it explicitly: `cannot use _ as Ident in field Name of Decl: the type of the value is
not known; convert it with a cast, as in value.(Ident)`. A [cast](#casts) is the check that runs:
when it fails, that is an evaluation error. A constructor itself checks nothing when it runs, so
constructors cost nothing extra. Fields declared `_` accept every value.

Nothing else checks the fields of a constructor or the elements of a typed list when it
runs: the compile-time check is complete.

In the public source AST, a tuple literal is `TupleLit{Elems}`, and parentheses around
one action expression are retained as `TermGroup{Term}`. The group is semantically transparent: it affects
precedence and source formatting, but creates no value or evaluation scope. See
[Parenthesized Groups in the Public AST](grammar-files.md#parenthesized-groups-in-the-public-ast)
for comment and layout preservation.

## Field Access

`x.Name` reads field `Name` from node `x`. An unset field reads as `nil`; a
field that no member of the static type declares is a compile-time error. For
a join type, every member must declare the field, and the result type is the join of the field types
(a single type if they are equal). A field that only some members declare is a compile-time error naming the
members that lack it; cast the value to a member that has it instead. Struct fields and the node members below
count alike, so `x.Start` on `N | node[string]` is `int`, and `x.F` on `A | B` is the join of the types of `F`
in `A` and `B`, which chains: `x.F.G` accesses `G` on that join. With `type A = { F string; X int }` and
`type B = { F int }`, `x.X` on `A | B` is rejected with
`A | B has no field X in every member: B lacks it; cast the value to a member that has it, as in x.(T).X`,
and `x.(A).X` is accepted. A recovered value (`error` or `missing`) among the members does not need the
field; it reads as `nil`, so the result is optional.
Reading a declared struct whose type lacks that field is a runtime
action error. A string, tuple or list node has no fields other than the members
below; naming another field is a compile-time error. Accessing a field on `nil` is a
runtime error; an explicitly optional receiver type is rejected by type checking.

The built-in members `.Start`, `.End` and `.Value` are available on every node
that has a range, that is, a string, tuple or list node, and on a named node type, which has the members of its
underlying `node[U]`. The built-in functions `len` and `text` accept a named node type as they accept its
underlying node type. `.Start` and `.End` are
the `int` range of the node in the parse's selected position unit. `.Value` is the
value of the node, of type `T` for a `node[T]`: the string of a string node, the
tuple of elements of a tuple node and the list of elements of a list node, as in
`$xs.Value[0]`; an element is a value or `nil`. A struct value
has no range members: `.Start` on a struct is a compile-time error.

`x.$y` reads capture `y` of node `x`, such as an element of a repetition
(see [Captures](parser-expressions.md#capture)):
`map($items, (i) => i.$name)`. Every node type that `x` may have must capture
`y`, and the result type is the join of their capture types; an unmatched
capture reads as `nil`. A struct value has no captures, so `x.$y` on one is a
compile-time error, or a runtime action error for a dynamically typed
receiver. A captured sequence carries the captures inside it, so `$x.$y` for
`x:(y:a b)` reads the same node as `$y`.

Field names begin with an uppercase letter, so `x.y` with any other name, except
the `expected` and `message` fields of `missing`, is an
error in PEGO source that asks for `x.$y`. A grammar in JSON or a compiled
grammar may read a capture with `x.y`, which reads the same capture as `x.$y` at
run time. Its type check is looser: `x.y` on a join some of whose node types
lack `y` has an optional type.

## Casts

`x.(T)` is the value of `x`, asserted to be of Type `T`: its static type is
`T`, and it chains with field access, as in `$x.(Number).Value`. `T` is any
Type, written as in a declaration; a join is written in the parentheses
directly, as in `$x.(Number | String)`. A value that is not a `T` raises the
runtime action error `cannot cast <value type> to <T> at input position <N> (line <L>, column <C>)`,
checked as a field of Type `T` checks a dynamically typed value (see
[Values and Constructors](#values-and-constructors)), with the same execution
steps. The position is where the rule that runs the action or predicate started matching (the
position unit is the one used for positions); with source information
([source information](../runtime/nodes.md#source-information)) the message ends with `; cast at grammar position <line>:<column>`, the
position of the cast in the grammar, as in `cannot cast int to string at input position 2 (line 2,
column 1); cast at grammar position 3:29`. In a Predicate Action a failed cast makes the predicate
fail instead. Every backend reports the same text. A cast is a compile-time error unless the static type of `x` or one of
its alternatives is assignable to `T` or one of its alternatives, or the other
way around, so `$x.(int)` on a capture of type `node[string]` is rejected while
`$x.(node[string])` on a `node[_]` value and `$x.(Number)` on a `_` value are
accepted. A struct is not a node, so `$x.(Number)` on a `node[_]` value is
rejected.

A cast to a named node type `N` tests the run-time type name: `$x.(N)` succeeds for a node named `N` and raises
`cannot cast <value type> to N` otherwise. A cast to an unnamed `node[U]` admits every node whose underlying
value fits `U`, whatever its name. Statically, a cast is a compile-time error only when no value can have both the
static type of `x` and `T`; the check is made per alternative of a join or optional type. So `$x.(N)` on a
`node[_]` value is accepted, and `$x.(N)` on a `node[string]` value is accepted only when the underlying type of
`N` overlaps `node[string]`.

## Operators and Functions

This section defines operators and the complete set of built-in functions
available in Value and Predicate Actions. Function calls use positional
arguments. Call arguments, struct field values and ordinary binary operands are
evaluated left to right. Lambdas are permitted only as the function argument
of `foldl`, `foldr`, `map` or `filter`. Their parameters are variables: the
lambda body refers to a parameter by its bare name, and the name hides a
parser variable of the same name inside the body. A name followed by `(` or
`{` is still a function or a type, and `true`, `false` and `nil` cannot name a
parameter. `$name` in a lambda body always refers to a capture, and naming a
parameter there is an error.

### Operator Precedence and Types

The table is ordered from loosest to tightest binding. Binary operators at a
level are left-associative. Comparisons do not chain; use parentheses to make a
comparison result an operand of another comparison.

| Precedence | Operators | Accepted operands | Result |
|:--|:--|:--|:--|
| 1 | `\|\|` | `bool`, `bool` | `bool` |
| 2 | `&&` | `bool`, `bool` | `bool` |
| 3 | `==`, `!=`, `<`, `<=`, `>`, `>=` | Equality: any values; ordering: two `int` or two `string` values | `bool` |
| 4 | `+` | `int`, `int` or `string`, `string` | `int` or `string` |
| 4 | `-` | `int`, `int` | `int` |
| 5 | `*`, `/`, `%` | `int`, `int` | `int` |
| 6 | Unary `-` | `int` | `int` |
| 6 | Unary `!` | `bool` | `bool` |
| 7 | `.` field access; `.$` capture access; `.(T)` cast; `[i]` indexing; calls; parentheses | Node and field; any value and a Type; list and `int`; built-in and declared arguments; any expression | Field or call result; `T`; list element; grouped expression type |

All comparison operators share one precedence level, and the parser accepts
only one comparison operator at that level. Thus `a < b < c` is invalid;
`(a < b) == c` groups the first comparison before equality.

`+` adds integers or concatenates strings. `-`, `*`, `/` and `%` are integer
operators. Division truncates toward zero; remainder has the dividend's sign.
Dividing or taking a remainder by zero is a runtime action error. `&&` and `||`
evaluate their left operand first and short-circuit when it determines the
result, so the right operand may not be evaluated.

`xs[i]` is the element of the list `xs` at the zero-based index `i`, an
`int`; its static type is the list's element type, and a `nil` list has no
elements. Indexing applies equally to a tuple, whose elements are indexed in
order. An index outside the list is a runtime action error, as is a
dynamically typed index that is not an `int`. The `[` follows the indexed
value directly: `$xs[0]`, `f(x)[0]` and `xs[0][1]` index, while in a Rule
Expression `name [a-z]` with a space is a name and a character class, and
`name[a-z]` is an error.

`==` and `!=` compare integers, strings and booleans by value, `nil` with
`nil`, and nodes by identity. Values of different types compare unequal;
other runtime value kinds, including functions, do not compare equal. Because
nodes compare by identity, separate calls at the same position are not
necessarily equal: memoization determines whether a result node is reused.
Strings are ordered lexicographically. Comparisons return `bool`.

### Built-In Functions

| Function | Arity | Result |
|:--|:--:|:--|
| [`len`](#len) | 1 | `int` |
| [`text`](#text) | 1 | `string` |
| [`foldl`](#foldl), [`foldr`](#foldr) | 3 | Initial-accumulator type, widened to include callback results |
| [`map`](#map) | 2 | `[]T`, with `T` the type of the callback's result |
| [`list`](#list) | Any, including 0 | `[]T`, with `T` the join of the argument types |
| [`concat`](#concat) | Any, including 0 | A list of the join of its input element types |
| [`filter`](#filter) | 2 | A list with the input's element Type |

An optional list value contributes no elements when it is `nil`. An error
value produced by `#recover` also contributes no elements to list-consuming
functions. The checker accepts a join of list types and combines their
possible element types. A value of type `node[_]` is not a list for the checker:
cast it first, as in `$n.([]_)`.

#### `len`

`len(x)` accepts a string, node or `nil` and returns `int`. For a string, it
returns the length in the selected position unit. For a string node, it
returns the length of matched text in that unit. For a tuple or list node or a
list value, it returns the number of elements. For `nil`, it returns `0`.

The length of a node takes constant time: the matched text of a string node is
the input of its range, so its length is the end of the range minus its start,
which are already in the position unit. The length of a string value takes
time in proportion to the string where the unit differs from the encoding of
the string: in code points on every backend, and in bytes in TypeScript, whose
strings are UTF-16. In bytes in Go, it takes constant time.

#### `text`

`text(x)` accepts a string, node or `nil` and returns `string`. It returns a
string argument unchanged, returns `""` for `nil`, returns a string node's
matched text, and returns the input text covered by the range of a tuple or list
node. A list value and a struct have no range and are rejected as arguments.

#### `foldl`

`foldl(init, xs, f)` visits list elements from first to last. The callback has
two parameters and is called as `f(accumulator, element)`; its result becomes
the next accumulator. An empty input returns `init`. The accumulator type is widened to the join of the initial value type and the
callback result type; the checker checks the callback against that widened
type. If the callback result still does not fit after three widenings, the accumulator
type does not converge and the call is a compile-time error; cast the callback result.

```pego
foldl(0, $items, (sum, item) => sum + item.$value)
```

#### `foldr`

`foldr(init, xs, f)` visits list elements from last to first but still calls
the two-parameter callback as `f(accumulator, element)`. An empty input
returns `init`. The accumulator type is widened to the join of the initial
value type and the callback result type, as for `foldl`.

#### `map`

`map(xs, f)` calls the one-parameter callback `f(element)` in input order and
returns its results in a list value. The callback may return any value;
`nil` remains a `nil` element. `map` does not filter elements. A `nil` or
error-value list input is empty, and a `nil` element in a list is passed to the
callback as `nil`.

#### `filter`

`filter(xs, (item) => condition)` visits the elements of `xs` from left to
right. The lambda must take one argument and produce an actual Boolean:
`true` keeps the original element and `false` drops it. A nil element is
passed to the lambda as nil and is kept when the result is true. The result
preserves element order and node identity; elements are not cloned. A nil
input produces an empty list and does not call the lambda.

The result keeps the input list's static element Type. A predicate such as
`$item != nil` does not narrow an optional element Type. A statically known
non-Boolean result is a compile-time error; a dynamically non-Boolean result
is a runtime action error. Runtime errors follow the surrounding action:
inside a Predicate Action they fail that predicate and permit normal
backtracking; inside a Value Action they abort the parse.

#### `list`

`list(x, ...)` constructs a list value with its arguments in order. Each argument
is an element, and `nil` values remain elements. `list()` constructs
an empty list. Unlike `concat`, `list` does not flatten its arguments.

#### `concat`

`concat(xs, ...)` appends the child elements of each list-like argument, in
argument order and then element order, into one list value. A `nil` or error value
argument contributes no elements. `concat()` returns an empty list. The
checker combines the element types of all list arguments.

## Default Values

A parser compiled with **default values** (`pego.WithDefaultValue()`, `pego parse -default-value`,
`pego compile -default-value`, or `pego gen -default-value` for a generated parser) ignores the value
actions of the grammar. Before compilation, every value action (`=>` on a rule, and the actions of the
operands and operators of an [Operators Rule](operators-rule.md)) and the declared type of each rule
are dropped, so each rule produces the default value of its expression, as described in
[Nodes and Values](../runtime/nodes.md). Nothing is checked against the types that only actions would have made.
Type declarations stay in the grammar.

A [hidden rule](#hidden-rules) or an inline action that had an action still makes a node of its
own: its default value is the default action, not the splice of a hidden rule without an action. The
grammar is validated as written, with the actions the mode drops removed, so a defect is reported
where the grammar has it, with the numbering of its declarations and never in a rule that the
compiler generated.

Predicates still run, because they decide the match. The rules called inside a capture that a predicate
reads, and the rules that those call, keep their actions and declared types, so the values that
predicates see are the same as without default values; the parse result holds these action values for
those rules too. Recovery, `#stream` and captures behave as
without actions.

A parser that is compiled with default values stores the flag in its compiled form, and a loaded
compiled grammar keeps it.

## Types and Errors

Action expressions are type-checked when a grammar is compiled. The checker
reports invalid operators and function arguments, unknown or undeclared
fields, non-assignable constructor fields and rule results, and accesses that may dereference `nil`. See [Type Checking](type-checking.md)
for the complete assignability rules and diagnostics.
A rule whose declared Type is exactly a named node type `N` converts its value to `N` when the value is
convertible, as specified in [Named Node Types](types.md#named-node-types). An
[Operators Rule](operators-rule.md#named-node-types) declared with exactly `N` converts the value of each
prefix, infix and postfix operator it applies instead, at every nesting level, whether the operator has an
action or builds the tuple node of its operands; it converts no operand value.

A value that cannot be converted to `N` is a compile-time error, `rule r produces X, which cannot be converted to N`
(for an operator of an Operators Rule, `operatorsRule r produces X, which cannot be converted to N`), when its
type is known. A value whose type is not known well enough is rejected as well, with a hint to
cast it first, as in `N{value.(node[string])}`.

A Value Action result statically known to be a function is a compile-time
error.

A rule value whose Type the checker does not know, such as an action result or
a called rule's value of type `_`, is a compile-time error when the rule declares
a Type it does not fit: `rule term produces _, which is not assignable to Expr: the type of the
value is not known; convert it with a cast, as in value.(Expr)`. This applies to the value
of a rule with or without an action, of each Operators Rule operand line and of each operator
line with an action. The cast is the check that runs (see [Casts](#casts)); it
admits the values a field of that Type admits (see
[Values and Constructors](#values-and-constructors)). A runtime error while evaluating a Value Action—such as
division by zero,
an out-of-range index, or access to a concrete node that lacks a field—
causes the parse to fail. It is not a PEG syntax mismatch and is not backtracked
or handled by `#recover`. A runtime error while evaluating a Predicate Action
instead makes that predicate fail, so normal PEG backtracking applies; it does
not abort the entire parse. Neither runtime outcome is a type-checking error
when it depends on the runtime value.

## Value and Range Details

A rule's Value Action result is a value or `nil`. An `int`, `string` or `bool`
result, and such an element of a list that `map` or `list` makes, is carried
by a value node. A struct node created during
evaluation of a whole-rule Value Action and returned directly receives the
range matched by the rule body. A node returned from a capture or rule call
keeps its existing range. A newly constructed struct node initially has the
smallest range covering its node fields, or the rule body's range when it has
no node fields.
A list or tuple value built by an action (`list`, `map`, `concat`, `filter`,
`[]T{...}`) has no range: `.Start`, `.End` and `text` do not apply to it, and it
encodes as a plain array. A struct encodes without a range as well, and its range
members are not readable from an action.
