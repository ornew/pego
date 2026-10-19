# Nodes and Values

A parse that succeeds returns a value. The value of a rule that has no action is
a **node**: a value together with the range of input that it covers. This
chapter defines the node model, the Go type `pego.Node` and its accessors, the
traversal of a node graph, and the text and JSON renderings of values. The text
and JSON renderings are specified in their own sections.

## Scope and Terminology

A **node** is a value produced by a parse with a range `[start, end)` of the
input. The value of a node is one of the following.

- A string: the input text that a literal, a character class, `.`, an
  [atomic](pragmas.md#atomic) expression or a [terminal type](types.md#terminal-types)
  rule matched.
- A tuple: the values of the elements of a sequence, in order.
- A list: the values of the iterations of a repetition, in order.

An **error value** is what [error recovery](pragmas.md#recover) and a
[missing token](pragmas.md#missing) produce. An action can also build a **struct**
(`T{...}`), a tuple or list without a range, and an `int`, `string` or `bool`.
A **value** is any of these or `nil`; Go code reads it through the accessors of
`pego.Node`, which stands for every one of them except `nil`. A **node graph** is the set
of nodes reachable from a root through elements and fields. A graph is usually a
tree, but a node MAY be reachable by several edges (shared), and a graph MAY
contain a cycle when a caller builds one.

The value of a node is stored once. A captured element is an element of the tuple
that contains it; a capture is a name for its location and not a second copy of it
(see [Captures](parser-expressions.md#capture)).

## Contract

### Node Model

| Kind | Produced by | Where the data is |
|:--|:--|:--|
| String | A literal, a character class, `.`, an atomic expression, a terminal type | The range and the text, which is the value |
| Tuple | A sequence in a rule without an action; an [Operators Rule](operators-rule.md) operator application without an action | The range and the elements: the values of the elements of the sequence that have a value, in order. An element is `nil` where an optional expression or a choice alternative without a value matched |
| List | A repetition | The range and the elements: the values of the iterations |
| Error | A range skipped by `#recover`, or a `#missing` literal insertion | The range, the message and, for an insertion, the expected text |
| Struct | `T{...}` in an action | The fields, named by the struct type; the range of the node that the action replaced |
| Tuple value, list value | `list(...)`, `map(...)`, `[]T{...}` and the like in an action | The elements; no range |
| Value | An `int`, `string` or `bool` that an action returns | The value; no range |

A rule that has only valueless elements, such as `rule a = _`, and a repetition
iteration whose expression has no value, produce a tuple of no elements, the **unit**
`()`, with the range that they matched.

A node produced by a rule without an action in its own body carries the name of
that rule when the parser records [source information](#source-information).

### Source Information

A parser compiled with **source information** (`pego.WithSourceInfo()`,
`pego parse -source-info`, or the option of `pego compile` and `pego gen`) records
two things that are otherwise not part of a result: the rule that produced each node
that a rule without an action created in its own body, and where the captures of that
node are. `Node.Rule`, `Node.Field` and `Node.Fields` on a node, the `rule` and
`captures` members of the JSON encoding and the `@rule` and `name:` forms of the text
rendering report them. Without source information they are empty and absent.
Source information does not change the values of a parse, the values that actions read,
or the ranges.

### The Go Type

`pego.Node` is an opaque type: no field is exported, and no method commits to
a layout in memory. A `*Node` is nil where a value is `nil`. `Text`, `Len`,
`Elems`, `Value`, `Field`, `Fields` and `IsTerminal` accept a nil receiver; the
other methods do not.

```go
func (n *Node) Kind() NodeKind
func (n *Node) Start() int
func (n *Node) End() int
func (n *Node) Text() string
func (n *Node) Len() int
func (n *Node) Elem(i int) *Node
func (n *Node) Elems() iter.Seq2[int, *Node]
func (n *Node) Value() any
func (n *Node) Field(name string) any
func (n *Node) Fields() iter.Seq2[string, any]
func (n *Node) Type() string
func (n *Node) Rule() string
func (n *Node) IsTerminal() bool
func (n *Node) Clone() *Node
func (n *Node) String() string
func (n *Node) WriteJSON(w io.Writer) error
```

`NodeKind` is `pego.NodeKind`, with the constants below.

| `Kind()` | The node is | Methods that apply |
|:--|:--|:--|
| `KindString` | A string node | `Text`, `Value` |
| `KindTuple` | A node whose value is a tuple | `Len`, `Elem`, `Elems` |
| `KindList` | A node whose value is a list | `Len`, `Elem`, `Elems` |
| `KindError` | An error value | `Text` (the message) |
| `KindStruct` | A struct built by an action | `Type`, `Field`, `Fields` |
| `KindTupleValue` | A tuple built by an action, without a range | `Len`, `Elem`, `Elems` |
| `KindListValue` | A list built by an action, without a range | `Len`, `Elem`, `Elems` |
| `KindValue` | An `int`, `string` or `bool` built by an action | `Value` |

| Member | Behavior |
|:--|:--|
| `n.Start()`, `n.End()` | The range `[start, end)` of the input that the node covers, in the position unit of the parse. A rule that includes trailing white space covers it |
| `n.Text()` | The string value of a `KindString` node, and the message of a `KindError` node. It is the empty string for other kinds and for nil |
| `n.Len()` | The number of elements of a tuple or list; 0 for other kinds and for nil |
| `n.Elem(i)` | The element `i`: a node, or nil for an element that has no value. It panics if `i` is outside `[0, Len())` |
| `n.Elems()` | The elements in order, as `Elem` returns them |
| `n.Value()` | The value of a `KindValue` node and the string of a `KindString` node; nil for other kinds and for nil |
| `n.Field(name)` | The value of the struct field, or, with source information, the capture `name` of a node; nil if there is none or `n` is nil. A field value is a `*Node`, an `int`, a `string`, a `bool` or nil |
| `n.Fields()` | The fields of a struct in the order that the action wrote them and, with source information, the captures of a node |
| `n.Type()` | The name of the struct type or terminal type of the node. For other nodes it is the name of the built-in node type: `Match`, `Seq`, `List`, `Operator`, `Error` or `Missing` |
| `n.Rule()` | The rule name with source information, or the empty string |
| `n.IsTerminal()` | True for a string node, including one of a terminal type, and for an error value from `#recover`; false for nil |
| `n.Clone()` | A deep copy; subtrees shared within the tree stay shared in the copy. A `Document` moves the nodes that it reuses after an edit, which changes trees that its earlier parses returned; a clone keeps a tree as it was |

`Start` and `End` delimit the input that the node covers as a half-open range in the
position unit of the parse. The text of a node that is not a string node is obtained by
slicing the input with its range or with `text(...)` in an action.

### Traversal

`pego.Walk(root, visit)` visits a node graph in a defined order. `visit`
receives a `WalkVisit` and returns a `WalkAction` and an error.

| `WalkVisit` member | Meaning |
|:--|:--|
| `Value` | The value reached by the current edge; typed nil leaves are retained |
| `Node` | Non-nil only if the value is a non-nil `*Node` |
| `Parent` | The nearest owning node |
| `Depth` | The number of edges from the root |
| `Edge` | The incoming edge: an element index, a field name or a position in a nested `[]any` |

- The root is visited first. Then the elements of a node are visited in order, its
  fields in sorted name order, and elements of a nested `[]any` value in index order.
  The fields are those that `Fields` returns: the captures of a node parsed without
  source information are not visited.
  Scalar and nil values are visited. Other containers are leaves.
- A shared node or list is visited once for each incoming edge. Only a cycle on
  the active traversal path is an error: `Walk` returns `*pego.CycleError`, which
  holds the path of the back edge.
- `WalkSkip` omits the descendants of the visit; `WalkStop` ends the walk
  successfully. An error returned by `visit` stops the walk and is returned
  unchanged. A nil callback is an error.
- `Walk` does not mutate or retain the graph. The graph MUST NOT be mutated while
  the walk or its callback runs.

`pego.Cursor{Root, Unit}` selects nodes by position, using `Walk` order. `Unit`
MUST be the unit of the parse that produced the positions; it is not inferred or
converted, and an invalid unit is an error. A parent's range does not prune
traversal, so a matching descendant is visited even if its parent does not match.
Scalars and lists have no range and are never selected.

| Method | Selects |
|:--|:--|
| `At(position, visit)` | Nodes that contain `position`, including zero-width nodes at `position` |
| `Range(start, end, visit)` | Nodes that overlap `[start, end)`. An empty range selects none. Zero-width nodes match when `start <= node.Start < end`. `start` MUST be nonnegative and `end` MUST NOT be smaller than `start` |

Generated Go parsers expose the same walker as `WalkTree`, with the same visits
and ordering.

### Text Rendering

`n.String()`, which `fmt` uses, renders a value as an S-expression of its shape,
without positions, for tests and debugging.

| Form | Meaning |
|:--|:--|
| `"a"` | A string value, quoted |
| `("a" "b")` | A tuple; `()` is the unit |
| `["a" "a"]` | A list |
| `nil` | A missing element or an optional that did not match |
| `X{A: 1, B: "s"}` | A struct, with its fields in name order |
| `error("msg")` | An error value with its message; an inserted token adds ` expected "text"` after the message |
| `1`, `true` | An `int` or a `bool` |

With source information, a node is followed by `@rule` when it has a rule, a
captured element is preceded by `name:` (`x:"a"`; one element can carry several
labels), and a capture whose value is not an element is written after the elements
as `&name:value`. For example, `(k:["a"]@key ":" v:["1"]@num)@main` is a tuple
produced by `main` whose first element is the list `["a"]` of `key`, captured as
`k`, and whose last element is the list of `num`, captured as `v`.

The command `pego parse -f sexpr` prints this form.

### JSON Encoding

A value encodes with `encoding/json` as follows.

| Value | JSON |
|:--|:--|
| A string, tuple or list node | An object with the members `start`, `end` and `value`: a string, or an array of the encodings of the elements, in which a missing element is `null` |
| An error value | An object with the members `start`, `end`, `error` (the message) and, for a missing token, `expected` |
| A struct | An object with the members `type` and `fields`; the `fields` object has its keys in lexicographic order, so the encoding is deterministic |
| A tuple or list value | An array |
| `int`, `string`, `bool`, `nil` | A number, a string, a boolean, `null` |

With source information, a node object also has the members `rule` (if not empty) and
`captures` (if any), after `value`. `captures` maps each capture name to an object
with `path`, the indexes that lead from the node's tuple to the element that the
capture labels (the first index is an element of the node's tuple, each further one an
element of the tuple in the element before it), or to an object with `value` for a
capture that is not an element. A tuple and a list encode the same way; the schema
tells them apart (see [Output Schemas](schemas-and-queries.md)). `pego parse`
prints this form by default.

Nodes cannot be decoded from JSON; the encoding is for inspection and for
exchanging results with other programs.

`n.WriteJSON(w)` writes the node graph as JSON without recursive node or `[]any`
traversal, so it handles trees that are deeper than the nesting limit of
`encoding/json`. A nil receiver writes `null`. Shared edges are encoded
separately and an active-path cycle is an error. It does not close or flush `w`,
and an error MAY leave a prefix written. Maps and other containers delegate to
`encoding/json`, which is outside the iterative depth guarantee. `MarshalJSON`
uses the same writer, but wrapping it in `json.Marshal` can still meet the nesting
limit of that package when it validates the returned bytes. The graph MUST NOT be
mutated concurrently.

## Validation and Errors

| Condition | Result |
|:--|:--|
| `Walk` with a nil callback | Error |
| A cycle on the active path of `Walk` or `WriteJSON` | `*pego.CycleError` (`Walk`) or an error (`WriteJSON`) |
| `Cursor` with an invalid unit or range | Error |
| `Field` on a nil node, on a node without source information, or for an absent field | `nil` |
| `Elem` with an index outside the elements | Panic |

## Compatibility and Support

Generated Go parsers expose `Node` trees from `Parse` with the same methods, and their
own types for typed values. Generated TypeScript parsers return a `Node` class with the members
`type`, `rule`, `start`, `end`, `text`, `children` and `fields`, which hold the string,
the elements and the fields or captures of the corresponding Go accessors, and the same
JSON and text renderings. See [Generated Parsers](generated-parsers.md).

## Conformance

`pego_test.go`, `traversal_test.go` and the node JSON tests of the repository
establish the model, the order of traversal and the JSON form, and the backend
equivalence tests require every backend to return identical nodes.
