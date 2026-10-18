# Output Schemas and Structural Queries

`Parser.OutputSchema` describes the possible values of a checked grammar. A
structural query searches the nodes of an actual parse result. Together they let a
tool inspect the shape of a grammar and find nodes without generating code or
serializing a tree. This chapter defines both.

> The Go APIs in this chapter are experimental and MAY change incompatibly. The
> schema JSON format is versioned; this chapter defines version 1.

## Scope and Terminology

A **schema** is a description of the output of a grammar: its declared types, the
inferred result shape of each rule and the reserved types of the concrete syntax
tree. A **pattern** (`QueryPattern`) is a programmatic description of a node and of
the values reachable from it. A **query** is a compiled pattern, evaluated against a
`*Node` graph. A **match** is one occurrence of a node in the graph that satisfies
the pattern, together with its captures.

## Contract

### Output schemas

`p.OutputSchema(opts...)` returns the schema of the parser's grammar.

- The default schema contains the configured start rule, the rules it calls and the
  types that their output shapes reference. `WithAllSchemaRules()` also includes
  unreachable rules and all declared types. `WithStart` changes the default root.
- Schema entries distinguish declared types, inferred rule shapes and the reserved
  types of the concrete syntax tree. Scalar, optional, list, union and dynamic `any`
  shapes are explicit.
- Entries, fields and rules have a stable order, so repeated exports of one checked
  grammar encode to identical JSON bytes.
- Export includes the reserved `cst/List` identity whenever a reachable shape is a
  list, and every reserved concrete syntax tree identity whenever a reachable shape
  is a generic node, a terminal or the dynamic `any`. It includes `Error` when the
  grammar uses `#recover` and `Missing` when it uses `#missing`.
- Export needs checked grammar metadata. A parser restored by `LoadParser` cannot
  export a schema even if its file holds the AST; the error matches
  `pego.ErrSchemaUnavailable`. A schema is not recoverable from a loaded parser.

### Schema JSON, version 1

Decoding requires exact field-name case and rejects unknown fields, duplicate keys at
every object level, unsupported formats and versions and incomplete envelopes.

`schema.Validate()` performs the full version 1 validation of a schema, whether it
was decoded from JSON or built in Go. It checks that:

- type IDs, rule names and field names are nonempty and unique;
- identities and origins are consistent;
- shape kinds, scalar names and members are supported;
- list and optional shapes have their element;
- references resolve, and inline shapes are acyclic (cycles through named references
  are allowed);
- `Start` names a rule of `Rules`, and the `Result` of each rule is a reference
  exactly to `inferred/<rule-name>`, which exists as a type entry;
- the `cst/` namespace is used only for the built-in identities and shapes. When
  validating reserved shapes, omitted, nil and empty optional shape slices are
  equivalent.

Validation checks structure and canonical relationships. Type names alone do not
establish compatibility between grammars.

### Comparing schemas

`pego.DiffOutputSchemas(before, after)` validates both complete operands and
compares their contents by type ID and rule name. It returns the added, removed and
changed entries in sorted order; for equal schemas its `Types` and `Rules` slices are
empty. The comparison ignores the order of entries, of struct fields and of union
alternatives and envelope fields such as `Start`; child order and named references
are significant. A diff is descriptive: it does not classify compatibility.

### Patterns

`QueryPattern` constrains a node and the values reachable from it.

| Member | Constrains |
|:--|:--|
| `Type` | The type name of the node |
| `Rule` | The rule that produced the node |
| `Category` | The runtime category of a value: `node`, `int`, `string`, `bool`, `list` or `nil` |
| `Fields` | Named values that MUST be present; each is a pattern |
| `Children` | An ordered prefix of the children; extra children and fields are allowed |
| `Capture` | Binds the value at this pattern under the given name |

- `list` means an explicit `[]any` field value. A repetition or a typed list field is
  a `Node` of type `List`; match it with `Type: "List"`; `Category: "node"` is accepted
  for it. The scalar categories and `list` apply to nested field-value patterns;
  `nil` MAY also match a nil child.
- Patterns are copied when compiled. There is no text query language and there are
  no text predicates.

### Compiling queries

`pego.CompileStructuralQuery(pattern, schema)` compiles a query. A supplied schema is
validated first, and the compilation rejects patterns that no described value can
match: unknown type and rule names, fields that a type does not have and categories
or types that a value cannot have. Nested patterns are checked against the shape of
the field or child to which they apply.

- A union or optional shape accepts a pattern that matches any one of its
  alternatives. With `Value *(Int | Pair)`, a `Value` pattern of type `Int`, of type
  `Pair` with `Pair` fields or of category `nil` compiles, while one of type `Box` or
  category `string` does not.
- A pattern applied to a value of unknown shape (`any`, a generic node or the query
  root) is checked only against its own type's fields.
- `Error` and `Missing` nodes MAY replace any node or list, so patterns of those
  types, with their string fields `message` (and, for `Missing`, `expected`), compile
  wherever the schema includes them.
- For an optional scalar field such as `Value *string`, the categories `string` and
  `nil` compile and other known categories do not.
- A nil schema defers these checks to evaluation. Evaluation always checks the
  runtime values, because a caller MAY mutate a `Node` after parsing.

`pego.PrepareQuerySchema(schema)` validates a schema once and returns a prepared
schema whose `CompileStructuralQuery(pattern)` performs the same pattern checks
without validating again. A prepared schema owns a copy of the schema and is safe for
concurrent use.

### Evaluation

`StructuralQuery.Matches(root, opts...)` accepts a `*Node` root and returns all
matches or an error. Scalars and lists cannot be roots.

- Matches visit each node occurrence before its outgoing edges, then children by
  index, fields by name and nested `[]any` elements by index. A shared node can
  produce several matches.
- Each `QueryCapture` holds the captured value, its immediate containing `Parent`
  and an owned `Path` relative to the root, made of child, field and list edges.
  Captured values and parents belong to the input graph. The graph MUST be stable
  while results are used and MUST NOT be mutated concurrently.
- A node or list cycle on the active path matches `pego.ErrQueryCycle`. Sharing
  without a cycle is allowed.
- `pego.WithQueryContext(ctx)` supplies a context; cancellation returns the error of
  the context. `pego.WithQueryWorkLimits(pego.WorkLimits{MaxSteps: n})` bounds
  traversal, matching, path copies and result construction; exhaustion matches
  `pego.ErrWorkLimit`. Zero limits are unlimited. `MaxInputWork` is unused, because a
  query inspects no source text. The step count is abstract and MAY change between
  versions.
- A query sees only the values that the `Node` graph retains. Actions and discard
  operators remove punctuation and comments; a query does not recover them, interpret
  source positions or highlight.

## Validation and Errors

| Condition | Result |
|:--|:--|
| Schema export from a loaded parser | `pego.ErrSchemaUnavailable` |
| Invalid schema | An error from `Validate`, `CompileStructuralQuery`, `PrepareQuerySchema` or `DiffOutputSchemas` |
| Pattern that cannot match any value the schema describes | An error from compilation |
| Cycle on the active path during evaluation | `pego.ErrQueryCycle` |
| Cancelled context | The error of the context |
| Exhausted work limit | `pego.ErrWorkLimit` |

## Compatibility and Support

Schemas and queries are available only through the Go API of the engine. Generated
Go and TypeScript parsers do not expose them.

## Conformance

The schema, validation, diff and query tests of the repository establish the
behavior. Repeated export of one grammar is tested for byte equality.
