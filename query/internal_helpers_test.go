package query

import "github.com/ornew/pego/schema"

// validInMemoryOutputSchema is a minimal valid schema built in memory. Package schema's tests
// have the same helper; this copy serves the query tests here.
func validInMemoryOutputSchema() schema.Output {
	return schema.Output{
		Format: schema.Format, Version: schema.Version, Start: "main", PositionUnitIndependent: true,
		Types: []schema.Type{
			{ID: "declared/Item", Name: "Item", Origin: "declared", Shape: schema.Shape{Kind: "struct"}},
			{ID: "inferred/main", Name: "main", Origin: "inferred", Shape: schema.Shape{Kind: "ref", Ref: "declared/Item"}},
		},
		Rules: []schema.Rule{{Name: "main", Origin: "declared", Result: schema.Shape{Kind: "ref", Ref: "inferred/main"}}},
	}
}
