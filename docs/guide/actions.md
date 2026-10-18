# Actions

An action replaces the tree of a rule with a value that you compute from its captures, usually a typed node. This guide
shows how to write actions, the built-in functions and lambdas, how positions of constructed nodes are assigned, and
recipes for lists and left- and right-associative chains. The grammars run like those in [Shaping
Trees](shaping-trees.md), which also explains how to run them. The exact rules are in the [Actions](../../spec/actions.md)
chapter. For operator expressions (`foldl` chains versus Operators Rule Definition Statements) see [Operators
Rule](operators-rule.md).

An action is an expression after `=>` inside a rule body that computes the value of the rule from what the body matched:

```pego
rule <name> [<type>] = <rule expression> => <action expression>
```

The action covers the whole right-hand side, and its result must be a node (or `nil`). Inline actions such as
`(expression => action)` are recognized by the source parser but are rejected by compilation, so they cannot yet attach
an action to one alternative or group; give each alternative its own rule:

```pego
// inline-action.pego
rule bad =  "a" (b:"b" => $b)
```

```bash
pego parse -g inline-action.pego -s bad -i 'ab'
```

```text
pego: inline-action.pego:2:24: inline actions are not implemented
```

## Referring to Captures

`$label` refers to the value captured with `label:` in this rule. Only labeled elements can be used in an action:
label each element the action needs, as `name:name` and `value:value` in the rule of [Structs, Unions and Terminal Types](types.md#structs-unions-and-terminal-types). Grammars written for
earlier versions may refer to elements by position (`$1`, `$0`); these are rejected, and `pego fix` rewrites them by
labeling the elements they refer to.

Captures are visible only in the rule that made them, not in the rules it calls or that call it. A reference to an
undefined capture is a compile-time error (`undefined capture $z`).

## Building Nodes and Reading Fields

`T{Field: value, ...}` builds a struct node. Field values are checked against the declared field types, so typos and
mismatches are compile-time errors:

```pego
// field-errors.pego
type Name terminal
type Number terminal
type Assign = { Name Name; Value Number }

rule a Assign = k:name "=" v:name => Assign{Name: $k, Value: $v}
rule b Assign = k:name "=" v:number => Assign{Name: $k, Valu: $v}
rule name Name = [a-z]+
rule number Number = [0-9]+
```

```bash
pego parse -g field-errors.pego -s a -i 'a=b'
```

```text
pego: field-errors.pego:7:61: Assign has no field Valu
```

## Typed Node Lists and Terminal Nodes

A typed node-list literal states the element Type directly rather than inferring
it from the evaluated values. Every item must have a known Type assignable to the target,
including items in nested lists. Nil is accepted only for an optional element
Type; an empty typed list is still a non-nil `List`.

```pego
type NameNode = { Name string }
rule name NameNode = v1:[a-z]+ => NameNode{Name: text($v1)}
rule names []NameNode = a:name "," b:name => []NameNode{$a, $b}
rule emptyNames []NameNode = "" => []NameNode{}
```

A terminal node can be built from a string already available in an Action
Expression. The text is stored as supplied; PEGO does not reparse it against the
terminal's matching Rule Expression. The constructor needs one string argument.

```pego
type Identifier terminal
rule identifier Identifier = [a-z]+
rule normalized Identifier = n:identifier => Identifier{text($n)}
```

Struct constructors still use named fields and commas. Struct Type Definition
Statement fields use semicolons or hard newlines. Lists of primitive values such
as `[]int` are not supported.

`x.Field` reads a field of a node; `x` may be a capture or the result of another access. Every node also has the fields
`startPos`, `endPos` (integers) and `children` (a list). Reading a field of a value that may be `nil` is an error, so
`n:name?` followed by `$n.startPos` does not compile. When `x` has a union type the field is allowed if at least one
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

rule main Word = ws w:word ws $$ => $w
rule word Word = n:[a-z]+ #atomic => Word{Text: text($n), Length: len($n), Long: len($n) > 4, From: $n.startPos, To: $n.endPos}
rule ws = " "*
```

```bash
pego parse -g derived.pego -f sexpr -i '  hello '
```

```text
(Word From=2 Length=5 Long=true Text=`hello` To=7)
```

## Built-In Functions and Lambdas

| Function | Result |
|:--|:--|
| `len(x)` | The length of a string or terminal in positions; the number of children of any other node; `0` for `nil` |
| `text(x)` | The input text that the node covers; `""` for `nil` |
| `list(a, b, ...)` | A list of the arguments (nodes or `nil`) |
| `concat(l1, l2, ...)` | The concatenation of lists; a `nil` argument counts as an empty list |
| `map(list, (x) => ...)` | The list of the results of applying the function to each element |
| `filter(list, (x) => bool)` | Keeps the original elements whose predicate returns `true` |
| `foldl(init, list, (acc, x) => ...)` | Folds the list from the left, starting with `init` |
| `foldr(init, list, (acc, x) => ...)` | Folds the list from the right, starting with `init` |

The function arguments are **lambdas**. They can only be written as arguments of `map`, `filter`, `foldl` and `foldr`, and
inside them the parameters are variables, referred to by name without `$`: `acc`, `x`. A lambda can also read the captures
of the enclosing rule with `$`. The function passed to `map` must produce a node (or `nil`) and `filter`'s predicate a
Boolean. The [specification](../../spec/actions.md#built-in-functions) defines each function exactly: element types, `nil`
and `Error` arguments, evaluation order and what happens when a callback fails.

```pego
// builtins.pego
type Call = {
    Name  string
    Arity int
    Raw   string
}

rule call Call = n:[a-z]+ #atomic "(" args:(a:[a-z]+ #atomic -","?)* ")" => Call{Name: text($n), Arity: len($args), Raw: text($args)}
```

```bash
pego parse -g builtins.pego -s call -f sexpr -i 'f(a,b,c)'
```

```text
(Call Arity=3 Name=`f` Raw=`a,b,c`)
```

`text($args)` is the input text covered by the node, including the commas that were discarded from the tree: ranges are
properties of the input, not of the children. Here is a lambda that reads a capture of the enclosing rule, `$k`, to
pair the key with every item:

```pego
// outer-capture.pego
type Ident terminal
type Pair = { A Ident; B Ident }
type Pairs = { Items []Pair }

rule pairs Pairs = k:ident ":" xs:(x:ident -","?)* => Pairs{Items: map($xs, (r) => Pair{A: $k, B: r.$x})}
rule ident Ident = [a-z]+
```

```bash
pego parse -g outer-capture.pego -s pairs -f sexpr -i 'k:a,b'
```

```text
(Pairs Items=[(Pair A=Ident"k"@ident B=Ident"a"@ident) (Pair A=Ident"k"@ident B=Ident"b"@ident)])
```

Action expressions also support the arithmetic, comparison and boolean operators listed in the
[Actions chapter](../../spec/actions.md#operators-and-functions). There is no conditional expression, so branch on the grammar side
(separate rules or alternatives) rather than inside actions.

## Positions

Every node has a range `[start, end)`. Nodes of the default tree cover what their expression matched. A constructed node has one of two ranges:

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
type Name terminal
rule name Name = [^=]+
rule kv = k:name "=" v:name
```

```bash
pego parse -g unit.pego -s kv -i 'あい=うえお' | grep -A6 '"v"'
pego parse -g unit.pego -s kv -i 'あい=うえお' -unit bytes | grep -A6 '"v"'
```

```text
    "v": {
      "type": "Name",
      "rule": "name",
      "start": 3,
      "end": 6,
      "text": "うえお"
    }
    "v": {
      "type": "Name",
      "rule": "name",
      "start": 7,
      "end": 16,
      "text": "うえお"
    }
```

## Recipes

### Left-Associative Trees With `foldl`

A repetition gives a flat list; a fold turns it into the nested tree. Capture the first operand, capture the repeated
`(operator, operand)` pairs, and fold from the left, so that `1 - 2 + 3` becomes `(1 - 2) + 3`:

```pego
// sum.pego
type Num terminal
type Op terminal
type Bin = {
    Left  Expr
    Op    Op
    Right Expr
}
type Expr ~ Num | Bin

rule main Expr = e:sum $$ => $e
rule sum Expr = first:num rest:(-ws op:op -ws n:num)* => foldl($first, $rest, (acc, i) => Bin{Left: acc, Op: i.$op, Right: i.$n})
rule num Num = [0-9]+
rule op Op = "+" / "-"
rule ws = " "*
```

```bash
pego parse -g sum.pego -f sexpr -i '1 - 2 + 3'
pego parse -g sum.pego -f sexpr -i '7'
```

```text
(Bin Left=(Bin Left=Num"1"@num Op=Op"-"@op Right=Num"2"@num) Op=Op"+"@op Right=Num"3"@num)
Num"7"@num
```

Notes: the accumulator starts as a `Num` and becomes a `Bin`, so its type is widened to the union of both, and the
declared `Expr ~ Num | Bin` must contain both. With a single operand the repetition is empty and the fold returns the
initial value unchanged, which is why `7` stays a bare `Num`. For several precedence levels you would write one such
rule per level; for that case also look at [Operators Rule](operators-rule.md).

### Right-Associative Trees With `foldr`

`a ^ b ^ c` must be `a ^ (b ^ c)`. Capture the pairs *(operand, operator)* and the last operand separately, and fold
from the right, starting with the last operand:

```pego
// pow.pego
type Num terminal
type Op terminal
type Bin = {
    Left  Expr
    Op    Op
    Right Expr
}
type Expr ~ Num | Bin

rule pow Expr = pairs:(l:num -ws op:op -ws)* last:num => foldr($last, $pairs, (acc, i) => Bin{Left: i.$l, Op: i.$op, Right: acc})
rule num Num = [0-9]+
rule op Op = "^"
rule ws = " "*
```

```bash
pego parse -g pow.pego -s pow -f sexpr -i '2 ^ 3 ^ 4'
pego parse -g pow.pego -s pow -f sexpr -i '2'
```

```text
(Bin Left=Num"2"@num Op=Op"^"@op Right=(Bin Left=Num"3"@num Op=Op"^"@op Right=Num"4"@num))
Num"2"@num
```

The repetition is greedy and does not backtrack, but it is still correct here: it consumes `2 ^` and `3 ^`, then
`4` fails to be followed by `^`, so the repetition stops and `last` matches `4`.

### Member Chains With a Union Accumulator

The same fold builds `a.b.c`. The accumulator is first an `Ident` and then a `Member`, so declare the union and use it
for the rule type and the field:

```pego
// path.pego
type Ident terminal
type Member = {
    X    Path
    Name Ident
}
type Path ~ Ident | Member

rule path Path = first:ident rest:(-"." n:ident)* => foldl($first, $rest, (acc, i) => Member{X: acc, Name: i.$n})
rule ident Ident = [a-z]+
```

```bash
pego parse -g path.pego -s path -f sexpr -i 'a.b.c'
```

```text
(Member Name=Ident"c"@ident X=(Member Name=Ident"b"@ident X=Ident"a"@ident))
```

### Flattening Separated Lists

`first (sep item)*` yields a nested `Seq`/`List` structure. To get one flat list of items, capture the repeated part and
rebuild a list: `list($first)` makes a one-element list, `map` extracts the item from every iteration, and `concat`
joins them. A list that is optional (`args?`) works with `concat` too, because a `nil` argument counts as empty:

```pego
// args.pego
type Ident terminal
type Call = {
    Fn   Ident
    Args []Ident
}

rule call Call = fn:ident -ws "(" -ws args:args? -ws ")" => Call{Fn: $fn, Args: concat($args)}
rule args = first:ident rest:(-ws "," -ws x:ident)* => concat(list($first), map($rest, (r) => r.$x))
rule args_cst = ident (-ws "," -ws ident)*
rule ident Ident = [a-z]+
rule ws = " "*
```

```bash
pego parse -g args.pego -s call -f sexpr -i 'f(a, b, c)'
pego parse -g args.pego -s call -f sexpr -i 'f()'
pego parse -g args.pego -s args_cst -f sexpr -i 'a, b, c'
```

```text
(Call Args=[Ident"a"@ident Ident"b"@ident Ident"c"@ident] Fn=Ident"f"@ident)
(Call Args=[] Fn=Ident"f"@ident)
(Seq Ident"a"@ident [(Seq "," Ident"b"@ident) (Seq "," Ident"c"@ident)])@args_cst
```

`args_cst` shows what you would otherwise get: the first item followed by a list of `(Seq "," item)`. Writing
`Args: $args` directly does not compile, because `$args` is optional (`*[]Ident`) and the field is `[]Ident`:

```pego
// args-optional.pego
type Ident terminal
type Call = { Fn Ident; Args []Ident }
rule call Call = fn:ident "(" args:args? ")" => Call{Fn: $fn, Args: $args}
rule args = first:ident rest:(-"," x:ident)* => concat(list($first), map($rest, (r) => r.$x))
rule ident Ident = [a-z]+
```

```bash
pego parse -g args-optional.pego -s call -i 'f(a)'
```

```text
pego: args-optional.pego:4:67: cannot use *[]Ident as []Ident in field Args of Call
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
type Ident terminal
type Value terminal
type Entry = {
    Key   Ident
    Value Value
}
type Config = { Entries []Entry }

rule main Config = es:entries ws $$ => Config{Entries: $es}
rule entries = es:(-ws e:entry)* => map($es, (x) => x.$e)
rule entry Entry = k:key -ws -"=" -ws v:value -ws -";" => Entry{Key: $k, Value: $v}
rule key Ident = [a-z_]+
rule value Value = [a-z0-9.]+
rule ws = ([ \t\r\n] / "#" [^\n]*)*
```

```bash
pego parse -g config.pego -f sexpr -i $'# web server\nhost = example.com;  # comment\nport = 8080;\n'
```

```text
(Config Entries=[(Entry Key=Ident"host"@key Value=Value"example.com"@value) (Entry Key=Ident"port"@key Value=Value"8080"@value)])
```

Comments, whitespace, `=` and `;` are all gone; only the entries remain. If you also need the comments (for a
formatter, say), capture them instead of discarding them.

## Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `T has no field F` | A typo, or the struct does not declare the field |
| `cannot access .F of *T` | The value may be `nil` (for example `n:name?`). Read the field in a rule where the value is always present |
| `inline actions are not implemented` | The compiler recognized an inline action such as `(p:part => build($p))`, but inline action execution is not implemented |
| Nondefault rule-level Value Action on an Operators Rule | Omit the action, use the explicit `=> ...` default-CST form, or put the action on an operand or operator entry |
| `undefined capture $z` | The capture is not made in this rule; captures are not visible across rules |

## Related Topics

- [Declaring Types](types.md): the types that actions construct.
- [Shaping Trees](shaping-trees.md): captures, atomic and discard.
- [Context-Sensitive Parsing](context-sensitive.md): predicates, which read captures too.
