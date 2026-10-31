package cue

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/internal/corpora/corporatest"
	"github.com/ornew/zeroparse/parsers/cue/internal/upstream"
)

// A refResult is the result of the reference implementation (cuelang.org/go/cue/parser) for one source.
type refResult struct {
	name   string
	src    string
	result string // "error: ..." or the canonical tree
}

// corpusSize is the number of sources of the corpus.
const corpusSize = 4061

// loadCorpus collects the CUE of the cue repository (the fetched corpus "cue") and joins it by name with the results of
// the reference implementation in testdata/corpus.refs.gz (see the README for how they were made). The test is
// skipped if the repository is not fetched.
func loadCorpus(t testing.TB) []refResult {
	t.Helper()
	corpus, err := readCorpus(corporatest.Dir(t, "cue"))
	if err != nil {
		t.Fatal(err)
	}
	return corpus
}

var (
	corpusOnce sync.Once
	corpusData []refResult
	corpusErr  error
)

// readCorpus reads the sources below dir and their results. A result without a source, a source without a result, a
// source whose hash differs from the recorded one, and a count that is not corpusSize are errors.
func readCorpus(dir string) ([]refResult, error) {
	corpusOnce.Do(func() { corpusData, corpusErr = readCorpusFiles(dir) })
	return slices.Clone(corpusData), corpusErr
}

func readCorpusFiles(dir string) ([]refResult, error) {
	srcs, err := upstream.Collect(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open("testdata/corpus.refs.gz")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(gz)
	sc.Buffer(nil, 1<<26)
	refs := map[string][2]string{} // name -> hash, result
	for sc.Scan() {
		fields := strings.SplitN(sc.Text(), "\t", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("corpus.refs.gz: bad line %.60q", sc.Text())
		}
		result, err := strconv.Unquote(fields[2])
		if err != nil {
			return nil, fmt.Errorf("corpus.refs.gz: %s: %v", fields[0], err)
		}
		if _, dup := refs[fields[0]]; dup {
			return nil, fmt.Errorf("corpus.refs.gz: %s twice", fields[0])
		}
		refs[fields[0]] = [2]string{fields[1], result}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := make([]refResult, 0, len(srcs))
	for _, s := range srcs {
		ref, ok := refs[s.Name]
		if !ok {
			return nil, fmt.Errorf("%s: no result in corpus.refs.gz", s.Name)
		}
		if got := upstream.Hash(s.Data); got != ref[0] {
			return nil, fmt.Errorf("%s: the fetched source has hash %s, the results are of %s", s.Name, got, ref[0])
		}
		delete(refs, s.Name)
		out = append(out, refResult{name: s.Name, src: string(s.Data), result: ref[1]})
	}
	if len(refs) > 0 {
		return nil, fmt.Errorf("%d results in corpus.refs.gz have no source (for example %s)", len(refs), slices.Sorted(maps.Keys(refs))[0])
	}
	if len(out) != corpusSize {
		return nil, fmt.Errorf("the corpus has %d sources, want %d", len(out), corpusSize)
	}
	return out, nil
}

// firstLine returns the tree of a result without its comments, and whether it is a tree.
func treeOf(result string) (string, bool) {
	if strings.HasPrefix(result, "error: ") {
		return "", false
	}
	return result, true
}

// TestCorpus compares the parser with the reference implementation on the CUE of the cue repository: the
// .cue files and the CUE inside the .txtar archives, the inputs of the tests of the reference parser, and the
// examples of the specification. Both must accept the same sources, and for an accepted one the tree and the
// comments must be the same.
func TestCorpus(t *testing.T) {
	corpus := loadCorpus(t)
	var accepted, rejected, checkOnly, treeDiff, accDiff int
	var reports, treeReports []string
	for _, c := range corpus {
		ref, refOK := treeOf(c.result)
		f, err := ParseFile(c.src, Bytes)
		if (err == nil) != refOK {
			accDiff++
			if len(reports) < 40 {
				reports = append(reports, fmt.Sprintf("%s: parser: %v, reference: %.80s", c.name, err, strings.ReplaceAll(c.result, "\n", " ")))
			}
			continue
		}
		if err != nil {
			rejected++
			if _, aerr := ParseAST(c.src, WithUnit(Bytes)); aerr == nil {
				checkOnly++ // the syntax is valid; Check rejects it
			}
			continue
		}
		accepted++
		got := dump(f, Comments(c.src, Bytes))
		if strings.HasSuffix(ref, "(comments ?)\n") {
			// The reference does not know the comments of this source: compare the trees.
			got, _, _ = strings.Cut(got, "\n")
			ref, _, _ = strings.Cut(ref, "\n")
		}
		if got != ref {
			treeDiff++
			if len(treeReports) < 20 {
				treeReports = append(treeReports, fmt.Sprintf("%s: trees differ\n%s", c.name, firstDiff(got, ref)))
			}
		}
	}
	t.Logf("%d sources: %d accepted, %d rejected (%d of them by Check, which ParseAST does not do), %d differ in acceptance, %d in the tree",
		len(corpus), accepted, rejected, checkOnly, accDiff, treeDiff)
	for _, r := range append(reports, treeReports...) {
		t.Error(r)
	}
}

// firstDiff shows where two canonical trees differ.
func firstDiff(got, want string) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	lo := max(0, i-150)
	return fmt.Sprintf(" got  ...%.300s\n want ...%.300s", got[lo:], want[lo:])
}

// mutantCount is the number of mutants in testdata/mutants.txt.gz.
const mutantCount = 32488

// TestMutants compares the parser with the reference implementation on mutated inputs: every source of
// the corpus with eight random edits (mutate_test.go). testdata/mutants.txt.gz records, for each mutant, the
// hash of the input, whether the reference accepted it, and a hash of its tree.
func TestMutants(t *testing.T) {
	corpus := loadCorpus(t)
	sort.Slice(corpus, func(i, j int) bool { return corpus[i].name < corpus[j].name })
	index := map[string]int{}
	for i, c := range corpus {
		index[c.name] = i
	}
	// The recorded results, or those of another run of refgen (-mutants), named by CUE_MUTANTS.
	name := "testdata/mutants.txt.gz"
	if n := os.Getenv("CUE_MUTANTS"); n != "" {
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
	sc.Buffer(nil, 1<<20)
	if !sc.Scan() {
		t.Fatal("empty mutants file")
	}
	var per int
	var seed uint64
	if _, err := fmt.Sscanf(sc.Text(), "# per=%d seed=%d", &per, &seed); err != nil {
		t.Fatalf("bad header %q", sc.Text())
	}
	var total, accDiff, treeDiff int
	var reports []string
	for sc.Scan() {
		fs := strings.Split(sc.Text(), "\t")
		if len(fs) < 4 {
			t.Fatalf("bad line %q", sc.Text())
		}
		i, ok := index[fs[0]]
		if !ok {
			t.Fatalf("unknown source %q", fs[0])
		}
		var m int
		fmt.Sscan(fs[1], &m)
		in := mutate([]byte(corpus[i].src), seed+uint64(i*per+m))
		h := fnv.New64a()
		h.Write(in)
		if fmt.Sprintf("%016x", h.Sum64()) != fs[2] {
			t.Fatalf("%s mutant %d: the mutation differs from the one of refgen (keep mutate_test.go and internal/refgen/mutate.go in sync)", fs[0], m)
		}
		total++
		refOK := fs[3] == "1"
		ast, err := ParseFile(string(in), Bytes)
		if (err == nil) != refOK {
			accDiff++
			if len(reports) < reportLimit {
				reports = append(reports, fmt.Sprintf("%s mutant %d: parser: %v, reference accepted: %v\n%s", fs[0], m, err, refOK, trunc(string(in), 400)))
			}
			continue
		}
		if err != nil {
			continue
		}
		tree, _, _ := strings.Cut(dump(ast, nil), "\n")
		sum := sha256.Sum256([]byte(tree))
		if fmt.Sprintf("%x", sum[:8]) != fs[4] {
			treeDiff++
			if len(reports) < reportLimit {
				reports = append(reports, fmt.Sprintf("%s mutant %d: trees differ\n%s", fs[0], m, trunc(string(in), 400)))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CUE_MUTANTS") == "" && total != mutantCount {
		t.Errorf("testdata/mutants.txt.gz holds %d mutants, want %d", total, mutantCount)
	}
	t.Logf("%d mutants: %d differ in acceptance, %d in the tree", total, accDiff, treeDiff)
	for _, r := range reports {
		t.Error(r)
	}
}

// reportLimit is the number of differences that the tests print (CUE_REPORTS=all prints them all).
var reportLimit = func() int {
	if os.Getenv("CUE_REPORTS") == "all" {
		return 1 << 30
	}
	return 40
}()

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
