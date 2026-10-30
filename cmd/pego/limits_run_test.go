//go:build !pego_budget_controls_reference

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/editfuzz"
)

const limitsGrammar = "@start\nrule main = [a-z]* $$"

// wantLimit checks that err is the limit error of kind with the given message in the output of
// the command.
func wantLimit(t *testing.T, what string, err error, kind pego.LimitKind, message string) {
	t.Helper()
	var le *pego.LimitError
	if !errors.As(err, &le) || le.Kind != kind {
		t.Fatalf("%s: error %v, want a limit error of kind %d", what, err, kind)
	}
	if !strings.Contains(err.Error(), message) {
		t.Errorf("%s: error %q does not contain %q", what, err, message)
	}
	var c exitCoder
	if errors.As(err, &c) {
		t.Errorf("%s: the error selects exit status %d, want the default 1", what, c.ExitCode())
	}
}

// parse, trace, profile and explain apply -budget and -max-input to their parse: a small budget
// interrupts it, a short -max-input rejects the input, and unlimited and zero run it.
func TestDebugAndParseCommandsApplyLimits(t *testing.T) {
	g := writeFile(t, "limits.pego", limitsGrammar)
	const input = "abcdefghijklmnopqrstuvwxyz"
	for _, cmd := range []string{"parse", "trace", "profile", "explain"} {
		t.Run(cmd, func(t *testing.T) {
			args := func(extra ...string) []string {
				return append([]string{cmd, "-g", g, "-i", input}, extra...)
			}
			for _, ok := range [][]string{nil, {"-budget", "0"}, {"-budget", "unlimited"}, {"-max-input", "unlimited"}, {"-max-input", "26"}, {"-budget", "1000", "-max-input", "100"}} {
				if _, err := runCLI(t, "", args(ok...)...); err != nil {
					t.Errorf("%v: %v", ok, err)
				}
			}
			if cmd == "profile" {
				// profile reports the result of the parse in its output, whatever it is, and succeeds.
				out, err := runCLI(t, "", args("-budget", "5")...)
				if err != nil || !strings.Contains(out, "result: parse budget limit exceeded: limit 5") {
					t.Errorf("-budget 5: error %v\n%s", err, out)
				}
				out, err = runCLI(t, input, cmd, "-g", g, "-max-input", "3")
				if err != nil || !strings.Contains(out, "result: parse input limit exceeded: limit 3, length 26") {
					t.Errorf("-max-input 3: error %v\n%s", err, out)
				}
				return
			}
			_, err := runCLI(t, "", args("-budget", "5")...)
			wantLimit(t, "-budget 5", err, pego.LimitBudget, "parse budget limit exceeded: limit 5")
			_, err = runCLI(t, "", args("-max-input", "25")...)
			wantLimit(t, "-max-input 25", err, pego.LimitInput, "parse input limit exceeded: limit 25, length 26")
			// The input comes from standard input without -i, and the limit applies to it too.
			_, err = runCLI(t, input, cmd, "-g", g, "-max-input", "3")
			wantLimit(t, "stdin", err, pego.LimitInput, "limit 3, length 26")
		})
	}
	// -unit bytes counts bytes.
	_, err := runCLI(t, "", "parse", "-g", g, "-i", "abc", "-unit", "bytes", "-max-input", "2")
	wantLimit(t, "bytes", err, pego.LimitInput, "limit 2, length 3")
	// -check and -stream apply the limits as well.
	_, err = runCLI(t, "", "parse", "-g", g, "-i", input, "-check", "-max-input", "3")
	wantLimit(t, "-check", err, pego.LimitInput, "length 26")
	st := writeFile(t, "stream.pego", "@start\nrule main = line* #stream $$\nrule line = [a-z]+ \"\\n\"")
	_, err = runCLI(t, "", "parse", "-g", st, "-i", "ab\ncd\nef\n", "-stream", "-max-input", "4")
	wantLimit(t, "-stream", err, pego.LimitInput, "parse input limit exceeded: limit 4")
	if _, err = runCLI(t, "", "parse", "-g", st, "-i", "ab\ncd\n", "-stream", "-max-input", "6"); err != nil {
		t.Errorf("a stream at the limit: %v", err)
	}
}

// A malformed value is a usage error that names the flag.
func TestCommandsRejectMalformedLimits(t *testing.T) {
	g := writeFile(t, "limits.pego", limitsGrammar)
	for _, cmd := range []string{"parse", "trace", "profile", "explain"} {
		_, err := runCLI(t, "", cmd, "-g", g, "-i", "a", "-budget", "lots")
		if err == nil || !strings.Contains(err.Error(), "-budget") {
			t.Errorf("%s: error %v", cmd, err)
		}
	}
}

func TestTestCommandAppliesLimits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grammar.pego"), []byte(limitsGrammar), 0o644); err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(dir, "suite.json")
	if err := os.WriteFile(suite, []byte(`{"version":1,"grammar":"grammar.pego","cases":[{"name":"long","input":"abcdefghijklmnopqrstuvwxyz","expect":{"outcome":"accepted"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		var out strings.Builder
		err := testCmd(append([]string{"-f", suite}, args...), &out)
		return out.String(), err
	}
	if out, err := run("-budget", "unlimited", "-max-input", "26"); err != nil || !strings.Contains(out, "all fixtures passed") {
		t.Fatalf("within the limits: %v\n%s", err, out)
	}
	// A limit error is the runtime error of the case, and the suite fails.
	out, err := run("-max-input", "25")
	if err == nil || !strings.Contains(out, "runtime error: parse input limit exceeded: limit 25, length 26") {
		t.Errorf("-max-input: error %v\n%s", err, out)
	}
	out, err = run("-budget", "5")
	if err == nil || !strings.Contains(out, "runtime error: parse budget limit exceeded: limit 5") {
		t.Errorf("-budget: error %v\n%s", err, out)
	}
}

func TestEditFuzzAppliesLimits(t *testing.T) {
	g := writeFile(t, "limits.pego", limitsGrammar)
	base := []string{"edit-fuzz", "-g", g, "-backend", "closure", "-unit", "codepoints", "-sequences", "2", "-operations", "4"}
	if out, err := runCLI(t, "", append(base, "-budget", "unlimited", "-max-input", "unlimited")...); err != nil || !strings.Contains(out, "ok closure/codepoints") {
		t.Fatalf("unlimited: %v\n%s", err, out)
	}
	// A budget of one cannot parse any initial text of the sequences.
	_, err := runCLI(t, "", append(base, "-budget", "1")...)
	if err == nil || !strings.Contains(err.Error(), "parse budget limit exceeded") {
		t.Errorf("-budget: error %v", err)
	}
	_, err = runCLI(t, "", append(base, "-max-input", "1")...)
	if err == nil || !strings.Contains(err.Error(), "parse input limit exceeded") {
		t.Errorf("-max-input: error %v", err)
	}
}

// A saved sequence does not record the limits: -replay takes them from its own flags, and fuzzing flags that
// the sequence already records are still refused.
func TestEditFuzzReplayAppliesLimits(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.pego")
	if err := os.WriteFile(g, []byte(limitsGrammar), 0o644); err != nil {
		t.Fatal(err)
	}
	seq := &editfuzz.Sequence{GrammarSHA256: editfuzz.GrammarHash([]byte(limitsGrammar)), Start: "main",
		Backend: pego.Closure, Unit: pego.CodePoints, Initial: []byte("abc"), Operations: []editfuzz.Operation{{Kind: editfuzz.Checkpoint}}}
	path, err := editfuzz.Save(dir, seq)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, "", "edit-fuzz", "-g", g, "-replay", path, "-budget", "unlimited", "-max-input", "3"); err != nil || !strings.Contains(out, "ok "+path) {
		t.Fatalf("at the limit: %v\n%s", err, out)
	}
	_, err = runCLI(t, "", "edit-fuzz", "-g", g, "-replay", path, "-max-input", "2")
	if err == nil || !strings.Contains(err.Error(), "parse input limit exceeded: limit 2, length 3") {
		t.Errorf("over the limit: %v", err)
	}
	_, err = runCLI(t, "", "edit-fuzz", "-g", g, "-replay", path, "-seed", "1")
	if err == nil || !strings.Contains(err.Error(), "-replay does not take -seed") {
		t.Errorf("-seed: %v", err)
	}
}
