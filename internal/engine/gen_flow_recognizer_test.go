package engine

import (
	"bytes"
	"path/filepath"
	"runtime"
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
