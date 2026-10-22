# Output Schemas and Structural Queries

`Parser.OutputSchema` describes the possible values of a checked grammar. By default the schema holds the start rule, the
rules it calls and the types their output references; `pego.WithAllSchemaRules()` also includes unreachable rules and all
declared types. A structural query searches the values of an
actual parse result. Together they let a tool inspect the shape of a grammar and find values without generating code or
serializing a result. These Go APIs are experimental; the schema JSON format is versioned. The contract is defined in
[Output Schemas and Structural Queries](../../spec/schemas-and-queries.md).

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

func main() {
	p, err := pego.CompileSource("@start\nrule main = name\nrule name = [a-z]+ #atomic", "main")
	if err != nil {
		log.Fatal(err)
	}

	schema, err := p.OutputSchema()
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		log.Fatal(err)
	}

	query, err := pego.CompileStructuralQuery(pego.QueryPattern{
		Category: "node", Value: &pego.QueryPattern{Category: "string"}, Capture: "name",
	}, schema)
	if err != nil {
		log.Fatal(err)
	}
	tree, err := p.Parse("abc")
	if err != nil {
		log.Fatal(err)
	}
	matches, err := query.Matches(tree)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(encoded), len(matches))
}
```

To compile many queries against one schema, validate it once with `pego.PrepareQuerySchema(schema)` and compile with
the result's `CompileStructuralQuery(pattern)`. With a 128-type schema, compiling a query takes about 80 µs without
preparation and under 1 µs with it; preparing takes about as long as one unprepared compilation.

```go
prepared, err := pego.PrepareQuerySchema(schema)
if err != nil {
	return err
}
query, err := prepared.CompileStructuralQuery(pego.QueryPattern{
	Category: "node", Value: &pego.QueryPattern{Category: "string"}, Capture: "name",
})
```

`pego.DiffOutputSchemas(before, after)` compares two schemas and returns the added, removed and changed entries, for
example to review how a grammar change alters its output:

```go
diff, err := pego.DiffOutputSchemas(before, after)
if err != nil {
	return err
}
for _, change := range diff.Types {
	fmt.Println(change.ID, change.Kind)
}
```

## The Command Line

`pego schema` prints the schema of a grammar as JSON, and `pego schema diff` compares two schemas. An operand of the
diff is a schema file or a grammar, so a schema saved with `-o` can be compared with the grammar it came from:

```bash
pego schema -g pairs.pego -o before.json
pego schema diff before.json pairs.pego
pego schema diff -all-rules -f json before.json pairs.pego
```

See [the `pego` command](command-line.md#schema) for the flags.

Keep in mind:

- A parser restored by `LoadParser` cannot export a schema, even if its file holds the grammar AST. Store the schema
  separately if a consumer needs it later.
- A query sees only what the tree retains. Actions and discard operators remove punctuation and comments, and a query
  does not recover them or interpret source positions.
- `Elements` sees through nodes: on a node it matches the elements of the node's value, as `x[i]` does in an action.
  `Value` matches the value itself.
- A `Rule` constraint needs a parser compiled with source information. Against a schema without it, the query does not
  compile.
- Generated parsers and TypeScript do not expose this API.

## Related Topics

- [Reading Trees](reading-trees.md): reading the nodes that a query returns.
- [Generated Parsers](../../spec/generated-parsers.md): the typed values that generated Go parsers return.
