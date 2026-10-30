package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func writeFixtureFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunOutcomesLocationsAndEquivalence(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suitePath := writeFixtureFile(t, dir, "suite.json", `{
  "version": 1,
  "grammar": "grammar.pego",
  "backends": ["closure", "bytecode", "bytecode-iterative"],
  "equivalence": true,
  "cases": [
    {"name":"accepted", "input":"a", "expect":{"outcome":"accepted", "start":0, "end":1}},
    {"name":"rejected", "input":"b", "expect":{"outcome":"rejected", "errors":[{"pos":0,"line":1,"column":1}]}}
  ]
}`)
	suite, err := Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(suite, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 6 || len(report.Equivalence) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunClassifiesNilNodeRecoveryAndRejection(t *testing.T) {
	cases := []struct {
		name, grammar, input string
		want                 Outcome
	}{
		{"nil node success", "@start\nrule main _ = \"a\" => nil", "a", Accepted},
		{"recovered completion", "@start\nrule main = \"a\" #recover(skip=\"?\") $$", "?", Recovered},
		{"syntax rejection", "@start\nrule main = \"a\" $$", "b", Rejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixtureFile(t, dir, "grammar.pego", tc.grammar)
			suitePath := writeFixtureFile(t, dir, "suite.json", fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":%q,"expect":{"outcome":%q}}]}`, tc.input, tc.want))
			suite, err := Load(suitePath)
			if err != nil {
				t.Fatal(err)
			}
			report, err := Run(suite, RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !report.Passed || len(report.Cases) != 1 || report.Cases[0].Actual.Outcome != tc.want {
				t.Fatalf("outcome report = %+v, want %q", report, tc.want)
			}
			if tc.name == "nil node success" && string(report.Cases[0].Actual.Node) != "null" {
				t.Fatalf("nil-node success JSON = %s, want null", report.Cases[0].Actual.Node)
			}
		})
	}
}

func TestRunRecordsRuntimeErrorAndContinues(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = [a-z]+ #atomic $$")
	long := strings.Repeat("a", 10000)
	suiteJSON := fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"before","input":"a","expect":{"outcome":"accepted"}},{"name":"limit","input":%q,"expect":{"outcome":"accepted"}},{"name":"after","input":"a","expect":{"outcome":"accepted"}}]}`, long)
	suite, err := Load(writeFixtureFile(t, dir, "suite.json", suiteJSON))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(suite, RunOptions{Limits: pego.Limits{MaxWork: 256}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 3 || !report.Cases[0].Passed || report.Cases[0].Actual.Error != "" || report.Cases[1].Actual.Error == "" || !report.Cases[2].Passed {
		t.Fatalf("runtime error did not remain isolated to its case: %+v", report)
	}
	if report.Passed {
		t.Fatalf("runtime error should fail the report: %+v", report)
	}
	before, err := os.ReadFile(suite.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateGoldens(suite, report, "default"); err == nil || !strings.Contains(err.Error(), "runtime error") {
		t.Fatalf("UpdateGoldens error = %v, want runtime-error refusal", err)
	}
	after, err := os.ReadFile(suite.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("runtime-error update changed the suite")
	}
}

func TestRunCancellationStopsInvocation(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suite, err := Load(writeFixtureFile(t, dir, "suite.json", `{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := Run(suite, RunOptions{Context: ctx})
	if !errors.Is(err, context.Canceled) || len(report.Cases) != 0 {
		t.Fatalf("canceled invocation report=%+v err=%v", report, err)
	}
}

func TestLoadRequiresInputButAllowsEmptyString(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"\"")
	t.Run("missing", func(t *testing.T) {
		path := writeFixtureFile(t, dir, "missing.json", `{"version":1,"grammar":"grammar.pego","cases":[{"name":"empty","expect":{"outcome":"accepted"}}]}`)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "input is required") {
			t.Fatalf("Load error = %v, want required-input error", err)
		}
	})
	t.Run("explicit empty", func(t *testing.T) {
		path := writeFixtureFile(t, dir, "empty.json", `{"version":1,"grammar":"grammar.pego","cases":[{"name":"empty","input":"","expect":{"outcome":"accepted"}}]}`)
		suite, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if suite.Cases[0].Input != "" {
			t.Fatalf("input = %q, want explicit empty input", suite.Cases[0].Input)
		}
		report, err := Run(suite, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !report.Passed {
			t.Fatalf("empty-input fixture failed: %+v", report)
		}
	})
}

func TestLoadRequiresExpectObjectEvenWithBackendOutcome(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	for _, tc := range []struct {
		name, expectField string
	}{
		{"missing", ""},
		{"null", `,"expect":null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a"%s,"byBackend":{"closure":{"outcome":"accepted"}}}]}`, tc.expectField)
			path := writeFixtureFile(t, dir, tc.name+".json", body)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "expect is required") {
				t.Fatalf("Load error = %v, want missing-expect-object error", err)
			}
		})
	}
}

func TestLoadRejectsNullTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	for _, field := range []string{"unit", "start", "backends", "equivalence"} {
		t.Run(field, func(t *testing.T) {
			body := fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","%s":null,"cases":[{"name":"case","input":"a","expect":{"outcome":"accepted"}}]}`, field)
			path := writeFixtureFile(t, dir, field+"-null.json", body)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), field+" must not be null") {
				t.Fatalf("Load error = %v, want null-field error", err)
			}
		})
	}
}

func TestLoadRejectsInvalidErrorLocationsAndSpans(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	tests := []struct {
		name, expectation, wantError string
	}{
		{"missing pos", `{"outcome":"rejected","errors":[{"line":1,"column":1}]}`, "pos is required"},
		{"missing line", `{"outcome":"rejected","errors":[{"pos":0,"column":1}]}`, "line is required"},
		{"missing column", `{"outcome":"rejected","errors":[{"pos":0,"line":1}]}`, "column is required"},
		{"null pos", `{"outcome":"rejected","errors":[{"pos":null,"line":1,"column":1}]}`, "pos is required"},
		{"null line", `{"outcome":"rejected","errors":[{"pos":0,"line":null,"column":1}]}`, "line is required"},
		{"null column", `{"outcome":"rejected","errors":[{"pos":0,"line":1,"column":null}]}`, "column is required"},
		{"negative pos", `{"outcome":"rejected","errors":[{"pos":-1,"line":1,"column":1}]}`, "pos must be nonnegative"},
		{"zero line", `{"outcome":"rejected","errors":[{"pos":0,"line":0,"column":1}]}`, "line must be at least 1"},
		{"zero column", `{"outcome":"rejected","errors":[{"pos":0,"line":1,"column":0}]}`, "column must be at least 1"},
		{"negative start", `{"outcome":"accepted","start":-1}`, "start must be nonnegative"},
		{"negative end", `{"outcome":"accepted","end":-1}`, "end must be nonnegative"},
		{"end before start", `{"outcome":"accepted","start":2,"end":1}`, "end must not be before start"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":%s}]}`, tc.expectation)
			path := writeFixtureFile(t, dir, strings.ReplaceAll(tc.name, " ", "-")+".json", body)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Load error = %v, want %q", err, tc.wantError)
			}
		})
	}
	t.Run("empty clear shared", func(t *testing.T) {
		body := `{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted","clear":[]}}]}`
		path := writeFixtureFile(t, dir, "clear-shared-empty.json", body)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "clear is only valid in byBackend expectations") {
			t.Fatalf("Load error = %v, want shared-clear rejection", err)
		}
	})
}

func TestLoadValidatesEffectiveBackendExpectations(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	path := writeFixtureFile(t, dir, "effective.json", `{"version":1,"grammar":"grammar.pego","backends":["closure"],"cases":[{"name":"case","input":"a","expect":{"outcome":"accepted","start":5,"end":10},"byBackend":{"closure":{"end":3}}}]}`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "end must not be before start") {
		t.Fatalf("Load error = %v, want merged-span validation error", err)
	}
}

func TestLoadValidatesUnselectedBackendOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	path := writeFixtureFile(t, dir, "effective-unselected.json", `{"version":1,"grammar":"grammar.pego","backends":["closure"],"cases":[{"name":"case","input":"a","expect":{"outcome":"accepted","start":5,"end":10},"byBackend":{"bytecode":{"end":3}}}]}`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "backend bytecode: end must not be before start") {
		t.Fatalf("Load error = %v, want merged bytecode-span validation error", err)
	}
}

func TestLoadValidatesBackendClearFields(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	tests := []struct {
		name, override, want string
	}{
		{"unknown", `"clear":["outcome"]`, `unknown clear field "outcome"`},
		{"duplicate", `"clear":["node","node"]`, `duplicate clear field "node"`},
		{"conflicting node", `"node":null,"clear":["node"]`, `clear field "node" conflicts`},
		{"conflicting span", `"start":1,"clear":["start"]`, `clear field "start" conflicts`},
		{"conflicting errors", `"errors":[],"clear":["errors"]`, `clear field "errors" conflicts`},
		{"clear shared", ``, `clear is only valid in byBackend expectations`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body string
			if tc.name == "clear shared" {
				body = `{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted","clear":["node"]}}]}`
			} else {
				body = fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted"},"byBackend":{"closure":{%s}}}]}`, tc.override)
			}
			path := writeFixtureFile(t, dir, "clear-"+strings.ReplaceAll(tc.name, " ", "-")+".json", body)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsNullTypedExpectationsAndAllowsNullNode(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	tests := []struct {
		name, caseFields, wantError string
	}{
		{"null byBackend map", `,"byBackend":null`, "byBackend must not be null"},
		{"null byBackend value", `,"byBackend":{"closure":null}`, `byBackend "closure" must be an expectation object`},
		{"null outcome", ``, "expectation outcome must not be null"},
		{"null start", ``, "expectation start must not be null"},
		{"null end", ``, "expectation end must not be null"},
		{"null errors", ``, "expectation errors must not be null"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expect := `{"outcome":"accepted"}`
			switch tc.name {
			case "null outcome":
				expect = `{"outcome":null}`
			case "null start":
				expect = `{"outcome":"accepted","start":null}`
			case "null end":
				expect = `{"outcome":"accepted","end":null}`
			case "null errors":
				expect = `{"outcome":"accepted","errors":null}`
			}
			body := fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":%s%s}]}`, expect, tc.caseFields)
			path := writeFixtureFile(t, dir, strings.ReplaceAll(tc.name, " ", "-")+".json", body)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Load error = %v, want %q", err, tc.wantError)
			}
		})
	}
	t.Run("node null", func(t *testing.T) {
		body := `{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted","node":null}}]}`
		path := writeFixtureFile(t, dir, "node-null.json", body)
		if _, err := Load(path); err != nil {
			t.Fatalf("Load rejected a valid null node golden: %v", err)
		}
	})
	for _, field := range []string{"outcome", "start", "end", "errors"} {
		t.Run("backend null "+field, func(t *testing.T) {
			body := fmt.Sprintf(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted"},"byBackend":{"closure":{"%s":null}}}]}`, field)
			path := writeFixtureFile(t, dir, "backend-null-"+field+".json", body)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "expectation "+field+" must not be null") {
				t.Fatalf("Load error = %v, want null-field rejection", err)
			}
		})
	}
	t.Run("backend node null", func(t *testing.T) {
		body := `{"version":1,"grammar":"grammar.pego","cases":[{"name":"case","input":"a","expect":{"outcome":"accepted"},"byBackend":{"closure":{"node":null}}}]}`
		path := writeFixtureFile(t, dir, "backend-node-null.json", body)
		if _, err := Load(path); err != nil {
			t.Fatalf("Load rejected a valid backend null node golden: %v", err)
		}
	})
}

func TestBackendsRejectAliasesOfSameRuntimeBackend(t *testing.T) {
	for _, backends := range [][]string{
		{"default", "closure"},
	} {
		t.Run(strings.Join(backends, "+"), func(t *testing.T) {
			if _, err := normalizeBackends(backends); err == nil || !strings.Contains(err.Error(), "same runtime backend") {
				t.Fatalf("normalizeBackends(%v) error = %v, want duplicate-runtime-backend error", backends, err)
			}
		})
	}
	got, err := normalizeBackends([]string{"bytecode", "closure", "bytecode"})
	if err != nil {
		t.Fatalf("normalizeBackends exact duplicate: %v", err)
	}
	want := []string{"bytecode", "closure"}
	if !slices.Equal(got, want) {
		t.Fatalf("normalized backends = %v, want %v", got, want)
	}
}

func TestUpdateGoldensPreservesInputAndAcceptance(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suitePath := writeFixtureFile(t, dir, "suite.json", `{
  "version": 1,
  "grammar": "grammar.pego",
  "backends": ["closure"],
  "cases": [{"name":"kept", "input":"a", "expect":{"outcome":"accepted","node":{"type" : "old"}}}]
}`)
	suite, err := Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(suite, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateGoldens(suite, report, "closure"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var got Suite
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cases[0].Input != "a" || got.Cases[0].Expect.Outcome != Accepted {
		t.Fatalf("update changed input or acceptance: %+v", got.Cases[0])
	}
	if got.Cases[0].Expect.Start == nil || *got.Cases[0].Expect.Start != 0 || got.Cases[0].Expect.End == nil || *got.Cases[0].Expect.End != 1 {
		t.Fatalf("goldens were not written: %+v", got.Cases[0].Expect)
	}
	updatedNode, err := canonicalJSON(got.Cases[0].Expect.Node)
	if err != nil {
		t.Fatal(err)
	}
	actualNode, err := canonicalJSON(report.Cases[0].Actual.Node)
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedNode) != string(actualNode) {
		t.Fatalf("shared target golden was not updated: got %s, want %s", updatedNode, actualNode)
	}
}

func TestUpdateGoldensRefusesOutcomeMismatchWithoutPartialChanges(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suitePath := writeFixtureFile(t, dir, "suite.json", `{
  "version": 1,
  "grammar": "grammar.pego",
  "backends": ["closure"],
  "cases": [
    {"name":"would update", "input":"a", "expect":{"outcome":"accepted","node":{"type":"old"}}},
    {"name":"mismatch", "input":"b", "expect":{"outcome":"accepted","node":{"type":"also-old"}}}
  ]
}`)
	suite, err := Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(suite, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateGoldens(suite, report, "closure"); err == nil || !strings.Contains(err.Error(), `expected outcome "accepted", got "rejected"`) {
		t.Fatalf("UpdateGoldens error = %v, want outcome mismatch refusal", err)
	}
	after, err := os.ReadFile(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("outcome mismatch update changed the suite file")
	}
	if string(suite.Cases[0].Expect.Node) != `{"type":"old"}` || suite.Cases[0].Expect.Start != nil {
		t.Fatalf("outcome mismatch partially changed in-memory suite: %+v", suite.Cases[0].Expect)
	}
}

func TestCanonicalJSONIgnoresObjectMemberOrder(t *testing.T) {
	a, err := canonicalJSON([]byte(`{"fields":{"x":"a","y":"b"},"end":1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalJSON([]byte(`{"end":1,"fields":{"y":"b","x":"a"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("canonical forms differ: %s != %s", a, b)
	}
}

func TestExpectationFailureDoesNotBecomeAnEquivalencePass(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suitePath := writeFixtureFile(t, dir, "suite.json", `{
  "version": 1,
  "grammar": "grammar.pego",
  "backends": ["closure", "bytecode"],
  "equivalence": true,
  "cases": [{"name":"wrong expectation", "input":"a", "expect":{"outcome":"rejected"}}]
}`)
	suite, err := Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(suite, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Cases) != 2 || report.Cases[0].Passed || report.Cases[1].Passed {
		t.Fatalf("expected both fixtures to fail: %+v", report)
	}
	if len(report.Equivalence) != 1 || !report.Equivalence[0].Passed {
		t.Fatalf("equivalence should be a separate passing comparison: %+v", report.Equivalence)
	}
}

func TestBackendUpdateRecordsClearsAndPreservesEmptyErrors(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suitePath := writeFixtureFile(t, dir, "suite.json", `{
  "version": 1,
  "grammar": "grammar.pego",
  "backends": ["closure"],
  "cases": [
    {"name":"rejected", "input":"b", "expect":{"outcome":"rejected", "node":{"old":true}, "start":0, "end":1}, "byBackend":{}},
    {"name":"accepted", "input":"a", "expect":{"outcome":"accepted", "errors":[{"pos":9,"line":1,"column":10}]}, "byBackend":{}},
    {"name":"empty-errors", "input":"a", "expect":{"outcome":"accepted"}, "byBackend":{"closure":{"errors":[]}}},
    {"name":"empty-map", "input":"a", "expect":{"outcome":"accepted"}, "byBackend":{}}
  ]
}`)
	runAndUpdate := func() {
		t.Helper()
		suite, err := Load(suitePath)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Run(suite, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := UpdateGoldens(suite, report, "closure"); err != nil {
			t.Fatal(err)
		}
	}
	runAndUpdate()
	first, err := os.ReadFile(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var got Suite
	if err := json.Unmarshal(first, &got); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]Case)
	for _, c := range got.Cases {
		byName[c.Name] = c
	}
	for _, field := range []string{"node", "start", "end"} {
		if !slices.Contains(byName["rejected"].ByBackend["closure"].Clear, field) {
			t.Errorf("rejected backend clear = %v, want %q", byName["rejected"].ByBackend["closure"].Clear, field)
		}
	}
	if !slices.Contains(byName["accepted"].ByBackend["closure"].Clear, "errors") {
		t.Errorf("accepted backend clear = %v, want errors", byName["accepted"].ByBackend["closure"].Clear)
	}
	if byName["empty-errors"].ByBackend["closure"].Errors == nil || len(byName["empty-errors"].ByBackend["closure"].Errors) != 0 {
		t.Errorf("explicit errors:[] was not preserved: %+v", byName["empty-errors"].ByBackend["closure"])
	}
	if byName["empty-map"].ByBackend["closure"].Node == nil || byName["empty-map"].Expect.Node != nil {
		t.Errorf("explicit byBackend:{} did not target the selected backend: %+v", byName["empty-map"])
	}
	runAndUpdate()
	second, err := os.ReadFile(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("a second update changed the already updated fixture")
	}
}

func TestBackendSpecificUpdateDoesNotCopySharedAcceptance(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "grammar.pego", "@start\nrule main = \"a\"")
	suitePath := writeFixtureFile(t, dir, "suite.json", `{
  "version": 1,
  "grammar": "grammar.pego",
  "backends": ["closure"],
  "cases": [{"name":"yes", "input":"a", "expect":{"outcome":"accepted","node":{"type" : "shared"}}, "byBackend":{"closure":{"start":0}}}]
}`)
	suite, err := Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(suite, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateGoldens(suite, report, "closure"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var got Suite
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cases[0].Expect.Outcome != Accepted {
		t.Fatalf("shared acceptance changed: %+v", got.Cases[0].Expect)
	}
	if string(got.Cases[0].Expect.Node) != `{"type" : "shared"}` {
		t.Fatalf("backend update changed shared node golden: %s", got.Cases[0].Expect.Node)
	}
	if got.Cases[0].ByBackend["closure"].Outcome != "" {
		t.Fatalf("update created a backend acceptance assertion: %+v", got.Cases[0].ByBackend["closure"])
	}
	updatedBackendNode, err := canonicalJSON(got.Cases[0].ByBackend["closure"].Node)
	if err != nil {
		t.Fatal(err)
	}
	actualNode, err := canonicalJSON(report.Cases[0].Actual.Node)
	if err != nil {
		t.Fatal(err)
	}
	if string(updatedBackendNode) != string(actualNode) {
		t.Fatalf("backend target golden was not updated: got %s, want %s", updatedBackendNode, actualNode)
	}
}
