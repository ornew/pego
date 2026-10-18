# Nodes and Values

A parse that succeeds returns a value, and the value of a rule that builds a
tree is a **node**. This chapter defines the node model, the Go type
`pego.Node` and its field accessors, the traversal of a node graph, and the text
and JSON renderings of nodes. The text and JSON renderings are specified in
their own sections.

## Scope and Terminology

A **node** is a value produced by a parse: a node of the concrete syntax tree
(see [Values and the Concrete Syntax Tree](parser-expressions.md#values-and-the-concrete-syntax-tree)),
a terminal, a list, or a struct or terminal built by an action. A **node graph**
is the set of nodes reachable from a root through children and fields. A graph is
usually a tree, but a node MAY be reachable by several edges (shared), and a
graph MAY contain a cycle when a caller builds one.

## Contract

### Node Model

| Property | Meaning |
|:--|:--|
| Type | The type name: a [reserved node type](types.md#reserved-node-types) (`Match`, `Seq`, `List`, `Operator`, `Error`, `Missing`) for nodes of the concrete syntax tree, or the name of a type defined in the grammar |
| Rule | The name of the rule that produced the node, if it is a node that a rule without an action created in its own body; otherwise empty |
| Range | `[start, end)` in the position unit |
| Text | For a terminal, the matched text |
| Children | For a `Seq`, `List` or `Operator`, the child nodes; a child is `nil` where an optional expression did not match |
| Fields | The captures of a `Seq` or of a node that a rule produced, and the fields of a struct node. Each value is a node, an integer, a string, a boolean or `nil` |

The data of each node type is held as follows.

| Type | Produced by | Where the data is |
|:--|:--|:--|
| `Match` | A terminal: a literal, a character class, an atomic expression `e #atomic` | `Text` |
| `Seq` | A sequence in a rule without an action | `Children` (the elements that have a value) and `Fields` (captures) |
| `List` | A repetition | `Children` |
| `Operator` | An [Operators Rule](operators-rule.md) operator application without an action | `Children`: the operands and the operator (left, operator, right for an infix operator) |
| `Error` | A range skipped by `#recover` | `Text` (the skipped input) and the string field `message` |
| `Missing` | A `#missing` literal insertion | Empty text and range, and the string fields `expected` and `message` |
| a type name | `T{...}` in an action, or a terminal type | `Fields` for struct types, `Text` for terminal types |

### The Go Type

```go
type Node struct {
	Start, End int32   // range in the input, End exclusive
	Text       string  // the text of a terminal
	Children   []*Node // children of Seq, List and Operator nodes; omitted elements are nil
	Fields     Fields  // struct fields and captures
	// unexported: type and rule names
}
```

| Member | Behavior |
|:--|:--|
| `n.Type()` | The type name |
| `n.Rule()` | The rule name, or the empty string |
| `n.IsTerminal()` | True for a `Match` or a terminal type; false for a nil receiver. For these nodes `Text` holds the text |
| `n.Field(name)` | The value of the field, or `nil` if the field is absent or `n` is nil |
| `n.Fields.Get(name)` | The value and whether the field exists |
| range over `n.Fields` | `NodeField{Name, Value}` in the order in which the fields were set; each name appears at most once |
| `n.Clone()` | A deep copy; subtrees shared within the tree stay shared in the copy. A `Document` moves the nodes that it reuses after an edit, which changes trees that its earlier parses returned; a clone keeps a tree as it was |

A field value is a `*Node`, an `int`, a `string`, a `bool` or `nil`; its static Go
type is `any`. `Start` and `End` are `int32`, and delimit the input that the node
covers as a half-open range in the position unit of the parse. A rule that
includes trailing white space covers it. The text of a node that is not a
terminal is obtained by slicing the input with its range or with `text(...)` in an
action.

### Traversal

`pego.Walk(root, visit)` visits a node graph in a defined order. `visit`
receives a `WalkVisit` and returns a `WalkAction` and an error.

| `WalkVisit` member | Meaning |
|:--|:--|
| `Value` | The value reached by the current edge; typed nil leaves are retained |
| `Node` | Non-nil only if the value is a non-nil `*Node` |
| `Parent` | The nearest owning node |
| `Depth` | The number of edges from the root |
| `Edge` | The incoming edge: a child index, a field name or a position in a nested `[]any` |

- The root is visited first. Then the children of a node are visited in slice
  order, its fields in sorted name order, and elements of a nested `[]any` value
  in index order. Scalar and nil values are visited. Other containers are leaves.
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

`n.String()`, which `fmt` uses, renders a node as an S-expression-like string
for tests and debugging.

| Form | Meaning |
|:--|:--|
| `"a"` | A `Match` terminal |
| `Number"12"` | A terminal of the type `Number` |
| `(Seq "a" "b")` | A `Seq` |
| `["a" "a"]` | A `List` |
| `(Op L=1 R=2)` | A struct node and its fields |
| `x@rule` | A node produced by the rule `rule` without an action |

The command `pego parse -f sexpr` prints this form.

### JSON Encoding

A node encodes with `encoding/json` as an object with the members `type`, `rule`
(if not empty), `start`, `end`, `text` (if not empty), `children` (if any) and
`fields` (if any). The `fields` object has its keys in lexicographic order, so the
encoding is deterministic. A child that is `nil` is `null`. `pego parse` prints
this form by default.

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
| `Field` on a nil node, or for an absent field | `nil`; use `Fields.Get` to tell an absent field from a `nil` value |

## Compatibility and Support

Generated Go parsers expose `Node` trees from `Parse` and their own types for
typed values. Generated TypeScript parsers return a `Node` class with the members
`type`, `rule`, `start`, `end`, `text`, `children` and `fields`. See [Generated Parsers](generated-parsers.md).

## Conformance

`pego_test.go`, `traversal_test.go` and the node JSON tests of the repository
establish the model, the order of traversal and the JSON form, and the backend
equivalence tests require every backend to return identical nodes.
