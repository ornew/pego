// Package fixture runs JSON-described examples against PEGO grammars.
package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

// FormatVersion is the only fixture format version understood by this package.
const FormatVersion = 1

// Suite is a fixture manifest. Grammar paths are relative to the manifest file.
type Suite struct {
	Version     int      `json:"version"`
	Grammar     string   `json:"grammar"`
	Start       string   `json:"start,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Backends    []string `json:"backends,omitempty"`
	Equivalence bool     `json:"equivalence,omitempty"`
	Cases       []Case   `json:"cases"`

	path    string
	grammar *grammar.Grammar
	source  []byte
}

// Case is one named input and its expected result. Expectation is shared by
// every backend unless ByBackend supplies an override for that backend.
type Case struct {
	Name      string                 `json:"name"`
	Input     string                 `json:"input"`
	Expect    Expectation            `json:"expect"`
	ByBackend map[string]Expectation `json:"byBackend,omitempty"`
	inputSet  bool
	expectSet bool
}

// UnmarshalJSON records whether input was supplied so an explicit empty input
// remains distinct from a missing field.
func (c *Case) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	rawInput, present := fields["input"]
	if !present || bytes.Equal(bytes.TrimSpace(rawInput), []byte("null")) {
		return errors.New("input is required and must be a string")
	}
	rawExpect, present := fields["expect"]
	trimmedExpect := bytes.TrimSpace(rawExpect)
	if !present || len(trimmedExpect) == 0 || trimmedExpect[0] != '{' {
		return errors.New("expect is required and must be an object")
	}
	if raw, ok := fields["byBackend"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("byBackend must not be null")
	}
	if raw, ok := fields["byBackend"]; ok {
		var overrides map[string]json.RawMessage
		if err := json.Unmarshal(raw, &overrides); err != nil {
			return fmt.Errorf("byBackend must be an object: %w", err)
		}
		for backend, value := range overrides {
			value = bytes.TrimSpace(value)
			if len(value) == 0 || value[0] != '{' {
				return fmt.Errorf("byBackend %q must be an expectation object", backend)
			}
		}
	}
	type caseAlias Case
	var decoded caseAlias
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return err
	}
	*c = Case(decoded)
	c.inputSet = true
	c.expectSet = true
	return nil
}

// Expectation contains the expected parse outcome and optional golden values.
type Expectation struct {
	Outcome  Outcome         `json:"outcome,omitempty"`
	Node     json.RawMessage `json:"node,omitempty"`
	Start    *int            `json:"start,omitempty"`
	End      *int            `json:"end,omitempty"`
	Errors   []ErrorLocation `json:"errors,omitempty"`
	Clear    []string        `json:"clear,omitempty"`
	clearSet bool
}

// UnmarshalJSON rejects null for typed expectation fields while preserving
// JSON null as a valid node golden.
func (e *Expectation) UnmarshalJSON(data []byte) error {
	type expectationAlias Expectation
	var decoded expectationAlias
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("expectation must be an object")
	}
	for _, key := range []string{"outcome", "start", "end", "errors", "clear"} {
		if raw, ok := fields[key]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("expectation %s must not be null", key)
		}
	}
	*e = Expectation(decoded)
	_, e.clearSet = fields["clear"]
	return nil
}

// MarshalJSON preserves an explicitly empty errors assertion as [] while
// omitting errors when it was not specified.
func (e Expectation) MarshalJSON() ([]byte, error) {
	type expectationAlias Expectation
	data, err := json.Marshal(expectationAlias(e))
	if err != nil || e.Errors == nil {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	errorsJSON, err := json.Marshal(e.Errors)
	if err != nil {
		return nil, err
	}
	fields["errors"] = errorsJSON
	return json.Marshal(fields)
}

// ErrorLocation identifies a syntax error in the suite's selected position unit.
type ErrorLocation struct {
	Pos    int `json:"pos"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// UnmarshalJSON requires all location coordinates and rejects null values.
func (e *ErrorLocation) UnmarshalJSON(data []byte) error {
	type locationAlias ErrorLocation
	var decoded locationAlias
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("error location must be an object")
	}
	for _, key := range []string{"pos", "line", "column"} {
		raw, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("error location %s is required and must be an integer", key)
		}
	}
	*e = ErrorLocation(decoded)
	return nil
}

// RunOptions overrides a suite's selected backends and equivalence setting,
// and supplies invocation-local cancellation and budget limits.
type RunOptions struct {
	Backends    []string
	Equivalence *bool
	Context     context.Context
	Limits      pego.Limits
}

// Outcome describes whether parsing matched cleanly, recovered, or rejected input.
type Outcome string

const (
	Accepted  Outcome = "accepted"
	Recovered Outcome = "recovered"
	Rejected  Outcome = "rejected"
)

// Actual is the observed result for one case/backend pair.
type Actual struct {
	Outcome Outcome         `json:"outcome,omitempty"`
	Node    json.RawMessage `json:"node,omitempty"`
	Start   *int            `json:"start,omitempty"`
	End     *int            `json:"end,omitempty"`
	Errors  []ErrorLocation `json:"errors,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Mismatch describes one failed expectation comparison.
type Mismatch struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// CaseResult is the deterministic result for one case/backend pair.
type CaseResult struct {
	Name     string     `json:"name"`
	Backend  string     `json:"backend"`
	Passed   bool       `json:"passed"`
	Actual   Actual     `json:"actual"`
	Mismatch []Mismatch `json:"mismatch,omitempty"`
}

// EquivalenceResult reports whether selected backends produced the same result.
type EquivalenceResult struct {
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Backends []string `json:"backends"`
	Fields   []string `json:"fields,omitempty"`
}

// Report contains fixture and optional backend-equivalence outcomes.
type Report struct {
	Passed      bool                `json:"passed"`
	Cases       []CaseResult        `json:"cases"`
	Equivalence []EquivalenceResult `json:"equivalence,omitempty"`
}

// Load reads and validates a suite and its PEGO-source or JSON grammar.
func Load(path string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := rejectNullTopLevel(data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var suite Suite
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&suite); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	suite.path = path
	suite.source = append([]byte(nil), data...)
	if suite.Version != FormatVersion {
		return nil, fmt.Errorf("%s: unsupported fixture version %d (supported: %d)", path, suite.Version, FormatVersion)
	}
	if suite.Grammar == "" {
		return nil, fmt.Errorf("%s: grammar is required", path)
	}
	if suite.Unit == "" {
		suite.Unit = "codepoints"
	}
	if suite.Unit != "codepoints" && suite.Unit != "bytes" {
		return nil, fmt.Errorf("%s: unknown unit %q", path, suite.Unit)
	}
	if len(suite.Cases) == 0 {
		return nil, fmt.Errorf("%s: cases must not be empty", path)
	}
	seen := make(map[string]bool, len(suite.Cases))
	for i := range suite.Cases {
		c := &suite.Cases[i]
		if c.Name == "" {
			return nil, fmt.Errorf("%s: case %d: name is required", path, i+1)
		}
		if !c.inputSet {
			return nil, fmt.Errorf("%s: case %q: input is required", path, c.Name)
		}
		if !c.expectSet {
			return nil, fmt.Errorf("%s: case %q: expect is required", path, c.Name)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("%s: duplicate case name %q", path, c.Name)
		}
		seen[c.Name] = true
		if c.Expect.Outcome == "" && len(c.ByBackend) == 0 {
			return nil, fmt.Errorf("%s: case %q: expect.outcome is required", path, c.Name)
		}
		if err := validateExpectation(c.Expect); err != nil {
			return nil, fmt.Errorf("%s: case %q: %w", path, c.Name, err)
		}
		if c.Expect.clearSet {
			return nil, fmt.Errorf("%s: case %q: clear is only valid in byBackend expectations", path, c.Name)
		}
		for backend, exp := range c.ByBackend {
			if !validBackend(backend) {
				return nil, fmt.Errorf("%s: case %q: unknown backend %q", path, c.Name, backend)
			}
			if err := validateExpectation(exp); err != nil {
				return nil, fmt.Errorf("%s: case %q backend %s: %w", path, c.Name, backend, err)
			}
		}
	}
	if len(suite.Backends) == 0 {
		suite.Backends = []string{"default"}
	}
	if _, err := normalizeBackends(suite.Backends); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, c := range suite.Cases {
		for _, backend := range suite.Backends {
			exp := c.Expect
			if override, ok := c.ByBackend[backend]; ok {
				exp = mergeExpectation(exp, override)
			}
			if exp.Outcome == "" {
				return nil, fmt.Errorf("%s: case %q backend %s: expect.outcome is required", path, c.Name, backend)
			}
			if err := validateExpectation(exp); err != nil {
				return nil, fmt.Errorf("%s: case %q backend %s: %w", path, c.Name, backend, err)
			}
		}
		for backend, override := range c.ByBackend {
			exp := mergeExpectation(c.Expect, override)
			if exp.Outcome == "" {
				return nil, fmt.Errorf("%s: case %q backend %s: expect.outcome is required", path, c.Name, backend)
			}
			if err := validateExpectation(exp); err != nil {
				return nil, fmt.Errorf("%s: case %q backend %s: %w", path, c.Name, backend, err)
			}
		}
	}
	grammarPath := suite.Grammar
	if !filepath.IsAbs(grammarPath) {
		grammarPath = filepath.Join(filepath.Dir(path), grammarPath)
	}
	grammarBytes, err := os.ReadFile(grammarPath)
	if err != nil {
		return nil, fmt.Errorf("%s: reading grammar: %w", path, err)
	}
	if strings.HasSuffix(grammarPath, ".json") {
		suite.grammar, err = grammar.UnmarshalJSON(grammarBytes)
	} else {
		suite.grammar, err = pego.ParseGrammar(string(grammarBytes))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: grammar %s: %w", path, suite.Grammar, err)
	}
	return &suite, nil
}

// Run executes the selected fixtures in manifest order. A backend override is
// used only when a case has an explicit expectation for that backend.
func Run(suite *Suite, opts RunOptions) (Report, error) {
	if suite == nil || suite.grammar == nil {
		return Report{}, errors.New("fixture: suite must be loaded with Load")
	}
	backends := suite.Backends
	if len(opts.Backends) > 0 {
		backends = opts.Backends
	}
	backends, err := normalizeBackends(backends)
	if err != nil {
		return Report{}, err
	}
	equivalence := suite.Equivalence
	if opts.Equivalence != nil {
		equivalence = *opts.Equivalence
	}
	if equivalence && len(backends) < 2 {
		return Report{}, errors.New("fixture: backend equivalence requires at least two selected backends")
	}
	var report Report
	if opts.Context != nil {
		if err := opts.Context.Err(); err != nil {
			return report, err
		}
	}
	unit := pego.CodePoints
	if suite.Unit == "bytes" {
		unit = pego.Bytes
	}
	parsers := make(map[string]*pego.Parser, len(backends))
	for _, backend := range backends {
		p, err := pego.Compile(suite.grammar, suite.Start)
		if err != nil {
			return Report{}, fmt.Errorf("compile grammar: %w", err)
		}
		parsers[backend] = p
	}
	for _, c := range suite.Cases {
		actuals := make([]Actual, len(backends))
		for bi, backend := range backends {
			if opts.Context != nil {
				if err := opts.Context.Err(); err != nil {
					return report, err
				}
			}
			actual, err := runCase(parsers[backend], backend, unit, c.Input, opts.Context, opts.Limits)
			if err != nil {
				return report, fmt.Errorf("case %q backend %s: %w", c.Name, backend, err)
			}
			actuals[bi] = actual
			exp := c.Expect
			if override, ok := c.ByBackend[backend]; ok {
				exp = mergeExpectation(exp, override)
			}
			if exp.Outcome == "" {
				return report, fmt.Errorf("fixture: case %q backend %s: expect.outcome is required", c.Name, backend)
			}
			result := CaseResult{Name: c.Name, Backend: backend, Actual: actual}
			result.Mismatch = compare(exp, actual)
			result.Passed = len(result.Mismatch) == 0
			report.Cases = append(report.Cases, result)
		}
		if equivalence && len(backends) > 1 {
			report.Equivalence = append(report.Equivalence, compareBackends(c.Name, backends, actuals))
		}
	}
	report.Passed = true
	for _, result := range report.Cases {
		report.Passed = report.Passed && result.Passed
	}
	for _, result := range report.Equivalence {
		report.Passed = report.Passed && result.Passed
	}
	return report, nil
}

// UpdateGoldens refreshes only node JSON and locations for a selected backend.
// It never changes fixture input or expected outcome.
func UpdateGoldens(suite *Suite, report Report, backend string) error {
	if suite == nil || suite.path == "" {
		return errors.New("fixture: update requires a suite loaded from a file")
	}
	if !validBackend(backend) {
		return fmt.Errorf("fixture: unknown backend %q", backend)
	}
	byName := make(map[string]Actual)
	for _, result := range report.Cases {
		if result.Backend == backend {
			if result.Actual.Error != "" {
				return fmt.Errorf("fixture: cannot update goldens after runtime error in case %q backend %q", result.Name, backend)
			}
			byName[result.Name] = result.Actual
		}
	}
	if len(byName) == 0 {
		return fmt.Errorf("fixture: report contains no results for backend %q", backend)
	}
	// Validate the complete selected outcome set before changing the suite in
	// memory or on disk. A later mismatch must not leave earlier goldens updated.
	for _, c := range suite.Cases {
		actual, ok := byName[c.Name]
		if !ok {
			return fmt.Errorf("fixture: report is missing case %q for backend %q", c.Name, backend)
		}
		exp := c.Expect
		if override, ok := c.ByBackend[backend]; ok {
			exp = mergeExpectation(exp, override)
		}
		if exp.Outcome == "" {
			return fmt.Errorf("fixture: case %q backend %q has no expected outcome", c.Name, backend)
		}
		if actual.Outcome != exp.Outcome {
			return fmt.Errorf("fixture: cannot update goldens for case %q backend %q: expected outcome %q, got %q", c.Name, backend, exp.Outcome, actual.Outcome)
		}
	}
	backendTargets := make([]bool, len(suite.Cases))
	for i := range suite.Cases {
		c := &suite.Cases[i]
		actual, ok := byName[c.Name]
		if !ok {
			return fmt.Errorf("fixture: report is missing case %q for backend %q", c.Name, backend)
		}
		backendTargets[i] = c.ByBackend != nil
		exp := c.Expect
		if backendTargets[i] {
			override := c.ByBackend[backend]
			updateBackendGoldens(c.Expect, &override, actual)
			c.ByBackend[backend] = override
			continue
		}
		setGoldens(&exp, actual)
		c.Expect = exp
	}
	data, err := json.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	data = preserveUntargetedNodeJSON(suite.source, data, suite.Cases, backend, backendTargets)
	data = append(data, '\n')
	info, err := os.Stat(suite.path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(suite.path), ".fixture-update-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, suite.path); err != nil {
		return err
	}
	suite.source = append(suite.source[:0], data...)
	return nil
}

func updateBackendGoldens(shared Expectation, override *Expectation, actual Actual) {
	override.Clear = slices.DeleteFunc(override.Clear, func(field string) bool {
		switch field {
		case "node":
			return len(actual.Node) > 0
		case "start":
			return actual.Start != nil
		case "end":
			return actual.End != nil
		case "errors":
			return len(actual.Errors) > 0 || override.Errors != nil
		default:
			return false
		}
	})
	if len(actual.Node) > 0 {
		override.Node = append(json.RawMessage(nil), actual.Node...)
	} else {
		override.Node = nil
		if len(shared.Node) > 0 {
			override.Clear = appendClear(override.Clear, "node")
		}
	}
	if actual.Start != nil {
		override.Start = intPointer(*actual.Start)
	} else {
		override.Start = nil
		if shared.Start != nil {
			override.Clear = appendClear(override.Clear, "start")
		}
	}
	if actual.End != nil {
		override.End = intPointer(*actual.End)
	} else {
		override.End = nil
		if shared.End != nil {
			override.Clear = appendClear(override.Clear, "end")
		}
	}
	if len(actual.Errors) == 0 && len(shared.Errors) > 0 && override.Errors == nil {
		override.Errors = nil
		override.Clear = appendClear(override.Clear, "errors")
	} else {
		override.Errors = make([]ErrorLocation, len(actual.Errors))
		copy(override.Errors, actual.Errors)
	}
}

func appendClear(clear []string, field string) []string {
	if slices.Contains(clear, field) {
		return clear
	}
	return append(clear, field)
}

func setGoldens(exp *Expectation, actual Actual) {
	exp.Node = append(json.RawMessage(nil), actual.Node...)
	if actual.Start == nil {
		exp.Start = nil
	} else {
		exp.Start = intPointer(*actual.Start)
	}
	if actual.End == nil {
		exp.End = nil
	} else {
		exp.End = intPointer(*actual.End)
	}
	exp.Errors = make([]ErrorLocation, len(actual.Errors))
	copy(exp.Errors, actual.Errors)
}

func runCase(p *pego.Parser, backend string, unit pego.PositionUnit, input string, ctx context.Context, limits pego.Limits) (Actual, error) {
	var b pego.Backend
	switch backend {
	case "default":
		b = pego.DefaultBackend
	case "closure":
		b = pego.Closure
	case "bytecode":
		b = pego.Bytecode
	case "bytecode-iterative":
		b = pego.BytecodeIterative
	}
	options := []pego.ParseOption{pego.WithExpectations(), pego.WithUnit(unit), pego.WithBackend(b), pego.WithLimits(limits)}
	if ctx != nil {
		options = append(options, pego.WithContext(ctx))
	}
	node, parseErr := p.Parse(input, options...)
	actual := Actual{}
	if node != nil {
		data, err := json.Marshal(node)
		if err != nil {
			return Actual{}, err
		}
		actual.Node = data
		actual.Start, actual.End = intPointer(int(node.Start())), intPointer(int(node.End()))
	}
	var many pego.SyntaxErrors
	var one *pego.SyntaxError
	switch {
	case parseErr == nil:
		actual.Outcome = Accepted
		if node == nil {
			actual.Node = json.RawMessage("null")
		}
	case errors.As(parseErr, &many):
		actual.Outcome = Recovered
		if node == nil {
			actual.Node = json.RawMessage("null")
		}
		for _, e := range many {
			actual.Errors = append(actual.Errors, errorLocation(e))
		}
	case errors.As(parseErr, &one):
		actual.Outcome = Rejected
		actual.Errors = append(actual.Errors, errorLocation(one))
	default:
		if errors.Is(parseErr, context.Canceled) || errors.Is(parseErr, context.DeadlineExceeded) {
			return actual, parseErr
		}
		actual.Error = parseErr.Error()
	}
	return actual, nil
}

func errorLocation(e *pego.SyntaxError) ErrorLocation {
	return ErrorLocation{Pos: e.Pos, Line: e.Line, Column: e.Col}
}

func compare(exp Expectation, actual Actual) []Mismatch {
	var out []Mismatch
	if actual.Error != "" {
		out = append(out, Mismatch{Field: "runtimeError", Expected: "<none>", Actual: "<present>"})
	}
	if exp.Outcome == "" {
		out = append(out, Mismatch{Field: "outcome", Expected: "<required>", Actual: string(actual.Outcome)})
	} else if exp.Outcome != actual.Outcome {
		got := string(actual.Outcome)
		if actual.Error != "" {
			got = "<runtime error>"
		}
		out = append(out, Mismatch{Field: "outcome", Expected: string(exp.Outcome), Actual: got})
	}
	if len(exp.Node) > 0 {
		want, err := canonicalJSON(exp.Node)
		if err != nil {
			out = append(out, Mismatch{Field: "node", Expected: string(exp.Node), Actual: "<invalid expected JSON>"})
		} else if got, _ := canonicalJSON(actual.Node); !bytes.Equal(want, got) {
			out = append(out, Mismatch{Field: "node", Expected: prettyJSON(want), Actual: prettyJSON(got)})
		}
	}
	compareInt(&out, "start", exp.Start, actual.Start)
	compareInt(&out, "end", exp.End, actual.End)
	if exp.Errors != nil && !slices.Equal(exp.Errors, actual.Errors) {
		want, _ := json.Marshal(exp.Errors)
		got, _ := json.Marshal(actual.Errors)
		out = append(out, Mismatch{Field: "errors", Expected: string(want), Actual: string(got)})
	}
	return out
}

func compareInt(out *[]Mismatch, field string, expected, actual *int) {
	if expected == nil {
		return
	}
	want := fmt.Sprint(*expected)
	got := "<absent>"
	if actual != nil {
		got = fmt.Sprint(*actual)
	}
	if actual == nil || *expected != *actual {
		*out = append(*out, Mismatch{Field: field, Expected: want, Actual: got})
	}
}

func compareBackends(name string, backends []string, actuals []Actual) EquivalenceResult {
	result := EquivalenceResult{Name: name, Passed: true, Backends: append([]string(nil), backends...)}
	if len(actuals) < 2 {
		return result
	}
	base := actuals[0]
	for _, actual := range actuals[1:] {
		if base.Outcome != actual.Outcome {
			result.Fields = appendField(result.Fields, "outcome")
		}
		if (base.Error != "") != (actual.Error != "") {
			result.Fields = appendField(result.Fields, "runtimeError")
		}
		baseNode, _ := canonicalJSON(base.Node)
		otherNode, _ := canonicalJSON(actual.Node)
		if !bytes.Equal(baseNode, otherNode) {
			result.Fields = appendField(result.Fields, "node")
		}
		if !sameInt(base.Start, actual.Start) || !sameInt(base.End, actual.End) {
			result.Fields = appendField(result.Fields, "span")
		}
		if !slices.Equal(base.Errors, actual.Errors) {
			result.Fields = appendField(result.Fields, "errors")
		}
	}
	result.Passed = len(result.Fields) == 0
	return result
}

func appendField(fields []string, field string) []string {
	if !slices.Contains(fields, field) {
		return append(fields, field)
	}
	return fields
}

func sameInt(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func canonicalJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func prettyJSON(data []byte) string {
	if len(data) == 0 {
		return "<absent>"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return string(data)
	}
	return out.String()
}

func validateExpectation(exp Expectation) error {
	if exp.Outcome != "" && exp.Outcome != Accepted && exp.Outcome != Recovered && exp.Outcome != Rejected {
		return fmt.Errorf("unknown outcome %q", exp.Outcome)
	}
	seenClear := make(map[string]bool, len(exp.Clear))
	for _, field := range exp.Clear {
		if field != "node" && field != "start" && field != "end" && field != "errors" {
			return fmt.Errorf("unknown clear field %q", field)
		}
		if seenClear[field] {
			return fmt.Errorf("duplicate clear field %q", field)
		}
		seenClear[field] = true
		conflict := field == "node" && len(exp.Node) > 0 || field == "start" && exp.Start != nil || field == "end" && exp.End != nil || field == "errors" && exp.Errors != nil
		if conflict {
			return fmt.Errorf("clear field %q conflicts with an expectation value", field)
		}
	}
	if len(exp.Node) > 0 {
		if _, err := canonicalJSON(exp.Node); err != nil {
			return fmt.Errorf("node must contain valid JSON: %w", err)
		}
	}
	if exp.Start != nil && *exp.Start < 0 {
		return errors.New("start must be nonnegative")
	}
	if exp.End != nil && *exp.End < 0 {
		return errors.New("end must be nonnegative")
	}
	if exp.Start != nil && exp.End != nil && *exp.End < *exp.Start {
		return errors.New("end must not be before start")
	}
	for i, location := range exp.Errors {
		if location.Pos < 0 {
			return fmt.Errorf("errors[%d].pos must be nonnegative", i)
		}
		if location.Line < 1 {
			return fmt.Errorf("errors[%d].line must be at least 1", i)
		}
		if location.Column < 1 {
			return fmt.Errorf("errors[%d].column must be at least 1", i)
		}
	}
	return nil
}

func rejectNullTopLevel(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"version", "grammar", "start", "unit", "backends", "equivalence", "cases"} {
		if raw, ok := fields[key]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%s must not be null", key)
		}
	}
	return nil
}

func mergeExpectation(base, override Expectation) Expectation {
	for _, field := range override.Clear {
		switch field {
		case "node":
			base.Node = nil
		case "start":
			base.Start = nil
		case "end":
			base.End = nil
		case "errors":
			base.Errors = nil
		}
	}
	if override.Outcome != "" {
		base.Outcome = override.Outcome
	}
	if len(override.Node) > 0 {
		base.Node = override.Node
	}
	if override.Start != nil {
		base.Start = override.Start
	}
	if override.End != nil {
		base.End = override.End
	}
	if override.Errors != nil {
		base.Errors = override.Errors
	}
	return base
}

func normalizeBackends(backends []string) ([]string, error) {
	if len(backends) == 0 {
		return nil, errors.New("fixture: at least one backend is required")
	}
	seenName := make(map[string]bool, len(backends))
	seenRuntime := make(map[string]string, len(backends))
	out := make([]string, 0, len(backends))
	for _, backend := range backends {
		if !validBackend(backend) {
			return nil, fmt.Errorf("fixture: unknown backend %q", backend)
		}
		if seenName[backend] {
			continue
		}
		runtimeBackend := backend
		if runtimeBackend == "default" {
			runtimeBackend = "closure"
		}
		if previous, ok := seenRuntime[runtimeBackend]; ok {
			return nil, fmt.Errorf("fixture: backends %q and %q resolve to the same runtime backend %q", previous, backend, runtimeBackend)
		}
		seenName[backend] = true
		seenRuntime[runtimeBackend] = backend
		out = append(out, backend)
	}
	return out, nil
}

func validBackend(backend string) bool {
	switch backend {
	case "default", "closure", "bytecode", "bytecode-iterative":
		return true
	default:
		return false
	}
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func intPointer(n int) *int { return &n }

// preserveUntargetedNodeJSON keeps the original spelling of node goldens that
// an update did not target. encoding/json otherwise reformats RawMessage values
// when the surrounding suite is indented.
func preserveUntargetedNodeJSON(original, encoded []byte, cases []Case, backend string, backendTargets []bool) []byte {
	originalRoot, err := parseJSONSpan(original)
	if err != nil {
		return encoded
	}
	encodedRoot, err := parseJSONSpan(encoded)
	if err != nil {
		return encoded
	}
	originalCases := originalRoot.field("cases")
	encodedCases := encodedRoot.field("cases")
	if originalCases == nil || encodedCases == nil || len(originalCases.items) != len(encodedCases.items) {
		return encoded
	}
	var edits []jsonEdit
	for i := range cases {
		oldCase, newCase := originalCases.items[i], encodedCases.items[i]
		targetBackend := backendTargets[i]
		oldExpect, newExpect := oldCase.field("expect"), newCase.field("expect")
		if targetBackend && oldExpect != nil && newExpect != nil {
			addUntargetedNodeEdit(original, oldExpect, newExpect, &edits)
		}
		oldBackends, newBackends := oldCase.field("byBackend"), newCase.field("byBackend")
		if oldBackends == nil || newBackends == nil {
			continue
		}
		for _, oldEntry := range oldBackends.fields {
			if targetBackend && oldEntry.key == backend {
				continue
			}
			newEntry := newBackends.field(oldEntry.key)
			if newEntry != nil {
				addUntargetedNodeEdit(original, &oldEntry.value, newEntry, &edits)
			}
		}
	}
	if len(edits) == 0 {
		return encoded
	}
	slices.SortFunc(edits, func(a, b jsonEdit) int { return b.start - a.start })
	out := append([]byte(nil), encoded...)
	for _, e := range edits {
		out = append(append(append([]byte(nil), out[:e.start]...), e.value...), out[e.end:]...)
	}
	return out
}

type jsonEdit struct {
	start, end int
	value      []byte
}

func addUntargetedNodeEdit(original []byte, oldExpectation *jsonSpan, newExpectation *jsonSpan, edits *[]jsonEdit) {
	oldNode, newNode := oldExpectation.field("node"), newExpectation.field("node")
	if oldNode != nil && newNode != nil {
		*edits = append(*edits, jsonEdit{newNode.start, newNode.end, append([]byte(nil), original[oldNode.start:oldNode.end]...)})
	}
}

type jsonSpan struct {
	start, end int
	text       string
	fields     []jsonMember
	items      []jsonSpan
}

type jsonMember struct {
	key   string
	value jsonSpan
}

func (s *jsonSpan) field(name string) *jsonSpan {
	if s == nil {
		return nil
	}
	for i := range s.fields {
		if s.fields[i].key == name {
			return &s.fields[i].value
		}
	}
	return nil
}

func parseJSONSpan(data []byte) (*jsonSpan, error) {
	pos := 0
	span, err := parseJSONValue(data, &pos)
	if err != nil {
		return nil, err
	}
	skipJSONSpace(data, &pos)
	if pos != len(data) {
		return nil, errors.New("extra JSON data")
	}
	return &span, nil
}

func parseJSONValue(data []byte, pos *int) (jsonSpan, error) {
	skipJSONSpace(data, pos)
	if *pos >= len(data) {
		return jsonSpan{}, errors.New("unexpected end of JSON")
	}
	span := jsonSpan{start: *pos}
	switch data[*pos] {
	case '{':
		*pos++
		skipJSONSpace(data, pos)
		for *pos < len(data) && data[*pos] != '}' {
			key, err := parseJSONString(data, pos)
			if err != nil {
				return jsonSpan{}, err
			}
			skipJSONSpace(data, pos)
			if *pos >= len(data) || data[*pos] != ':' {
				return jsonSpan{}, errors.New("expected colon")
			}
			*pos++
			value, err := parseJSONValue(data, pos)
			if err != nil {
				return jsonSpan{}, err
			}
			span.fields = append(span.fields, jsonMember{key: key, value: value})
			skipJSONSpace(data, pos)
			if *pos < len(data) && data[*pos] == ',' {
				*pos++
				skipJSONSpace(data, pos)
				continue
			}
			break
		}
		if *pos >= len(data) || data[*pos] != '}' {
			return jsonSpan{}, errors.New("expected object end")
		}
		*pos++
	case '[':
		*pos++
		skipJSONSpace(data, pos)
		for *pos < len(data) && data[*pos] != ']' {
			item, err := parseJSONValue(data, pos)
			if err != nil {
				return jsonSpan{}, err
			}
			span.items = append(span.items, item)
			skipJSONSpace(data, pos)
			if *pos < len(data) && data[*pos] == ',' {
				*pos++
				skipJSONSpace(data, pos)
				continue
			}
			break
		}
		if *pos >= len(data) || data[*pos] != ']' {
			return jsonSpan{}, errors.New("expected array end")
		}
		*pos++
	case '"':
		text, err := parseJSONString(data, pos)
		if err != nil {
			return jsonSpan{}, err
		}
		span.text = text
	default:
		for *pos < len(data) && !strings.ContainsRune(",]} \t\r\n", rune(data[*pos])) {
			*pos++
		}
	}
	span.end = *pos
	return span, nil
}

func parseJSONString(data []byte, pos *int) (string, error) {
	start := *pos
	if start >= len(data) || data[start] != '"' {
		return "", errors.New("expected JSON string")
	}
	*pos++
	for *pos < len(data) {
		c := data[*pos]
		*pos++
		if c == '\\' {
			if *pos < len(data) {
				*pos++
			}
			continue
		}
		if c == '"' {
			var value string
			if err := json.Unmarshal(data[start:*pos], &value); err != nil {
				return "", err
			}
			return value, nil
		}
	}
	return "", errors.New("unterminated JSON string")
}

func skipJSONSpace(data []byte, pos *int) {
	for *pos < len(data) && strings.ContainsRune(" \t\r\n", rune(data[*pos])) {
		*pos++
	}
}
