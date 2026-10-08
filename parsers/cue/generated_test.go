package cue

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"
)

// TestGenerated compares the parser with the reference implementation on generated inputs (gen_test.go): field
// values that are string literals of random forms, number-like tokens, and random sequences of tokens, which
// test the lexical rules and the insertion of commas at the borders of what the corpus holds.
// testdata/generated.txt.gz records, for each input, the hash of the input, whether the reference accepted it,
// and a hash of its tree.
func TestGenerated(t *testing.T) {
	name := "testdata/generated.txt.gz"
	if n := os.Getenv("CUE_GENERATED"); n != "" {
		name = n
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(gz)
	if !sc.Scan() {
		t.Fatal("empty generated file")
	}
	var count int
	var seed uint64
	if _, err := fmt.Sscanf(sc.Text(), "# count=%d seed=%d", &count, &seed); err != nil {
		t.Fatalf("bad header %q", sc.Text())
	}
	var total, accepted, accDiff, treeDiff int
	var reports []string
	for sc.Scan() {
		fs := strings.Split(sc.Text(), "\t")
		if len(fs) < 4 {
			t.Fatalf("bad line %q", sc.Text())
		}
		var i int
		fmt.Sscan(fs[1], &i)
		in := generate(fs[0], seed+uint64(i))
		h := fnv.New64a()
		h.Write([]byte(in))
		if fmt.Sprintf("%016x", h.Sum64()) != fs[2] {
			t.Fatalf("%s %d: the input differs from the one of refgen (keep gen_test.go and internal/refgen/gen.go in sync)", fs[0], i)
		}
		total++
		refOK := fs[3] == "1"
		ast, err := ParseFile(in, Bytes)
		if (err == nil) != refOK {
			accDiff++
			if len(reports) < reportLimit {
				reports = append(reports, fmt.Sprintf("%s %d: parser: %v, reference accepted: %v\n%s", fs[0], i, err, refOK, trunc(in, 300)))
			}
			continue
		}
		if err != nil {
			continue
		}
		accepted++
		tree, _, _ := strings.Cut(dump(ast, nil), "\n")
		sum := sha256.Sum256([]byte(tree))
		if fmt.Sprintf("%x", sum[:8]) != fs[4] {
			treeDiff++
			if len(reports) < reportLimit {
				reports = append(reports, fmt.Sprintf("%s %d: trees differ\n%s", fs[0], i, trunc(in, 300)))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d generated inputs: %d accepted by the reference, %d differ in acceptance, %d in the tree", total, accepted, accDiff, treeDiff)
	for _, r := range reports {
		t.Error(r)
	}
}
