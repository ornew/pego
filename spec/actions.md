# Actions

PEGO has two executable action forms. A **Predicate Action** appears in braces
where a Rule Expression atom is expected. It evaluates without consuming
input or producing a value. A **Value Action** follows `=>` and computes a
rule's value from the values produced by its grammar body. An ordinary rule may
have a whole-rule Value Action after its complete expression:
`rule name [Type] = expression => term`. An Operators Rule has specialized
Value Actions on its operand and operator entries. A declaration-level action
is accepted only when it selects the default value behavior; omitting
it or writing the explicit `=> ...` default form preserves that behavior. Any
other declaration-level action on an Operators Rule is a compile-time error.

A Predicate Action block may contain several statements; its semantics are
defined in [Predicate Actions](predicates.md#predicate-actions).

Value Action blocks remain unsupported and are rejected during compilation;
their statements do not execute. Inline Value Action forms are also recognized
in source and rejected by compilation. Runtime execution of inline actions is
not implemented. PEGO has no rule-initialization action block.

```pego
// Source syntax only; compilation rejects this inline Value Action.
(b => { x := 1; x < 10; B{} })
```

```pego
type KV = { Key node[string]; Op node[string]; Value node[string] }
rule key_value KV = (k:[a-z]+ #atomic op:"=" v:[0-9]+ #atomic) => KV{Key: $k, Op: $op, Value: $v}
```

A Value Action runs after the complete body succeeds. Its result is a value: a node
(a string, tuple, list or struct value; see [Nodes](nodes.md#node-model)), an int, a string, a bool or `nil`, and must fit the declared or inferred rule type. It applies to the
whole body, including every alternative. Predicate Actions use the same value
operators and built-in functions described below, but their result controls
whether the predicate succeeds; see [Predicate Actions](predicates.md#predicate-actions).

## Whole-Rule Value Actions

A single-term Value Action follows `=>` after the complete rule expression.
When a grammar expression spans multiple lines, parenthesize that expression;
a line break inside a braced action block is local to the block.

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

An action can read parser variables visible where it is evaluated. Lookup uses
the nearest definition: a rule can read values defined by its callers, while
definitions in called rules are not visible to their callers. Reading a
variable that is undefined at the action's position is a runtime action error.
Variables are `int`, `string` or `bool`; definitions shadow rather than mutate
an existing value. Scope and rollback are specified in [Variables](predicates.md#variables).
Inside a lambda body, the lambda's parameters are variables too and are read
the same way; see [Operators and Functions](#operators-and-functions).

```pego
type Item = { Depth int; Name node[string] }
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


A struct field value whose Type the checker does not know, such as a variable,
a result of type `any`, or a list with elements of unknown Type, is checked
against the declared field Type when the constructor runs, after the value is
evaluated:

- `int`, `string` and `bool` admit only values of that kind, and `nil` only `nil`;
- an optional Type `*T` admits `nil` and the values `T` admits;
- a union admits the values any of its alternatives admits;
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

A mismatch aborts a Value Action with an error such as
`cannot use node[string] as Ident in field Name of Decl`; inside a Predicate Action it
makes the predicate fail. Each examined list element counts as one execution step for
[work limits](parsing.md#cancellation-and-work-limits). Fields declared `_` or `any` and values whose
Type the checker proved assignable are not checked, so constructors in fully
typed grammars cost nothing extra.

`NoTypeCheck` deliberately skips assignability checks for typed-list elements,
and instead checks every struct field whose declared
Type is not `_` or `any` at run time as described above, and every rule value as
described in [Types and Errors](#types-and-errors).

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
run time. Its type check is looser: `x.y` on a union some of whose node types
lack `y` has an optional type.

## Casts

`x.(T)` is the value of `x`, asserted to be of Type `T`: its static type is
`T`, and it chains with field access, as in `$x.(Number).Value`. `T` is any
Type, written as in a declaration; a union is written in the parentheses
directly, as in `$x.(Number | String)`. A value that is not a `T` raises the
runtime action error `cannot cast <value type> to <T>`, checked as a field of
Type `T` checks a dynamically typed value (see
[Values and Constructors](#values-and-constructors)), with the same execution
steps. A cast is a compile-time error unless the static type of `x` or one of
its alternatives is assignable to `T` or one of its alternatives, or the other
way around, so `$x.(int)` on a capture of type `node[string]` is rejected while
`$x.(node[string])` on a `node[_]` value and `$x.(Number)` on a `_` value are
accepted. A struct is not a node, so `$x.(Number)` on a `node[_]` value is
rejected.

A cast to a named node type `N` tests the run-time type name: `$x.(N)` succeeds for a node named `N` and raises
`cannot cast <value type> to N` otherwise. A cast to an unnamed `node[U]` admits every node whose underlying
value fits `U`, whatever its name. Statically, a cast is a compile-time error only when no value can have both the
static type of `x` and `T`; the check is made per alternative of a union or optional type. So `$x.(N)` on a
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
elements. An index outside the list is a runtime action error, as is a
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
| [`list`](#list) | Any, including 0 | `[]T`, with `T` the union of the argument types |
| [`concat`](#concat) | Any, including 0 | A list of the union of its input element types |
| [`filter`](#filter) | 2 | A list with the input's element Type |

An optional list value contributes no elements when it is `nil`. An error
value produced by `#recover` also contributes no elements to list-consuming
functions. The checker accepts a union of list types and combines their
possible element types. At runtime the functions consume the elements of a
list node, list value or tuple node, so a value of type `node[_]` can supply
that node's elements.

#### `len`

`len(x)` accepts a string, node or `nil` and returns `int`. For a string, it
returns the length in the selected position unit. For a string node, it
returns the length of matched text in that unit. For a tuple or list node or a
list value, it returns the number of elements. For `nil`, it returns `0`.

#### `text`

`text(x)` accepts a string, node or `nil` and returns `string`. It returns a
string argument unchanged, returns `""` for `nil`, returns a string node's
matched text, and returns the input text covered by the range of a tuple or list
node. A list value and a struct have no range and are rejected as arguments.

#### `foldl`

`foldl(init, xs, f)` visits list elements from first to last. The callback has
two parameters and is called as `f(accumulator, element)`; its result becomes
the next accumulator. An empty input returns `init`. The accumulator type is widened to the union of the initial value type and the
callback result type; the checker checks the callback against that widened
type.

```pego
foldl(0, $items, (sum, item) => sum + item.$value)
```

#### `foldr`

`foldr(init, xs, f)` visits list elements from last to first but still calls
the two-parameter callback as `f(accumulator, element)`. An empty input
returns `init`. The accumulator type is widened to the union of the initial
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

## Types and Errors

Action expressions are type-checked when a grammar is compiled. The checker
reports invalid operators and function arguments, unknown or undeclared
fields, non-assignable constructor fields and rule results, and accesses that may dereference `nil`. See [Type Checking](type-checking.md)
for the complete assignability rules and diagnostics.
A rule whose declared Type is exactly a named node type `N` converts its value to `N` when the value is
convertible, as specified in [Named Node Types](types.md#named-node-types).

A value that cannot be converted to `N` is a compile-time error, `rule r produces X, which cannot be converted to N`,
when its type is known. Only under `NoTypeCheck` is an uncertain value checked at run time instead.

A Value Action result statically known to be a function is a compile-time
error.

A rule value whose Type the checker does not know, such as an action result or
a called rule's value of type `any`, is checked against the rule's declared Type
when the rule produces it: after its action, or for a rule without an action,
after its value is labeled. The value of each Operators Rule operand line, and
of each operator line with an action, is checked when the line produces it.
The check admits the values a field of that Type admits (see
[Values and Constructors](#values-and-constructors)), with the same execution
steps. A mismatch causes the parse to fail with an error such as
`cannot use List as Expr in result of rule term`, including when the rule was
called for a predicate's value. A runtime error while evaluating a Value Action—such as
division by zero,
undefined variable lookup, or access to a concrete node that lacks a field—
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
