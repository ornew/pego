package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/fixture"
)

func TestTestCommandTextAndBackendSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grammar.zp"), []byte("@start\nrule main = \"a\""), 0o644); err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(dir, "suite.json")
	if err := os.WriteFile(suite, []byte(`{"version":1,"grammar":"grammar.zp","cases":[{"name":"yes","input":"a","expect":{"outcome":"accepted"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := testCmd([]string{"-f", suite, "-backend", "closure,bytecode", "-equivalence"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PASS yes [closure]", "PASS yes [bytecode]", "PASS yes [backend equivalence: closure, bytecode]", "all fixtures passed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
}

func TestTestCommandDispatchRunsSuite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grammar.zp"), []byte("@start\nrule main = \"a\""), 0o644); err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(dir, "suite.json")
	if err := os.WriteFile(suite, []byte(`{"version":1,"grammar":"grammar.zp","cases":[{"name":"dispatch","input":"a","expect":{"outcome":"accepted"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "", "test", "-f", suite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "PASS dispatch [default]") || !strings.Contains(out, "all fixtures passed") {
		t.Fatalf("dispatched test command output = %q", out)
	}
}

func TestTestCommandUpdateRequiresBackendAndPreservesOutcome(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grammar.zp"), []byte("@start\nrule main = \"a\""), 0o644); err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(dir, "suite.json")
	const body = `{"version":1,"grammar":"grammar.zp","cases":[{"name":"yes","input":"a","expect":{"outcome":"accepted"}}]}`
	if err := os.WriteFile(suite, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := testCmd([]string{"-f", suite, "-update"}, &out); err == nil || !strings.Contains(err.Error(), "explicit -backend") {
		t.Fatalf("got %v", err)
	}
	if err := testCmd([]string{"-f", suite, "-update", "-backend", "closure"}, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(suite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `"input": "a"`) || !strings.Contains(string(updated), `"outcome": "accepted"`) {
		t.Fatalf("update changed fixture input or outcome: %s", updated)
	}
}

func TestWriteFixtureTextShowsRuntimeError(t *testing.T) {
	var out bytes.Buffer
	report := fixture.Report{Cases: []fixture.CaseResult{{
		Name: "broken", Backend: "closure", Actual: fixture.Actual{Error: "budget limit exceeded"},
		Mismatch: []fixture.Mismatch{{Field: "runtimeError", Expected: "<none>", Actual: "<present>"}},
	}}}
	if err := writeFixtureText(&out, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "runtime error: budget limit exceeded") {
		t.Fatalf("text report omitted runtime diagnostic: %s", out.String())
	}
}
