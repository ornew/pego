package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ornew/pego/sample"
)

// sampleStderr receives the coverage report of pego sample -coverage in the lines format, and
// warnings. Tests replace it.
var sampleStderr io.Writer = os.Stderr

// sampleOutput is the JSON document that pego sample -f json prints.
type sampleOutput struct {
	Start    string           `json:"start"`
	Seed     uint64           `json:"seed"`
	Inputs   []string         `json:"inputs,omitempty"`
	Invalid  []invalidOutput  `json:"invalid,omitempty"`
	Coverage *sample.Coverage `json:"coverage,omitempty"`
}

type invalidOutput struct {
	Input    string `json:"input"`
	Base     string `json:"base"`
	Mutation string `json:"mutation"`
	Error    string `json:"error"`
}

func sampleCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sample", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.pego, .json, or .pegoc with the AST)")
	start := fs.String("s", "", "start rule (default: the one saved in a .pegoc, otherwise main)")
	n := fs.Int("n", 10, "number of distinct inputs to generate")
	seed := fs.Uint64("seed", 0, "seed of the random decisions; the same seed gives the same inputs")
	maxDepth := fs.Int("max-depth", sample.DefaultMaxDepth, "recursion depth beyond which the generator finishes the input the shortest way")
	maxRepeat := fs.Int("max-repeat", sample.DefaultMaxRepeat, "iterations beyond the minimum that a repetition aims for, at most")
	maxLen := fs.Int("max-len", sample.DefaultMaxLen, "soft limit on the length of an input in bytes")
	budget := fs.Int("budget", sample.DefaultBudget, "steps one attempt may take before it gives up")
	coverage := fs.Bool("coverage", false, "prefer rules and alternatives not exercised yet, and report the coverage")
	invalid := fs.Bool("invalid", false, "generate near-miss inputs that the grammar rejects instead")
	format := fs.String("f", "lines", "output format: lines (one quoted input per line) or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *grammarPath == "" {
		return fmt.Errorf("-g is required")
	}
	if *n < 0 {
		return fmt.Errorf("-n must not be negative")
	}
	if *format != "lines" && *format != "json" {
		return fmt.Errorf("unknown format %q", *format)
	}
	p, err := loadParser(*grammarPath, *start)
	if err != nil {
		return err
	}
	opts := []sample.Option{
		sample.WithSeed(*seed),
		sample.WithMaxDepth(*maxDepth),
		sample.WithMaxRepeat(*maxRepeat),
		sample.WithMaxLen(*maxLen),
		sample.WithBudget(*budget),
	}
	if *coverage {
		opts = append(opts, sample.WithCoverage())
	}
	g, err := sample.New(p, opts...)
	if err != nil {
		return fmt.Errorf("%s: %w", *grammarPath, err)
	}
	out := sampleOutput{Start: p.Start(), Seed: *seed}
	var lines []string
	// When nothing is found, the coverage report is still printed (it shows how far generation got),
	// and the error is returned at the end.
	var genErr error
	if *invalid {
		var invs []sample.Invalid
		invs, genErr = g.GenerateInvalid(*n)
		for _, inv := range invs {
			out.Invalid = append(out.Invalid, invalidOutput{inv.Input, inv.Base, inv.Mutation, inv.Err.Error()})
			lines = append(lines, inv.Input)
		}
	} else {
		out.Inputs, genErr = g.Generate(*n)
		lines = out.Inputs
	}
	if genErr == nil && len(lines) < *n {
		fmt.Fprintf(sampleStderr, "pego sample: found only %d distinct inputs\n", len(lines))
	}
	var c sample.Coverage
	if *coverage {
		c = g.Coverage()
		out.Coverage = &c
	}
	if *format == "json" {
		if genErr != nil && !*coverage {
			return genErr
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
		return genErr
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(stdout, strconv.Quote(l)); err != nil {
			return err
		}
	}
	if *coverage {
		fmt.Fprint(sampleStderr, c.Report())
	}
	return genErr
}
