# Output Schemas and Structural Queries

`Parser.OutputSchema` describes the possible values of a checked grammar. A
structural query searches the values of an actual parse result. Together they let a
tool inspect the shape of a grammar and find values without generating code or
serializing a result. This chapter defines both.

> The Go APIs in this chapter are experimental and MAY change incompatibly. The
> schema JSON format is versioned; this chapter defines version 2.

## Scope and Terminology

A **schema** is a description of the output of a grammar: its declared struct types
and the inferred value shape of each rule. A **pattern** (`query.Pattern`) is a
programmatic description of a value and of the values reachable from it. A **query**
is a compiled pattern, evaluated against a parse result. A **match** is one occurrence
of a value in the result that satisfies the pattern, together with its captures.

A parser compiled with **source information** records the rule that produced each
node and the labels of its captures. Rule constraints need it.

## Contract

### Output Schemas

`p.OutputSchema(opts...)` returns the schema of the parser's grammar.

- The default schema contains the configured start rule, the rules it calls and the
  types that their output shapes reference. `WithAllSchemaRules()` also includes
  unreachable rules and all declared types. `WithStart` changes the default root.
- A type entry is either a declared struct type, with ID `declared/<Name>`, or the
  inferred value of a rule, with ID `inferred/<rule>`. Aliases are not
  entries: their shapes appear where they are used.
- `sourceInfo` records whether the parser was compiled with source information.
- Entries, fields, join alternatives and captures have a stable order, so repeated
  exports of one checked grammar encode to identical JSON bytes.
- Export needs checked grammar metadata, which only the grammar source has. A
  parser loaded from a compiled grammar holds none, in any mode: its export fails
  with an error that matches `pego.ErrSchemaUnavailable`, and `pego schema`
  rejects a compiled grammar. Export the schema from the grammar source.

### Shapes

A shape describes a set of values. Each kind allows only the members listed.

| Kind | Members | Values |
|:--|:--|:--|
| `scalar` | `scalar`: `int`, `string` or `bool` | The scalar |
| `nil` | | `nil` |
| `top` | | Every value (the type `_`) |
| `error` | | An error value of recovery |
| `missing` | | A token that recovery inserted, a kind of `error` value |
| `node` | `value`, `captures` | A node whose value has the shape `value` |
| `tuple` | `elements` | A tuple with one element per shape, in order |
| `list` | `element` | A list whose elements have the shape `element` |
| `optional` | `element` | The values of `element` and `nil` |
| `join` | `alternatives` | The values of any alternative; no alternatives describe no value |
| `struct` | `fields` | A struct with the named fields |
| `ref` | `ref` | The values of the referenced type entry |

A node shape requires `value`. Each entry of `captures` names a capture of the node
and gives its `shape` and its locations: `paths` lists the index paths of the
elements of the node's value that the capture labels, and `side` is true when the
capture is held beside the value rather than as one of its elements. A capture has at
least one location. Captures are static: the schema describes them whether or not the
parser records labels.

A tuple of no elements is the unit value.

### Schema JSON, Version 2

Decoding requires exact field-name case and rejects unknown fields, duplicate keys at
every object level, unsupported formats and versions and incomplete envelopes.

`schema.Validate()` performs the full version 2 validation of a schema, whether it
was decoded from JSON or built in Go. It checks that:

- type IDs, rule names, field names and the capture names of a node are nonempty
  and unique;
- each type ID is `declared/<name>` or `inferred/<name>` and agrees with its origin,
  and every declared type has a struct shape;
- shape kinds and scalar names are supported, and each shape has only the members of
  its kind;
- list and optional shapes have their element, and node shapes their value;
- every capture has a location, and no index of a path is negative;
- references resolve, and inline shapes are acyclic (cycles through named references
  are allowed);
- `Start` names a rule of `Rules`, and the `Result` of each rule is a reference
  exactly to `inferred/<rule-name>`, which exists as a type entry.

Omitted, nil and empty member slices are equivalent.

Validation checks structure and canonical relationships. Type names alone do not
establish compatibility between grammars.

### Comparing Schemas

`schema.Compare(before, after)` validates both complete operands and
compares their contents by type ID and rule name. It returns the added, removed and
changed entries in sorted order; for equal schemas its `Types` and `Rules` slices are
empty. The comparison ignores the order of entries, of struct fields, of join
alternatives, of captures and of capture paths, and envelope fields such as `Start`
and `sourceInfo`; tuple element order and named references are significant. A diff is descriptive: it does not classify compatibility.

### Patterns

`query.Pattern` constrains a value and the values reachable from it.

| Member | Constrains |
|:--|:--|
| `Category` | The category of the value: `node`, `struct`, `tuple`, `list`, `int`, `string`, `bool`, `nil` or `error` |
| `Type` | The name of a declared struct type, or `error` |
| `Rule` | The rule that produced a node |
| `Fields` | Named fields of a struct or of an error value; each is a pattern |
| `Value` | The value of a node |
| `Elements` | An ordered prefix of the elements of a tuple or a list, or of a node's value; extra elements are allowed |
| `Capture` | Binds the value at this pattern under the given name |

- `Elements` sees through nodes as `x[i]` does: on a node it applies to the elements
  of the node's value.
- The fields of an error value are `message` and `expected`, both strings.
- Constraints that no value can satisfy together, such as `Category: "int"` with
  `Fields`, are rejected when the query is compiled.
- Patterns are copied when compiled. There is no text query language and there are
  no text predicates.

### Compiling Queries

`query.Compile(pattern, schema)` compiles a query. A supplied schema is
validated first, and the compilation rejects patterns that no described value can
match: unknown type and rule names, fields that a type does not have and categories
or types that a value cannot have. Nested patterns are checked against the shape of
the field, element or node value to which they apply.

- A join or optional shape accepts a pattern that matches any one of its
  alternatives. With `Value *(Pair | Box)`, a `Value` pattern of type `Pair` with
  `Pair` fields, of type `Box` or of category `nil` compiles, while one of category
  `string` does not.
- A pattern applied to a value of unknown shape (`top` or the query root) is checked
  only against its own type's fields.
- An error value appears only where the shape includes `error` or `missing`, so a
  pattern of type or category `error` compiles only there. A missing token is an error
  value: the patterns of errors, with their fields, apply to a `missing` shape.
- For an optional scalar field such as `Value *string`, the categories `string` and
  `nil` compile and other known categories do not.
- A pattern with a `Rule` constraint compiles only against a schema whose
  `sourceInfo` is true, and only for a rule the schema has.
- A nil schema defers these checks to evaluation. Evaluation always checks the
  runtime values, because a caller MAY mutate a result after parsing.

`query.Prepare(schema)` validates a schema once and returns a prepared
schema whose `Compile(pattern)` performs the same pattern checks
without validating again. A prepared schema owns a copy of the schema and is safe for
concurrent use.

### Evaluation

`Query.Matches(root, opts...)` accepts a parse result and returns all
matches or an error.

- Matches visit each value before its outgoing edges: the elements of a tuple, of a
  list or of a node's value by index, then the captures of a node that are held
  beside its value, by name, and the fields of a struct by name. A shared value can
  produce several matches.
- Each `query.Capture` holds the captured value, its immediate containing `Parent`
  and an owned `Path` relative to the root, made of `element`, `capture`, `field`
  and `value` edges. Captured values and parents belong to the input graph. The
  graph MUST be stable while results are used and MUST NOT be mutated concurrently.
- A cycle on the active path matches `query.ErrCycle`. Sharing without a cycle
  is allowed.
- A pattern with a `Rule` constraint on a result that has nodes but records no rule,
  because the parser was compiled without source information, matches
  `query.ErrNoSourceInfo`.
- `query.WithContext(ctx)` supplies a context; cancellation returns the error of
  the context. `query.WithLimits(pego.Limits{Budget: n})` bounds
  traversal, matching, path copies and result construction; exhaustion matches
  `pego.ErrLimit`. Zero limits are unlimited. A query inspects no source text, so
  its budget counts only traversal and result construction. The count is abstract
  and MAY change between versions.
- A query sees only the values that the result retains. Actions and discard
  operators remove punctuation and comments; a query does not recover them, interpret
  source positions or highlight.

## Validation and Errors

| Condition | Result |
|:--|:--|
| Schema export from a parser loaded from a compiled grammar | `pego.ErrSchemaUnavailable` |
| Invalid schema | An error from `Validate`, `query.Compile`, `query.Prepare` or `schema.Compare` |
| Pattern that cannot match any value the schema describes | An error from compilation |
| Cycle on the active path during evaluation | `query.ErrCycle` |
| Rule constraint on a result without source information | `query.ErrNoSourceInfo` |
| Cancelled context | The error of the context |
| Exhausted budget limit | `pego.ErrLimit` |

## Compatibility and Support

Schemas and queries are available only through the Go API of the engine. Generated
Go and TypeScript parsers do not expose them.

## Conformance

The schema, validation, diff and query tests of the repository establish the
behavior. Repeated export of one grammar is tested for byte equality.
