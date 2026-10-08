package python_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"github.com/ornew/pego/parsers/python"
)

// The vendored reference data, generated with CPython 3.14.0 by internal/refgen/generate.sh: the
// snippets of code in the strings and doctests of CPython's Lib/test, and some of its test files
// that exercise the grammar. For each, whether ast.parse accepts it, and the short SHA-256 of the
// dump of its tree (ast.dump) without and with the positions. These tests need Go alone.

type reference struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Hash   string `json:"hash"`
	PHash  string `json:"phash"`
	Source string `json:"source"`
}

func readReferences(t *testing.T, name string) []reference {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var refs []reference
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		var r reference
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return refs
}

// checkReferences compares the parser with the references: the same inputs accepted, with the same
// ast.dump and the same positions, and ParseAST and Recognize in agreement.
func checkReferences(t *testing.T, refs []reference) {
	var same, rejected, fail int
	report := func(format string, args ...any) {
		if fail++; fail <= 40 {
			t.Errorf(format, args...)
		}
	}
	for _, r := range refs {
		name := r.Path
		if name == "" {
			name = r.Source
			if len(name) > 80 {
				name = name[:80] + "..."
			}
		}
		m, err := python.ParseModule(r.Source)
		_, aerr := python.ParseAST(r.Source)
		if rerr := python.Recognize(r.Source); (rerr == nil) != (aerr == nil) {
			report("%q: ParseAST: %v, Recognize: %v", name, aerr, rerr)
		}
		switch {
		case r.OK && err != nil:
			report("rejected %q: %v", name, firstLine(err))
		case !r.OK && err == nil:
			report("accepted %q", name)
		case !r.OK:
			rejected++
		case sha(python.Dump(m))[:16] != r.Hash:
			report("different ast.dump for %q", name)
		case sha(python.DumpWithPositions(m, r.Source))[:16] != r.PHash:
			report("different positions for %q", name)
		default:
			same++
		}
	}
	t.Logf("%d inputs: %d accepted by both with the same ast.dump and positions, %d rejected by both, %d differ",
		len(refs), same, rejected, fail)
}

func TestReferenceSnippets(t *testing.T) {
	checkReferences(t, readReferences(t, "testdata/cpython-snippets.jsonl.gz"))
}

func TestReferenceFiles(t *testing.T) {
	checkReferences(t, readReferences(t, "testdata/cpython-files.jsonl.gz"))
}
