# Actions

An action replaces the tree of a rule with a value that you compute from its captures, usually a typed node. This guide
shows how to write actions, the built-in functions and lambdas, how positions of constructed nodes are assigned, and
recipes for lists and left- and right-associative chains. The grammars run like those in [Shaping
Trees](shaping-trees.md), which also explains how to run them. The exact rules are in the [Actions](../../spec/language/actions.md)
chapter. For operator expressions (`foldl` chains versus Operators Rule Declaration Statements) see [Operators
Rule](operators-rule.md).

An action is an expression after `=>` inside a rule body that computes the value of the rule from what the body matched:

```pego
rule <name> [<type>] = <rule expression> => <action expression>
```

The action covers the whole right-hand side, and its result may be a node, an `int`, a `string`, a `bool` or `nil`.

An action in parentheses, `(expression => action)`, covers only that group. It behaves like a rule written at that
position: the group is one element of the enclosing sequence, its value is the result of the action, and the action sees
only the captures made inside the parentheses. The captures of the enclosing rule are not visible there, and the ones made
inside are not visible outside.

```pego
// inline-action.pego
type Pair = { Left node[string]; Right node[string] }
@start
rule pairs = (l:[a-z] r:[a-z] => Pair{Left: $l, Right: $r})+
```

```bash
pego parse -g inline-action.pego -s pairs -f sexpr -i 'abcd'
```

```text
[Pair{Left: "a", Right: "b"} Pair{Left: "c", Right: "d"}]
```

An inline action cannot appear in a lookahead (`&`, `!`) or a discard (`-`), which make no value. The braced
form `(expression => { ... })` is not supported.

The rule behind an inline action is hidden: a rule whose name begins with `_`. You can write such a rule yourself to
share a part of a grammar without giving it a node. A hidden rule without an action makes no node, and a call of it
contributes what its expression would if it were written in its place as a group. A hidden rule is not an entry point:
it cannot be the start rule or be parsed by name. [Shaping Trees](shaping-trees.md#hidden-rules) shows the effect on
the tree.

```pego
rule pair = "(" _items ")"
rule _items = [a-z] ("," [a-z])*
```

Here `pair` has the tuple of `"("`, the first letter, the list of the rest and `")"`, as if `_items` were written in
`pair`. The captures made in `_items` are not visible in `pair`.

## Referring to Captures

`$label` refers to the value captured with `label:` in this rule. Only labeled elements can be used in an action:
label each element the action needs, as `name:name` and `value:value` in the rule of [Structs and Joins](types.md#structs-and-joins).

Captures are visible only in the rule that made them, not in the rules it calls or that call it. A reference to an
undefined capture is a compile-time error (`undefined capture $z`).

## Building Structs and Reading Members

`T{Field: value, ...}` builds a struct. Field values are checked against the declared field types, so typos and
mismatches are compile-time errors:

```pego
// field-errors.pego
type Assign = { Name node[string]; Value node[string] }

@start
rule a Assign = k:name "=" v:name => Assign{Name: $k, Value: $v}
rule b Assign = k:name "=" v:number => Assign{Name: $k, Valu: $v}
rule name = [a-z]+ #atomic
rule number = [0-9]+ #atomic
```

```bash
pego parse -g field-errors.pego -s a -i 'a=b'
```

```text
pego: field-errors.pego:6:57: Assign has no field Valu
```

## Typed Lists

A typed list literal states the element type directly rather than inferring
it from the evaluated values. Every item must have a known type assignable
to the target, including items in nested lists. Nil is accepted only for an optional element
type; an empty typed list is still a non-nil list.

```pego
type NameNode = { Name string }
rule name NameNode = v1:[a-z]+ => NameNode{Name: text($v1)}
rule names []NameNode = a:name "," b:name => []NameNode{$a, $b}
rule emptyNames []NameNode = "" => []NameNode{}
```

Struct constructors use named fields and commas. Struct type definition
fields use semicolons or hard newlines. Lists of primitive values such
as `[]int` are not supported.

`x.Field` reads a field of a struct; `x` may be a capture or the result of another access. A captured element or a
rule's value is a node, and a node has the members `.Start` and `.End` (integers) and `.Value`, the value that the node
holds: the matched string of a string node, or the tuple or list of the others. `x[i]` reads the element `i` of a tuple
or list, and of the value of a node. Reading a member of a value that may be `nil` is an error, so
`n:name?` followed by `$n.Start` does not compile. When `x` has a join type the field is allowed if at least one
member declares it.

Struct fields can hold integers, strings and booleans too, which is how you attach derived data to a node:

```pego
// derived.pego
type Word = {
    Text   string
    Length int
    Long   bool
    From   int
    To     int
}

@start
rule main Word = ws w:word ws $$ => $w
rule word Word = n:[a-z]+ #atomic => Word{Text: text($n), Length: len($n), Long: len($n) > 4, From: $n.Start, To: $n.End}
rule ws = " "*
```

```bash
pego parse -g derived.pego -f sexpr -i '  hello '
```

```text
Word{From: 2, Length: 5, Long: true, Text: "hello", To: 7}
```

## Tuples

A tuple literal builds a tuple value of its elements: `()` is the unit, `(x,)` has one element and
`(x, y)` two. It follows the form of a tuple type, and parentheses around one expression without a
comma stay a group. The elements may be nodes, ints, strings, bools or `nil`, and the literal fits
a declared tuple type of the same arity elementwise.

```pego
type Entry = { Key (int, string) }
rule entry Entry = "a" => Entry{Key: (1, "a")}
rule pair (node[string], node[string]) = a:"a" b:"b" => ($a, $b)
```

## Built-In Functions and Lambdas

| Function | Result |
|:--|:--|
| `len(x)` | The length of a string or a string node in positions; the number of elements of a tuple or list node; `0` for `nil` |
| `text(x)` | The input text that the node covers, whatever its value; `""` for `nil` |
| `list(a, b, ...)` | A list of the arguments |
| `concat(l1, l2, ...)` | The concatenation of lists; a `nil` argument counts as an empty list |
| `map(list, (x) => ...)` | The list of the results of applying the function to each element |
| `filter(list, (x) => bool)` | Keeps the original elements whose predicate returns `true` |
| `foldl(init, list, (acc, x) => ...)` | Folds the list from the left, starting with `init` |
| `foldr(init, list, (acc, x) => ...)` | Folds the list from the right, starting with `init` |

The function arguments are **lambdas**. They can only be written as arguments of `map`, `filter`, `foldl` and `foldr`, and
inside them the parameters are variables, referred to by name without `$`: `acc`, `x`. A lambda can also read the captures
of the enclosing rule with `$`. The function passed to `map` may produce any value and `filter`'s predicate a
Boolean. The [specification](../../spec/language/actions.md#built-in-functions) defines each function exactly: element types, `nil`
and `Error` arguments, evaluation order and what happens when a callback fails.

```pego
// builtins.pego
type Call = {
    Name  string
    Arity int
    Raw   string
}

@start
rule call Call = n:[a-z]+ #atomic "(" args:(a:[a-z]+ #atomic -","?)* ")" => Call{Name: text($n), Arity: len($args), Raw: text($args)}
```

```bash
pego parse -g builtins.pego -s call -f sexpr -i 'f(a,b,c)'
```

```text
Call{Arity: 3, Name: "f", Raw: "a,b,c"}
```

`text($args)` is the input text covered by the node, including the commas that were discarded from the tree: ranges are
properties of the input, not of the elements. Here is a lambda that reads a capture of the enclosing rule, `$k`, to
pair the key with every item:

```pego
// outer-capture.pego
type Pair = { A node[string]; B node[string] }
type Pairs = { Items []Pair }

@start
rule pairs Pairs = k:ident ":" xs:(x:ident -","?)* => Pairs{Items: map($xs, (r) => Pair{A: $k, B: r.$x})}
rule ident = [a-z]+ #atomic
```

```bash
pego parse -g outer-capture.pego -s pairs -f sexpr -i 'k:a,b'
```

```text
Pairs{Items: [Pair{A: "k", B: "a"} Pair{A: "k", B: "b"}]}
```

Action expressions also support the arithmetic, comparison and boolean operators listed in the
[Actions chapter](../../spec/language/actions.md#operators-and-functions). There is no conditional expression, so branch on the grammar side
(separate rules or alternatives) rather than inside actions.

## Positions

Every node has a range `[start, end)`, which `.Start` and `.End` read. Nodes that rules build without an action cover what their expression matched. A constructed struct has one of two ranges:

- If it is the **result of the action**, it covers what the rule matched.
- If it is created elsewhere, for example inside a `foldl` lambda, it covers the smallest range that includes the nodes
  in its fields (or the rule's match if it has none).

Lists made by `list`, `map` and `concat` cover their elements. This is what makes the nested nodes of a left fold span
exactly their operands, which the Go program below prints.

Positions are counted in **code points** by default, or in UTF-8 **bytes** with `-unit bytes`
(`pego.WithUnit(pego.Bytes)` in Go). Matching is always by code point; the unit only changes how positions and `len`
are measured. The value `v` below starts at position 3 in code points and at 7 in bytes:

```pego
// unit.pego
rule name = [^=]+ #atomic
@start
rule kv = k:name "=" v:name
```

```bash
pego parse -g unit.pego -s kv -i 'あい=うえお' | grep -B2 '"うえお"'
pego parse -g unit.pego -s kv -i 'あい=うえお' -unit bytes | grep -B2 '"うえお"'
```

```text
      "start": 3,
      "end": 6,
      "value": "うえお"
      "start": 7,
      "end": 16,
      "value": "うえお"
```

## Recipes

### Left-Associative Trees With `foldl`

A repetition gives a flat list; a fold turns it into the nested tree. Capture the first operand, capture the repeated
`(operator, operand)` pairs, and fold from the left, so that `1 - 2 + 3` becomes `(1 - 2) + 3`:

```pego
// sum.pego
type Bin = {
    Left  Expr
    Op    node[string]
    Right Expr
}
type Expr ~ node[string] | Bin

@start
rule main Expr = e:sum $$ => $e
rule sum Expr = first:num rest:(-ws op:op -ws n:num)* => foldl($first, $rest, (acc, i) => Bin{Left: acc, Op: i.$op, Right: i.$n})
rule num = [0-9]+ #atomic
rule op = "+" / "-"
rule ws = " "*
```

```bash
pego parse -g sum.pego -f sexpr -i '1 - 2 + 3'
pego parse -g sum.pego -f sexpr -i '7'
```

```text
Bin{Left: Bin{Left: "1", Op: "-", Right: "2"}, Op: "+", Right: "3"}
"7"
```

Notes: the accumulator starts as a node and becomes a `Bin`, so its type is widened to the join of both, and the
declared `Expr ~ node[string] | Bin` must contain both. With a single operand the repetition is empty and the fold returns the
initial value unchanged, which is why `7` stays a bare node. For several precedence levels you would write one such
rule per level; for that case also look at [Operators Rule](operators-rule.md).

### Right-Associative Trees With `foldr`

`a ^ b ^ c` must be `a ^ (b ^ c)`. Capture the pairs *(operand, operator)* and the last operand separately, and fold
from the right, starting with the last operand:

```pego
// pow.pego
type Bin = {
    Left  Expr
    Op    node[string]
    Right Expr
}
type Expr ~ node[string] | Bin

@start
rule pow Expr = pairs:(l:num -ws op:op -ws)* last:num => foldr($last, $pairs, (acc, i) => Bin{Left: i.$l, Op: i.$op, Right: acc})
rule num = [0-9]+ #atomic
rule op = "^"
rule ws = " "*
```

```bash
pego parse -g pow.pego -s pow -f sexpr -i '2 ^ 3 ^ 4'
pego parse -g pow.pego -s pow -f sexpr -i '2'
```

```text
Bin{Left: "2", Op: "^", Right: Bin{Left: "3", Op: "^", Right: "4"}}
"2"
```

The repetition is greedy and does not backtrack, but it is still correct here: it consumes `2 ^` and `3 ^`, then
`4` fails to be followed by `^`, so the repetition stops and `last` matches `4`.

### Member Chains With a Join Accumulator

The same fold builds `a.b.c`. The accumulator is first a node and then a `Member`, so declare the join and use it
for the rule type and the field:

```pego
// path.pego
type Member = {
    X    Path
    Name node[string]
}
type Path ~ node[string] | Member

@start
rule path Path = first:ident rest:(-"." n:ident)* => foldl($first, $rest, (acc, i) => Member{X: acc, Name: i.$n})
rule ident = [a-z]+ #atomic
```

```bash
pego parse -g path.pego -s path -f sexpr -i 'a.b.c'
```

```text
Member{Name: "c", X: Member{Name: "b", X: "a"}}
```

### Flattening Separated Lists

`first (sep item)*` yields a tuple of the first item and a list of tuples. To get one flat list of items, capture the repeated part and
rebuild a list: `list($first)` makes a one-element list, `map` extracts the item from every iteration, and `concat`
joins them. A list that is optional (`args?`) works with `concat` too, because a `nil` argument counts as empty:

```pego
// args.pego
type Call = {
    Fn   node[string]
    Args []node[string]
}

@start(default)
rule call Call = fn:ident -ws "(" -ws args:args? -ws ")" => Call{Fn: $fn, Args: concat($args)}
rule args = first:ident rest:(-ws "," -ws x:ident)* => concat(list($first), map($rest, (r) => r.$x))
@start
rule args_cst = ident (-ws "," -ws ident)*
rule ident = [a-z]+ #atomic
rule ws = " "*
```

```bash
pego parse -g args.pego -s call -f sexpr -i 'f(a, b, c)'
pego parse -g args.pego -s call -f sexpr -i 'f()'
pego parse -g args.pego -s args_cst -f sexpr -i 'a, b, c'
```

```text
Call{Args: ["a" "b" "c"], Fn: "f"}
Call{Args: [], Fn: "f"}
("a" [("," "b") ("," "c")])
```

`args_cst` shows what you would otherwise get: the first item followed by a list of `("," item)` tuples. Writing
`Args: $args` directly does not compile, because `$args` is optional and the field is a plain list:

```pego
// args-optional.pego
type Call = { Fn node[string]; Args []node[string] }
@start
rule call Call = fn:ident "(" args:args? ")" => Call{Fn: $fn, Args: $args}
rule args = first:ident rest:(-"," x:ident)* => concat(list($first), map($rest, (r) => r.$x))
rule ident = [a-z]+ #atomic
```

```bash
pego parse -g args-optional.pego -s call -i 'f(a)'
```

```text
pego: args-optional.pego:4:63: cannot use *[]ident as []node[string] in field Args of Call
```

### Keeping Only the AST You Need

The concrete syntax tree of a realistic language is full of trivia: whitespace, comments, separators and keywords. The AST should
contain the data. Four habits get you there:

1. **Name what you need, discard the rest.** Capture the values the action uses and put `-` in front of everything
   else, including whitespace rules that also skip comments.
2. **Discard inside captured repetitions**, not only around them (see [Captures](shaping-trees.md#captures)).
3. **Pick fields with `map`** when each iteration carries more than the value you want.
4. **Unwrap the start rule** with `e:rule $$ => $e`.

```pego
// config.pego
type Entry = {
    Key   node[string]
    Value node[string]
}
type Config = { Entries []Entry }

@start
rule main Config = es:entries ws $$ => Config{Entries: $es}
rule entries = es:(-ws e:entry)* => map($es, (x) => x.$e)
rule entry Entry = k:key -ws -"=" -ws v:value -ws -";" => Entry{Key: $k, Value: $v}
rule key = [a-z_]+ #atomic
rule value = [a-z0-9.]+ #atomic
rule ws = ([ \t\r\n] / "#" [^\n]*)*
```

```bash
pego parse -g config.pego -f sexpr -i $'# web server\nhost = example.com;  # comment\nport = 8080;\n'
```

```text
Config{Entries: [Entry{Key: "host", Value: "example.com"} Entry{Key: "port", Value: "8080"}]}
```

Comments, whitespace, `=` and `;` are all gone; only the entries remain. If you also need the comments (for a
formatter, say), capture them instead of discarding them.

## Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `T has no field F` | A typo, or the struct does not declare the field |
| `cannot access .F of *T` | The value may be `nil` (for example `n:name?`). Read the field in a rule where the value is always present |
| `an inline action is not allowed in a lookahead or a discard` | The value of the group is not used there; move the action to a rule or out of the `&`, `!` or `-` |
| `undefined capture $z` | The capture is not made in this rule; captures are not visible across rules |
| `$value is only available in the whole-rule action of an Operators Rule` | The folded value exists only in the action written after the rule's closing `}`; an operator action reads `$lhs` or `$rhs` |
| `capture name value is reserved for the whole-rule action of an Operators Rule` | The name `value` is the folded value of an Operators Rule; rename the capture in the operand or operator line |

## Related Topics

- [Declaring Types](types.md): the types that actions construct.
- [Shaping Trees](shaping-trees.md): captures, atomic and discard.
- [Context-Sensitive Parsing](context-sensitive.md): predicates, which read captures too.
