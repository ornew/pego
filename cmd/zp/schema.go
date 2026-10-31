package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/schema"
)

// schemaDiffOutput is the JSON document that zp schema diff -f json prints.
type schemaDiffOutput struct {
	Types []schemaTypeChangeOutput `json:"types"`
	Rules []schemaRuleChangeOutput `json:"rules"`
}

// schemaTypeChangeOutput is one type of a schema diff. Before and After are the entries in the two
// schemas; one of them is absent for an addition or a removal.
type schemaTypeChangeOutput struct {
	ID      string               `json:"id"`
	Kind    schema.ChangeKind    `json:"kind"`
	Class   schema.Class         `json:"class"`
	Impacts []schemaImpactOutput `json:"impacts"`
	Before  *schema.Type         `json:"before,omitempty"`
	After   *schema.Type         `json:"after,omitempty"`
}

// schemaRuleChangeOutput is one rule of a schema diff, with the same convention as schemaTypeChangeOutput.
type schemaRuleChangeOutput struct {
	ID      string               `json:"id"`
	Kind    schema.ChangeKind    `json:"kind"`
	Class   schema.Class         `json:"class"`
	Impacts []schemaImpactOutput `json:"impacts"`
	Before  *schema.Rule         `json:"before,omitempty"`
	After   *schema.Rule         `json:"after,omitempty"`
}

// schemaImpactOutput is one reason for the class of a change: where in the entry, the class of that
// reason and the rule that produced it.
type schemaImpactOutput struct {
	Path  string       `json:"path"`
	Class schema.Class `json:"class"`
	Rule  string       `json:"rule"`
}

func schemaImpacts(impacts []schema.Impact) []schemaImpactOutput {
	out := make([]schemaImpactOutput, len(impacts))
	for i, im := range impacts {
		out[i] = schemaImpactOutput{Path: im.Path, Class: im.Class, Rule: im.Rule}
	}
	return out
}

// schemaImpactText lists the rules of impacts, with the path of those below the root.
func schemaImpactText(impacts []schema.Impact) string {
	parts := make([]string, len(impacts))
	for i, im := range impacts {
		parts[i] = im.Rule
		if im.Path != "$" {
			parts[i] += " at " + im.Path
		}
	}
	return strings.Join(parts, "; ")
}

// schemaOptions returns the export options of the -all-rules flag.
func schemaOptions(allRules bool) []zeroparse.SchemaOption {
	if allRules {
		return []zeroparse.SchemaOption{zeroparse.WithAllSchemaRules()}
	}
	return nil
}

// schemaCmd exports the output schema of a grammar, or compares two of them with zp schema diff.
func schemaCmd(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "diff" {
		return schemaDiffCmd(args[1:], stdout)
	}
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.zp or .json)")
	start := fs.String("s", "", "start rule (default: the grammar's default start rule)")
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
	p, err := loadSourceParser(*grammarPath, *start, "schema")
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
func loadSchemaOperand(path, start string, allRules bool) (*schema.Output, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isOutputSchema(data) {
		var s schema.Output
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return &s, nil
	}
	p, err := loadSourceParser(path, start, "schema diff")
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
	return json.Unmarshal(data, &head) == nil && head.Format == "zp-output-schema"
}

func schemaDiffCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema diff", flag.ContinueOnError)
	format := fs.String("f", "text", "output format: text or json")
	start := fs.String("s", "", "start rule of grammar operands (default: the grammar's default start rule)")
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
	diff, err := schema.Compare(before, after)
	if err != nil {
		return err
	}
	if *format == "json" {
		out := schemaDiffOutput{Types: []schemaTypeChangeOutput{}, Rules: []schemaRuleChangeOutput{}}
		for _, c := range diff.Types {
			out.Types = append(out.Types, schemaTypeChangeOutput{ID: c.ID, Kind: c.Kind, Class: c.Class, Impacts: schemaImpacts(c.Impacts), Before: c.Before, After: c.After})
		}
		for _, c := range diff.Rules {
			out.Rules = append(out.Rules, schemaRuleChangeOutput{ID: c.ID, Kind: c.Kind, Class: c.Class, Impacts: schemaImpacts(c.Impacts), Before: c.Before, After: c.After})
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
		fmt.Fprintf(stdout, "%s type %s: %s (%s)\n", c.Kind, c.ID, c.Class, schemaImpactText(c.Impacts))
	}
	for _, c := range diff.Rules {
		fmt.Fprintf(stdout, "%s rule %s: %s (%s)\n", c.Kind, c.ID, c.Class, schemaImpactText(c.Impacts))
	}
	return nil
}
