package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ornew/pego"
)

// schemaDiffOutput is the JSON document that pego schema diff -f json prints.
type schemaDiffOutput struct {
	Types []schemaTypeChangeOutput `json:"types"`
	Rules []schemaRuleChangeOutput `json:"rules"`
}

// schemaTypeChangeOutput is one type of a schema diff. Before and After are the entries in the two
// schemas; one of them is absent for an addition or a removal.
type schemaTypeChangeOutput struct {
	ID     string                `json:"id"`
	Kind   pego.SchemaChangeKind `json:"kind"`
	Before *pego.SchemaType      `json:"before,omitempty"`
	After  *pego.SchemaType      `json:"after,omitempty"`
}

// schemaRuleChangeOutput is one rule of a schema diff, with the same convention as schemaTypeChangeOutput.
type schemaRuleChangeOutput struct {
	ID     string                `json:"id"`
	Kind   pego.SchemaChangeKind `json:"kind"`
	Before *pego.SchemaRule      `json:"before,omitempty"`
	After  *pego.SchemaRule      `json:"after,omitempty"`
}

// schemaOptions returns the export options of the -all-rules flag.
func schemaOptions(allRules bool) []pego.SchemaOption {
	if allRules {
		return []pego.SchemaOption{pego.WithAllSchemaRules()}
	}
	return nil
}

// schemaCmd exports the output schema of a grammar, or compares two of them with pego schema diff.
func schemaCmd(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "diff" {
		return schemaDiffCmd(args[1:], stdout)
	}
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.pego, .json, or .pegoc)")
	start := fs.String("s", "", "start rule (default: the one saved in a .pegoc, otherwise the grammar's default start rule)")
	allRules := fs.Bool("all-rules", false, "include unreachable rules and all declared types, not only those the start rule uses")
	output := fs.String("o", "", "output file (default: standard output)")
	paths, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(paths) != 0 {
		return fmt.Errorf("schema: unexpected argument %q (the grammar is given with -g)", paths[0])
	}
	if *grammarPath == "" {
		return errors.New("-g is required")
	}
	p, err := loadParser(*grammarPath, *start, compileFlags{})
	if err != nil {
		return err
	}
	s, err := p.OutputSchema(schemaOptions(*allRules)...)
	if err != nil {
		return fmt.Errorf("schema: %s: %w", *grammarPath, err)
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if *output == "" {
		_, err = stdout.Write(out)
		return err
	}
	return os.WriteFile(*output, out, 0o644)
}

// loadSchemaOperand reads one operand of schema diff: a schema JSON file, which is validated as it
// is decoded, or a grammar, whose schema is exported with the start rule and -all-rules options.
func loadSchemaOperand(path, start string, allRules bool) (*pego.OutputSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isOutputSchema(data) {
		var s pego.OutputSchema
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return &s, nil
	}
	p, err := loadParser(path, start, compileFlags{})
	if err != nil {
		return nil, err
	}
	s, err := p.OutputSchema(schemaOptions(allRules)...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// isOutputSchema reports whether data is a JSON document with the format of an output schema.
// Grammars in JSON have no format member, and compiled grammars and sources are not JSON.
func isOutputSchema(data []byte) bool {
	var head struct {
		Format string `json:"format"`
	}
	return json.Unmarshal(data, &head) == nil && head.Format == "pego-output-schema"
}

func schemaDiffCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema diff", flag.ContinueOnError)
	format := fs.String("f", "text", "output format: text or json")
	start := fs.String("s", "", "start rule of grammar operands (default: the one saved in a .pegoc, otherwise the grammar's default start rule)")
	allRules := fs.Bool("all-rules", false, "for grammar operands, include unreachable rules and all declared types")
	paths, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(paths) != 2 {
		return errors.New("schema diff: expected two operands, each a grammar or a schema JSON file")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown format %q", *format)
	}
	before, err := loadSchemaOperand(paths[0], *start, *allRules)
	if err != nil {
		return err
	}
	after, err := loadSchemaOperand(paths[1], *start, *allRules)
	if err != nil {
		return err
	}
	diff, err := pego.DiffOutputSchemas(before, after)
	if err != nil {
		return err
	}
	if *format == "json" {
		out := schemaDiffOutput{Types: []schemaTypeChangeOutput{}, Rules: []schemaRuleChangeOutput{}}
		for _, c := range diff.Types {
			out.Types = append(out.Types, schemaTypeChangeOutput{ID: c.ID, Kind: c.Kind, Before: c.Before, After: c.After})
		}
		for _, c := range diff.Rules {
			out.Rules = append(out.Rules, schemaRuleChangeOutput{ID: c.ID, Kind: c.Kind, Before: c.Before, After: c.After})
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	if len(diff.Types)+len(diff.Rules) == 0 {
		_, err := fmt.Fprintln(stdout, "no changes")
		return err
	}
	for _, c := range diff.Types {
		fmt.Fprintf(stdout, "%s type %s\n", c.Kind, c.ID)
	}
	for _, c := range diff.Rules {
		fmt.Fprintf(stdout, "%s rule %s\n", c.Kind, c.ID)
	}
	return nil
}
