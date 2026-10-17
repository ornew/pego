# Actions

PEGO has two executable action forms. A **Predicate Action** appears in braces
where a Rule Expression atom is expected. It evaluates without consuming
input or producing a value. A **Value Action** follows `=>` and computes a
rule's value from the values produced by its grammar body. An ordinary rule may
have a whole-rule Value Action after its complete expression:
`rule name [Type] = expression => term`. An Operators Rule has specialized
Value Actions on its operand and operator entries. A declaration-level action
is accepted only when it selects the existing default CST behavior; omitting
it or writing the explicit `=> ...` default form preserves that behavior. Any
other declaration-level action on an Operators Rule is a compile-time error.

A Predicate Action block may contain multiple statements separated by
semicolons or hard newlines. Its statements execute in source order as adjacent
Predicate Actions would: an assignment updates the current invocation's
variable environment, and a term succeeds unless it evaluates to false or
errors. Evaluation stops at the first failure. The block creates no variable,
capture, rollback, memoization or recovery scope. Empty blocks are compile-time
errors. In a sequence the statements become adjacent Predicate Actions; in a
single-expression position they retain the grouped adjacent-sequence behavior
of the existing CST.

Value Action blocks remain unsupported and are rejected during compilation;
their statements do not execute. Inline Value Action forms are also recognized
in source and rejected by compilation. Runtime execution of inline actions is
not implemented. PEGO has no rule-initialization action block.

```pego
// Source syntax only; compilation rejects this inline Value Action.
(b => { x := 1; x < 10; B{} })
```

```pego
type KV = { Key Match; Op Match; Value Match }
rule key_value KV = (k:[a-z]+ op:"=" v:[0-9]+) => KV{Key: $k, Op: $op, Value: $v}
```

A Value Action runs after the complete body succeeds. Its result must be a node
or `nil` and must fit the declared or inferred rule type. It applies to the
whole body, including every alternative. Predicate Actions use the same value
operators and built-in functions described below, but their result controls
whether the predicate succeeds; see [Predicate Actions](predicates.md#predicate-actions).

## Predicate Action Blocks

A nonempty block is shorthand for adjacent Predicate Actions at the same
Rule Expression position:

```pego
rule item = { count := 1; count == 1 } "a"
```

This has the same behavior as:

```pego
rule item = { count := 1 } { count == 1 } "a"
```

Assignments and tests run left to right, stopping at the first false result or
evaluation error. Assignment bindings are visible to later statements and
follow the same rollback behavior as adjacent Predicate Actions. A block does
not introduce a variable or capture scope, consume input, or add a rule,
memoization or recovery boundary. Within a sequence it expands to adjacent
actions; in a single-expression position it behaves like the grouped adjacent
sequence, including the existing CST result for a sequence of valueless
predicates. Empty blocks are compile-time errors.

## Whole-Rule Value Actions

A single-term Value Action follows `=>` after the complete rule expression.
When a grammar expression spans multiple lines, parenthesize that expression;
a line break inside a braced action block is local to the block.

## Capture References

`$label` refers to a named capture visible to the action's expression scope.
Captures are referred to by name only: an element is labeled (`label:expr`) to
be used in an action. A `$` followed by a digit, the positional reference of
earlier versions, is a syntax error; `pego fix` labels the element it referred
to and rewrites the reference.

Captures are visible only in their enclosing grammar-expression scope. A
capture that did not match has value `nil`.

## Variable References

An action can read parser variables visible where it is evaluated. Lookup uses
the nearest definition: a rule can read values defined by its callers, while
definitions in called rules are not visible to their callers. Reading a
variable that is undefined at the action's position is a runtime action error.
Variables are `int`, `string` or `bool`; definitions shadow rather than mutate
an existing value. Scope and rollback are specified in [Variables](predicates.md#variables).
Inside a lambda body, the lambda's parameters are variables too and are read
the same way; see [Operators and Functions](#operators-and-functions).

```pego
type Item = { Depth int; Name Match }
rule item Item = {depth := 1} n:name => Item{Depth: depth, Name: $n}
```

## Values and Constructors

Action Expressions operate on nodes, integers, strings, booleans, `nil`, and
restricted lambda functions. String and integer literals use the same escapes
and decimal syntax as grammar literals. `T{Field: value, ...}` constructs a
node of struct type `T`; unset fields read as `nil`. A typed node-list literal
`[]T{value, ...}` constructs a `List` whose element Type is explicitly `T`. A
terminal Type can be constructed positionally as `T{text}`; the sole argument
must be a string-valued Action Expression. These forms never stringify or filter
values implicitly.

```pego
type NameNode = { Name string }
rule name NameNode = v1:[a-z]+ => NameNode{Name: text($v1)}

rule twoNames []NameNode = a:name "," b:name => []NameNode{$a, $b}
rule noNames []NameNode = "" => []NameNode{}

type Identifier terminal
rule identifier Identifier = [a-z]+
rule normalized Identifier = n:identifier => Identifier{text($n)}
```

A typed node-list literal has the form `[]T{value, ...}`. Every element must
have a known static Type assignable to `T`; nested literals are checked
recursively, and an unknown element Type is an error even when the target is a
concrete node list. An empty literal such as `[]NameNode{}` has Type
`[]NameNode`. Nil is accepted only when the element Type is optional, as in
`[]*NameNode{nil}`. Elements are evaluated once, left to right; no element is
dropped implicitly. Primitive-valued lists such as `[]int` are unsupported.

A terminal constructor has the form `T{stringValue}` when `T` resolves to a
terminal Type. It requires exactly one string-valued argument. Wrong arity or a
statically known non-string argument is a compile-time error; `T{}` remains an empty struct
constructor syntactically and is rejected if `T` resolves to a terminal. The
provided string becomes the terminal text without being parsed again. Its
source range follows the ordinary newly constructed node rule.

A dynamically typed `any` argument uses the ordinary runtime string check.
A mismatch aborts a Value Action; inside a Predicate Action it makes the
predicate fail and permits the existing backtracking behavior.

A struct field value whose Type the checker does not know, such as a variable,
a result of type `any`, or a list with elements of unknown Type, is checked
against the declared field Type when the constructor runs, after the value is
evaluated:

- `int`, `string` and `bool` admit only values of that kind, and `nil` only `nil`;
- an optional Type `*T` admits `nil` and the values `T` admits;
- a union admits the values any of its alternatives admits;
- a struct, terminal or CST node Type admits only nodes of that Type, `node`
  admits every node, and `terminal` every terminal;
- `[]T` admits a `List` whose every element `T` admits;
- `Error` and `Missing` nodes from error recovery stand for every node Type
  and, as an empty list, for every list Type.

A mismatch aborts a Value Action with an error such as
`cannot use Match as Ident in field Name of Decl`; inside a Predicate Action it
makes the predicate fail. Each examined list element counts as one execution step for
[work limits](parsing.md#cancellation-and-work-limits). Fields declared `any` and values whose
Type the checker proved assignable are not checked, so constructors in fully
typed grammars cost nothing extra.

`NoTypeCheck` deliberately skips assignability checks for typed-list elements
and terminal arguments, and instead checks every struct field whose declared
Type is not `any` at run time as described above, and every rule value as
described in [Types and Errors](#types-and-errors). It does not add runtime
support for primitive-valued lists.

In the public source AST, parentheses around an action expression are retained
as `TermGroup{Term}`. The group is semantically transparent: it affects
precedence and source formatting, but creates no value or evaluation scope. See
[Parenthesized Groups in the Public AST](grammar-files.md#parenthesized-groups-in-the-public-ast)
for comment and layout preservation.

## Field Access

`x.Name` reads field `Name` from node `x`. An unset field reads as `nil`; a
field that no member of the static type declares is a compile-time error. For
a union type, a field declared by only some members has an optional result
type. Reading a declared struct whose type lacks that field is a runtime
action error. Other node kinds, such as `Match`, `List` or a capture record,
return `nil` for absent fields. Accessing a field on `nil` is a runtime error;
an explicitly optional receiver type is rejected by type checking.

The built-in fields `startPos`, `endPos` and `children` are available on every
node. Positions use the parse's selected unit. `children` is the node's child
list; it is empty for node kinds without children.

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
parameter there is an error that `pego fix` rewrites.

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
| 7 | `.` field access; calls; parentheses | Node and field; built-in and declared arguments; any expression | Field or call result; grouped expression type |

All comparison operators share one precedence level, and the parser accepts
only one comparison operator at that level. Thus `a < b < c` is invalid;
`(a < b) == c` groups the first comparison before equality.

`+` adds integers or concatenates strings. `-`, `*`, `/` and `%` are integer
operators. Division truncates toward zero; remainder has the dividend's sign.
Dividing or taking a remainder by zero is a runtime action error. `&&` and `||`
evaluate their left operand first and short-circuit when it determines the
result, so the right operand may not be evaluated.

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
| [`map`](#map) | 2 | `[]T`, with `T` a node or `nil` |
| [`list`](#list) | Any, including 0 | `[]T`, with each argument a node or `nil` |
| [`concat`](#concat) | Any, including 0 | A list of the union of its input element types |
| [`filter`](#filter) | 2 | A list with the input's element Type |

An optional list value contributes no elements when it is `nil`. An `Error`
node produced by `#recover` also contributes no elements to list-consuming
functions. The checker accepts a union of list types and combines their
possible element types. At runtime the functions consume the child values of a
list-like node; a broad `node` type can therefore supply that node's children.

#### len

`len(x)` accepts a string, node or `nil` and returns `int`. For a string, it
returns the length in the selected position unit. For a terminal node, it
returns the length of matched text in that unit. For another node, it returns
the number of children. For `nil`, it returns `0`.

#### text

`text(x)` accepts a string, node or `nil` and returns `string`. It returns a
string argument unchanged, returns `""` for `nil`, returns a terminal node's
matched text, and returns the input text covered by a nonterminal node's range.

#### foldl

`foldl(init, xs, f)` visits list elements from first to last. The callback has
two parameters and is called as `f(accumulator, element)`; its result becomes
the next accumulator. An empty input returns `init`. The accumulator type is widened to the union of the initial value type and the
callback result type; the checker checks the callback against that widened
type.

```pego
foldl(0, $items, (sum, item) => sum + item.value)
```

#### foldr

`foldr(init, xs, f)` visits list elements from last to first but still calls
the two-parameter callback as `f(accumulator, element)`. An empty input
returns `init`. The accumulator type is widened to the union of the initial
value type and the callback result type, as for `foldl`.

#### map

`map(xs, f)` calls the one-parameter callback `f(element)` in input order and
returns its results in a `List`. The callback must return a node or `nil`;
`nil` remains a `nil` element. `map` does not filter elements. A `nil` or
`Error` list input is empty, and a `nil` element in a list is passed to the
callback as `nil`.

#### filter

`filter(xs, (item) => condition)` visits the elements of `xs` from left to
right. The lambda must take one argument and produce an actual Boolean:
`true` keeps the original element and `false` drops it. A nil element is
passed to the lambda as nil and is kept when the result is true. The result
preserves element order and node identity; elements are not cloned. A nil
input produces an empty List and does not call the lambda.

The result keeps the input list's static element Type. A predicate such as
`$item != nil` does not narrow an optional element Type. A statically known
non-Boolean result is a compile-time error; a dynamically non-Boolean result
is a runtime action error. Runtime errors follow the surrounding action:
inside a Predicate Action they fail that predicate and permit normal
backtracking; inside a Value Action they abort the parse.

#### list

`list(x, ...)` constructs a `List` with its arguments in order. Each argument
must be a node or `nil`, and `nil` values remain elements. `list()` constructs
an empty `List`. Unlike `concat`, `list` does not flatten its arguments.

#### concat

`concat(xs, ...)` appends the child elements of each list-like argument, in
argument order and then element order, into one `List`. A `nil` or `Error`
argument contributes no elements. `concat()` returns an empty `List`. The
checker combines the element types of all list arguments.

## Types and Errors

Action expressions are type-checked when a grammar is compiled. The checker
reports invalid operators and function arguments, unknown or undeclared
fields, non-assignable constructor fields and rule results, and accesses that may dereference `nil`. See [Type Checking](type-checking.md)
for the complete assignability rules and diagnostics.

A Value Action result statically known not to be a node or `nil` is a
compile-time error. A result whose type is not known until evaluation is also
checked at runtime.

A rule value whose Type the checker does not know, such as an action result or
a called rule's value of type `any`, is checked against the rule's declared Type
when the rule produces it: after its action, or for a rule without an action,
after its value is labeled. The value of each Operators Rule operand line, and
of each operator line with an action, is checked when the line produces it.
The check admits the values a field of that Type admits (see
[Values and Constructors](#values-and-constructors)), with the same execution
steps. A mismatch causes the parse to fail with an error such as
`cannot use List as Expr in result of rule term`, including when the rule was
called for a predicate's value. A rule declared with a terminal Type and without
an action converts its value instead, and neither it nor, for an Operators
Rule, its lines are checked. A runtime error while evaluating a Value Action—such as
division by zero,
undefined variable lookup, or access to a concrete node that lacks a field—
causes the parse to fail. It is not a PEG syntax mismatch and is not backtracked
or handled by `#recover`. A runtime error while evaluating a Predicate Action
instead makes that predicate fail, so normal PEG backtracking applies; it does
not abort the entire parse. Neither runtime outcome is a type-checking error
when it depends on the runtime value.

## Value and Range Details

A rule's Value Action result must be a node or `nil`. A struct node created during
evaluation of a whole-rule Value Action and returned directly receives the
range matched by the rule body. A node returned from a capture or rule call
keeps its existing range. A newly constructed struct node initially has the
smallest range covering its node fields, or the rule body's range when it has
no node fields.
A list has the smallest range covering its non-nil elements; a list with no
non-nil elements has an empty range at the start of the rule match.
