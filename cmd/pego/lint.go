package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ornew/pego"
)

// lintOutput is the JSON document that pego lint -f json prints.
type lintOutput struct {
	Grammar  string        `json:"grammar"`
	Start    string        `json:"start"`
	Findings []lintFinding `json:"findings"`
}

type lintFinding struct {
	Line     int           `json:"line,omitempty"`
	Col      int           `json:"col,omitempty"`
	Severity pego.Severity `json:"severity"`
	Check    string        `json:"check"`
	Rule     string        `json:"rule,omitempty"`
	Message  string        `json:"message"`
	Fix      string        `json:"fix,omitempty"`
}

// checkList is a flag that collects check names, given comma-separated or in several flags.
type checkList []string

func (l *checkList) String() string { return strings.Join(*l, ",") }

func (l *checkList) Set(s string) error {
	for _, name := range strings.Split(s, ",") {
		if name = strings.TrimSpace(name); name != "" {
			*l = append(*l, name)
		}
	}
	return nil
}

func lintCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.pego, .json, or .pegoc with the AST)")
	start := fs.String("s", "", "start rule (default: the one saved in a .pegoc, otherwise main)")
	format := fs.String("f", "text", "output format: text or json")
	strict := fs.Bool("strict", false, "fail on warnings too, not only on errors")
	list := fs.Bool("list", false, "list the checks and exit")
	var disable checkList
	fs.Var(&disable, "disable", "comma-separated checks not to run (the flag can be repeated)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *list {
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, c := range pego.LintChecks() {
			fmt.Fprintf(w, "%s\t%s\t%s\n", c.Name, c.Severity, c.Summary)
		}
		return w.Flush()
	}
	if *grammarPath == "" {
		return errors.New("-g is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown format %q", *format)
	}
	g, saved, err := loadGrammar(*grammarPath)
	if err != nil {
		return err
	}
	*start = cmp.Or(*start, saved, "main")
	findings, err := pego.Lint(g, *start, pego.DisableChecks(disable...))
	if err != nil {
		return fileError(*grammarPath, err)
	}
	counts := map[pego.Severity]int{}
	out := lintOutput{Grammar: *grammarPath, Start: *start, Findings: []lintFinding{}}
	for _, f := range findings {
		counts[f.Severity]++
		out.Findings = append(out.Findings, lintFinding{f.Pos.Line, f.Pos.Col, f.Severity, f.Check, f.Rule, f.Message, f.Fix})
	}
	if *format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		for _, f := range findings {
			where := *grammarPath
			if f.Pos.IsValid() {
				where = fmt.Sprintf("%s:%d:%d", where, f.Pos.Line, f.Pos.Col)
			}
			fmt.Fprintf(stdout, "%s: %s: %s [%s]\n", where, f.Severity, f.Message, f.Check)
			if f.Fix != "" {
				fmt.Fprintf(stdout, "\tfix: %s\n", f.Fix)
			}
		}
	}
	failed := counts[pego.SeverityError]
	if *strict {
		failed += counts[pego.SeverityWarning]
	}
	if failed > 0 {
		return fmt.Errorf("lint: %s, %s, %s", plural(counts[pego.SeverityError], "error"),
			plural(counts[pego.SeverityWarning], "warning"), plural(counts[pego.SeverityHint], "hint"))
	}
	return nil
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}
