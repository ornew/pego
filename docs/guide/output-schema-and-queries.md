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
same checked grammar produce identical JSON bytes. A decoder rejects other
schema formats and versions, unknown JSON keys, and incomplete envelopes.
Type names alone do not establish compatibility
between grammars.

Decoding also performs limited structural checks for version 1: type IDs, rule
names, and field names must be unique and nonempty; type identities and origins
must agree; shape kinds, scalar names, and kind-specific members must be valid;
list and optional shapes need elements; and references must resolve to IDs in
the schema's type table. Recursive named references are allowed. This is not
full semantic validation: empty unions are not currently rejected. Passing a
schema constructed directly in Go to
`CompileStructuralQuery` does not run this whole-graph check first. Schema
diffing is not available.

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
is a `Node` with type `List`, so match it with `Type: "List"` and, if useful,
`Category: "node"`. `Fields` require named values; `Children` are an ordered
prefix, so extra children and fields are allowed.
`Capture` binds the value at that pattern. Patterns are copied at compilation.
Supplying a schema rejects known impossible type, rule, field, and some scalar
category constraints. For a directly declared optional scalar field such as
`Value *string`, compilation permits `string` and `nil` categories and rejects
other known categories. This check does not yet follow aliases or unions, or
check nested schema compatibility. A nil schema defers these checks to
evaluation. Runtime values are always checked because callers can mutate a
`Node` after parsing. This API uses programmatic patterns; it has no text query
language or text predicates yet.

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
