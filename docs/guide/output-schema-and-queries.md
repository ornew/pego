# Output Schemas and Structural Queries

`Parser.OutputSchema` describes the possible values of a checked grammar. A
structural query searches actual `Node` results. Together they let tools inspect
grammar shape and find nodes without generating code or serializing a parse
tree. These Go APIs are experimental; the schema JSON format is versioned.

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

func main() {
	p, err := pego.CompileSource("rule main = name\nrule name = [a-z]+ #atomic", "main")
	if err != nil { log.Fatal(err) }

	schema, err := p.OutputSchema()
	if err != nil { log.Fatal(err) }
	encoded, err := json.Marshal(schema)
	if err != nil { log.Fatal(err) }

	query, err := pego.CompileStructuralQuery(pego.QueryPattern{
			Type: "Match", Rule: "name", Capture: "name",
	}, schema)
	if err != nil { log.Fatal(err) }
	tree, err := p.Parse("abc")
	if err != nil { log.Fatal(err) }
	matches, err := query.Matches(tree)
	if err != nil { log.Fatal(err) }
	fmt.Println(len(encoded), len(matches))
}
```

The default schema includes the configured start rule, rules it calls, and the
types referenced by their output shapes. `p.OutputSchema(pego.WithAllSchemaRules())`
includes unreachable rules and declared types. `WithStart` changes the default
export root. Schema entries separate declared types, inferred rule shapes, and
reserved CST types; scalar, optional, list, union, and dynamic `any` shapes are
explicit. Fields and rules have stable ordering, so repeated exports of the
same checked grammar produce identical JSON bytes. Version-1 JSON decoding
requires exact field-name case and rejects unknown fields and duplicate keys
at every object level. It also rejects unsupported formats and versions and
incomplete envelopes. Type names alone do not establish compatibility between
grammars.

`schema.Validate()` performs full version-1 schema validation whether the schema
was decoded from JSON or constructed in Go. It checks unique nonempty type IDs,
rule names, and field names; consistent identities and origins; supported shape
kinds, scalar names, and members; required list/optional elements; resolved
references; and acyclic inline shapes while allowing named-reference cycles.
The `Start` value must name a rule in `Rules`. Each rule's `Result` must be a
reference exactly to `inferred/<rule-name>`, and that type entry must exist.
The `cst/` namespace is reserved for built-in CST identities and shapes. When
validating those reserved shapes, omitted, nil, and empty optional shape slices
are treated equivalently. `CompileStructuralQuery` validates a supplied schema
before using it. This checks schema structure and canonical relationships; it
does not prove source or binary compatibility between grammars.

`pego.DiffOutputSchemas(before, after)` validates both complete operands, then
compares version-1 schema contents by type ID and rule name. It returns sorted
added, removed, and changed entries; unchanged schemas have empty `Types` and
`Rules` slices:

```go
diff, err := pego.DiffOutputSchemas(before, after)
if err != nil {
    log.Fatal(err)
}
for _, change := range diff.Types {
    fmt.Println(change.ID, change.Kind)
}
```

The comparison ignores type/rule entry order, struct-field order, union
alternative order, and envelope fields such as `Start`; both operands must still
have valid envelope values. Child order and named references remain significant.
A diff is descriptive only and does not classify compatibility.

Export requires checked grammar metadata. A parser restored by `LoadParser`
cannot export it, even if its compiled file includes the grammar AST; the error
matches `pego.ErrSchemaUnavailable`. Store the schema separately if a compiled
parser's consumer needs it later.

`StructuralQuery.Matches` accepts a `*Node` root. It evaluates patterns at node
occurrences in that graph; scalar values and lists cannot be query roots.
`QueryPattern` can constrain `Type`, producing `Rule`, and runtime `Category`
(`node`, `int`, `string`, `bool`, `list`, or `nil`). Scalar and `list` categories
apply to nested field-value patterns; `nil` can also match a nil child.
Here `list` means an explicit `[]any` field value. An ordinary parser repetition
or typed list field is a `Node` with type `List`, so match it with `Type: "List"`
and, if useful, `Category: "node"`. Export includes the reserved `cst/List`
identity whenever a reachable schema shape is a list, including empty lists.
`Fields` require named values; `Children` are an ordered prefix, so extra
children and fields are allowed.
`Capture` binds the value at that pattern. Patterns are copied at compilation.
Supplying a schema rejects known impossible type, rule, field, and some scalar
category constraints. For a directly declared optional scalar field such as
`Value *string`, compilation permits `string` and `nil` categories and rejects
other known categories; it follows aliases that resolve to scalar or optional
scalar shapes. Union and nested schema compatibility are not checked yet. A nil
schema defers these checks to evaluation. Runtime values are always checked
because callers can mutate a `Node` after parsing. This API uses programmatic
patterns; it has no text query language or text predicates yet.

Matches visit each node occurrence before outgoing edges, then children by
index, fields by name, and nested `[]any` elements by index. A shared node can
therefore produce multiple matches. Each `QueryCapture` includes its immediate
containing `Parent` and an owned, root-relative `Path` of child, field, and list
edges. Captured values and parent nodes still belong to the input graph. Keep
that graph stable while using results; do not mutate it concurrently. A
`Document` consumer can materialize a stable graph before querying it.

`Matches` returns all matches or an error. Active-path node or list cycles match
`pego.ErrQueryCycle`; sharing without a cycle is allowed. Use
`pego.WithQueryContext(ctx)` for cancellation and
`pego.WithQueryWorkLimits(pego.WorkLimits{MaxSteps: n})` to bound traversal,
matching, path copies, and result construction. Cancellation returns the context
error; exhaustion matches `pego.ErrWorkLimit`. Zero limits mean unlimited work.
Structural queries inspect no source text, so `MaxInputWork` is unused. The
budget is an abstract work count and can change with implementation versions.

Queries see only values retained in the `Node` graph. Actions and discard
operators can remove punctuation and comments. Queries do not recover those
tokens, interpret source positions, or provide highlighting. Generated parser
runtimes and TypeScript do not yet expose this query API.

For the schema's shape model and query design rationale, see
[design 038](../design/038-structured-output-schema.md) and
[design 040](../design/040-structural-queries-and-highlighting.md).
