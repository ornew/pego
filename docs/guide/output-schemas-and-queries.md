# Output Schemas and Structural Queries

`Parser.OutputSchema` describes the possible values of a checked grammar. By default the schema holds the start rule, the
rules it calls and the types their output references; `zeroparse.WithAllSchemaRules()` also includes unreachable rules and all
declared types. A structural query searches the values of an
actual parse result. Together they let a tool inspect the shape of a grammar and find values without generating code or
serializing a result. Structural queries are in package `github.com/ornew/zeroparse/query`. These Go APIs are experimental; the schema JSON format is versioned. The contract is defined in
[Output Schemas and Structural Queries](../../spec/runtime/schemas-and-queries.md).

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/query"
)

func main() {
	p, err := zeroparse.CompileSource("@start\nrule main = name\nrule name = [a-z]+ #atomic", "main")
	if err != nil {
		log.Fatal(err)
	}

	out, err := p.OutputSchema()
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		log.Fatal(err)
	}

	q, err := query.Compile(query.Pattern{
		Category: "node", Value: &query.Pattern{Category: "string"}, Capture: "name",
	}, out)
	if err != nil {
		log.Fatal(err)
	}
	tree, err := p.Parse("abc")
	if err != nil {
		log.Fatal(err)
	}
	matches, err := q.Matches(tree)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(encoded), len(matches))
}
```

To compile many queries against one schema, validate it once with `query.Prepare(out)` and compile with
the result's `Compile(pattern)`. With a 128-type schema, compiling a query takes about 80 µs without
preparation and under 1 µs with it; preparing takes about as long as one unprepared compilation.

```go
prepared, err := query.Prepare(out)
if err != nil {
	return err
}
q, err := prepared.Compile(query.Pattern{
	Category: "node", Value: &query.Pattern{Category: "string"}, Capture: "name",
})
```

`schema.Compare(before, after)` in package `github.com/ornew/zeroparse/schema` compares two schemas and returns the added,
removed and changed entries, for example to review how a grammar change alters its output:

```go
diff, err := schema.Compare(before, after)
if err != nil {
	return err
}
for _, change := range diff.Types {
	fmt.Println(change.ID, change.Kind, change.Class)
}
```

Each change also has a `Class`, which tells whether an existing reader of the output can break. A change is
`schema.Breaking` when code written against the generated types may stop compiling or a value may stop being handled:
every removal, and every change to an existing shape, including narrowing a type or making a value optional or
required. It is `schema.Compatible` when it only adds something a reader can ignore: a type, a rule, a struct field or
a capture. `change.Impacts` lists the reasons, each with a path into the shape and the name of the rule that produced
it, such as `field-removed` at `$.fields.Old`.

## The Command Line

`zp schema` prints the schema of a grammar as JSON, and `zp schema diff` compares two schemas. An operand of the
diff is a schema file or a grammar, so a schema saved with `-o` can be compared with the grammar it came from:

```bash
zp schema -g pairs.zp -o before.json
zp schema diff before.json pairs.zp
zp schema diff -all-rules -f json before.json pairs.zp
```

See [the `zp` command](command-line.md#schema) for the flags.

Keep in mind:

- The schema comes from the grammar source. A parser restored by `LoadParser` holds no schema in any mode, and its
  `OutputSchema` returns an error that matches `zeroparse.ErrSchemaUnavailable`; `zp schema` rejects a `.zpc`.
- A query sees only what the tree retains. Actions and discard operators remove punctuation and comments, and a query
  does not recover them or interpret source positions.
- `Elements` sees through nodes: on a node it matches the elements of the node's value, as `x[i]` does in an action.
  `Value` matches the value itself.
- A `Rule` constraint needs a parser compiled with source information. Against a schema without it, the query does not
  compile.
- Generated parsers and TypeScript do not expose this API.

## Related Topics

- [Reading Trees](reading-trees.md): reading the nodes that a query returns.
- [Generated Parsers](../../spec/runtime/generated-parsers.md): the typed values that generated Go parsers return.
