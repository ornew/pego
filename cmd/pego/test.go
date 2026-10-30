package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ornew/pego/internal/fixture"
)

func testCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	path := fs.String("f", "", "fixture suite JSON file")
	format := fs.String("format", "text", "report format: text or json")
	backendNames := fs.String("backend", "", "comma-separated execution backends (default: those in the suite)")
	equivalence := fs.Bool("equivalence", false, "compare selected backends independently of fixture expectations")
	update := fs.Bool("update", false, "update node and location goldens for one selected backend")
	var lim limitFlags
	lim.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("test: unexpected argument %q", fs.Arg(0))
	}
	if *path == "" {
		return errors.New("test: -f is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("test: unknown format %q", *format)
	}
	var backends []string
	if *backendNames != "" {
		for _, name := range strings.Split(*backendNames, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				return errors.New("test: -backend contains an empty backend name")
			}
			backends = append(backends, name)
		}
	}
	if *update && len(backends) != 1 {
		return errors.New("test: -update requires exactly one explicit -backend")
	}
	suite, err := fixture.Load(*path)
	if err != nil {
		return err
	}
	options := fixture.RunOptions{Backends: backends, Limits: lim.limits()}
	if isFlagSet(fs, "equivalence") {
		options.Equivalence = equivalence
	}
	if *update {
		if isFlagSet(fs, "equivalence") && *equivalence {
			return errors.New("test: -update cannot compare backend equivalence with one selected backend")
		}
		noEquivalence := false
		options.Equivalence = &noEquivalence
	}
	report, err := fixture.Run(suite, options)
	if err != nil {
		return err
	}
	if *update {
		if err := fixture.UpdateGoldens(suite, report, backends[0]); err != nil {
			return err
		}
		suite, err = fixture.Load(*path)
		if err != nil {
			return err
		}
		report, err = fixture.Run(suite, options)
		if err != nil {
			return err
		}
	}
	if *format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else if err := writeFixtureText(stdout, report); err != nil {
		return err
	}
	if !report.Passed {
		return errors.New("fixture failures")
	}
	return nil
}

func writeFixtureText(w io.Writer, report fixture.Report) error {
	for _, result := range report.Cases {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		if _, err := fmt.Fprintf(w, "%s %s [%s]\n", status, result.Name, result.Backend); err != nil {
			return err
		}
		if result.Actual.Error != "" {
			if _, err := fmt.Fprintf(w, "  runtime error: %s\n", result.Actual.Error); err != nil {
				return err
			}
		}
		for _, mismatch := range result.Mismatch {
			if _, err := fmt.Fprintf(w, "  %s mismatch\n", mismatch.Field); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    expected: %s\n    actual:   %s\n", indent(mismatch.Expected, "      "), indent(mismatch.Actual, "      ")); err != nil {
				return err
			}
		}
	}
	for _, result := range report.Equivalence {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		if _, err := fmt.Fprintf(w, "%s %s [backend equivalence: %s]", status, result.Name, strings.Join(result.Backends, ", ")); err != nil {
			return err
		}
		if len(result.Fields) > 0 {
			if _, err := fmt.Fprintf(w, " differs: %s", strings.Join(result.Fields, ", ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%s\n", map[bool]string{true: "all fixtures passed", false: "fixture failures"}[report.Passed]); err != nil {
		return err
	}
	return nil
}

func indent(s, prefix string) string {
	if s == "" {
		return "<empty>"
	}
	var b bytes.Buffer
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(prefix)
		b.WriteString(line)
	}
	return b.String()
}
