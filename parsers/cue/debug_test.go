package cue

import (
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestWriteMutant writes one mutant of the corpus, to compare the parser with the reference implementation
// by hand (go run ./internal/refgen -probe file):
//
//	CUE_MUTANT='parser_test.go#in17' CUE_MUTANT_N=0 CUE_OUT=/tmp/m.cue go test -run TestWriteMutant -v
//
// CUE_MUTANT_PER and CUE_MUTANT_SEED (default 8 and 0) are those of the results file.
func TestWriteMutant(t *testing.T) {
	name, out := os.Getenv("CUE_MUTANT"), os.Getenv("CUE_OUT")
	if name == "" || out == "" {
		t.Skip("set CUE_MUTANT, CUE_MUTANT_N and CUE_OUT to write a mutant")
	}
	var n int
	fmt.Sscan(os.Getenv("CUE_MUTANT_N"), &n)
	per, seed := 8, uint64(0)
	fmt.Sscan(os.Getenv("CUE_MUTANT_PER"), &per)
	fmt.Sscan(os.Getenv("CUE_MUTANT_SEED"), &seed)
	corpus := loadCorpus(t)
	sort.Slice(corpus, func(i, j int) bool { return corpus[i].name < corpus[j].name })
	for i, c := range corpus {
		if c.name == name {
			in := mutate([]byte(c.src), seed+uint64(i*per+n))
			if err := os.WriteFile(out, in, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := ParseAST(string(in), WithUnit(Bytes))
			t.Logf("%s mutant %d: %v", name, n, err)
			return
		}
	}
	t.Fatalf("no source %s", name)
}

// TestDumpFile prints the canonical tree of a file, as the reference results show it:
//
//	CUE_DUMP=/tmp/m.cue go test -run TestDumpFile -v
func TestDumpFile(t *testing.T) {
	name := os.Getenv("CUE_DUMP")
	if name == "" {
		t.Skip("set CUE_DUMP to the file to dump")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFile(string(data), Bytes)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Print("DUMP:", dump(f, Comments(string(data), Bytes)))
}

// TestWriteGenerated writes one generated input (see TestGenerated):
//
//	CUE_GEN=strings CUE_GEN_N=40 CUE_GEN_SEED=0 CUE_OUT=/tmp/g.cue go test -run TestWriteGenerated
func TestWriteGenerated(t *testing.T) {
	fam, out := os.Getenv("CUE_GEN"), os.Getenv("CUE_OUT")
	if fam == "" || out == "" {
		t.Skip("set CUE_GEN, CUE_GEN_N, CUE_GEN_SEED and CUE_OUT to write an input")
	}
	var n int
	var seed uint64
	fmt.Sscan(os.Getenv("CUE_GEN_N"), &n)
	fmt.Sscan(os.Getenv("CUE_GEN_SEED"), &seed)
	if err := os.WriteFile(out, []byte(generate(fam, seed+uint64(n))), 0o644); err != nil {
		t.Fatal(err)
	}
}
