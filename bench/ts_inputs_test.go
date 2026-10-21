package bench

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExportTSInputs writes the inputs of the plain-parse TypeScript throughput script
// (ts-parse.mjs) to the directory named by PEGO_TS_INPUT_DIR, one file per workload. It does
// nothing without the variable.
func TestExportTSInputs(t *testing.T) {
	dir := os.Getenv("PEGO_TS_INPUT_DIR")
	if dir == "" {
		t.Skip("PEGO_TS_INPUT_DIR is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"json":  JSONInput(256 << 10),
		"csv":   CSVInput(5000),
		"xml":   XMLInput(256 << 10),
		"arith": ArithInput(20000),
	} {
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
