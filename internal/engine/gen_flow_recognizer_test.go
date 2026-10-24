package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestGeneratedFlowRecognizerParity runs the generated recognizers printed from Flow IR (the
// flowRecognizer switch) against the engine, on the generated-corpus harness, with the
// recognizers of the same corpus printed without the switch as the other side of the comparison.
// The fixtures are generated without work controls, which the switch requires.
func TestGeneratedFlowRecognizerParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	t.Setenv("PEGO_TEST_ORDINARY", "1")
	cases := genCorpus(t)
	changed := 0
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		off, err := generateCode(g, GenOptions{Start: "main", Package: "gen", Recognize: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		on, err := generateCode(g, GenOptions{Start: "main", Package: "gen", Recognize: true, flowRecognizer: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.Equal(on, off) {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("the switch changes no generated recognizer: the parity test exercises nothing")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	t.Run("switch off", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{}) })
	t.Run("switch on", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{flowRecognizer: true}) })
}

// flowRecognizerMutant, if set, changes the code generated with the flowRecognizer switch. The
// negative control sets it to a wrong class test, which the parity test must reject.
var flowRecognizerMutant func([]byte) []byte

// invertClassTests inverts every range test of a character class in the code, as a printer would
// if it negated the test of a range.
func invertClassTests(code []byte) []byte {
	re := regexp.MustCompile(`ch >= (\d+) && ch <= (\d+)`)
	return re.ReplaceAllFunc(code, func(m []byte) []byte {
		sub := re.FindSubmatch(m)
		return []byte(fmt.Sprintf("!(ch >= %s && ch <= %s)", sub[1], sub[2]))
	})
}

// TestGeneratedFlowRecognizerMutantChild runs the switch-on parity test with the wrong class tests
// of invertClassTests. It runs only as a child of TestGeneratedFlowRecognizerNegativeControl.
func TestGeneratedFlowRecognizerMutantChild(t *testing.T) {
	if os.Getenv("PEGO_FLOW_MUTANT") != "1" {
		t.Skip("run by TestGeneratedFlowRecognizerNegativeControl")
	}
	mutated := 0
	flowRecognizerMutant = func(code []byte) []byte {
		mutant := invertClassTests(code)
		if !bytes.Equal(mutant, code) {
			mutated++
		}
		return mutant
	}
	t.Setenv("PEGO_TEST_ORDINARY", "1")
	cases := genCorpus(t)
	testGeneratedParsersCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, GenOptions{flowRecognizer: true})
	if mutated == 0 {
		t.Fatal("the mutant changes no class test")
	}
}

// TestGeneratedFlowRecognizerNegativeControl checks that the parity test rejects a recognizer with a
// wrong class test: the mutant child must fail.
func TestGeneratedFlowRecognizerNegativeControl(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestGeneratedFlowRecognizerMutantChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "PEGO_FLOW_MUTANT=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the mutant passed the parity test:\n%s", out)
	}
	if !strings.Contains(string(out), "--- FAIL: TestGeneratedFlowRecognizerMutantChild") {
		t.Fatalf("the mutant child did not fail its test:\n%s", out)
	}
	// The rejection must come from the recognition comparison, not from a failure to build.
	if !strings.Contains(string(out), "recognize: ") {
		t.Fatalf("the mutant child failed without a recognition mismatch:\n%s", out)
	}
}
