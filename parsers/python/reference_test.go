package python_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/corpora"
	"github.com/ornew/pego/internal/corpora/corporatest"
	"github.com/ornew/pego/parsers/python"
)

// The reference data, generated with CPython 3.14.0 by internal/refgen/generate.sh: the snippets of code in the
// strings and doctests of CPython's Lib/test, and some of its test files that exercise the grammar. For each, whether
// ast.parse accepts it, and the short SHA-256 of the dump of its tree (ast.dump) without and with the positions. These
// tests need Go alone, and the fetched corpus "cpython": the data says where each text is in Lib/test (see
// corpora.Place) and has the first 8 digits of its hash, which is checked.

type reference struct {
	corpora.Place
	Path   string `json:"path"` // of a file, under Lib/test
	OK     bool   `json:"ok"`
	Hash   string `json:"hash"`
	PHash  string `json:"phash"`
	H      string `json:"h"` // the start of the SHA-256 of the source
	Source string `json:"-"` // read from the corpus
}

// readReferences reads the references in name and the text of each from the fetched CPython. The test is skipped if
// the corpus is not fetched. A text whose hash is not the recorded one, or a count other than want, is a failure.
func readReferences(t *testing.T, name string, want int) []reference {
	t.Helper()
	lib := filepath.Join(corporatest.Dir(t, "cpython"), "Lib", "test")
	files := &corpora.Files{Dir: lib}
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
		var text []byte
		if r.Path != "" {
			text, err = files.Read(r.Path)
			if err == nil && bytes.HasPrefix(text, []byte("\xef\xbb\xbf")) {
				text = text[3:]
			}
		} else {
			text, err = files.Text(r.Place)
		}
		if err != nil {
			t.Fatalf("%s: line %d: %v", name, len(refs)+1, err)
		}
		if got := corpora.Hash(text, len(r.H)); got != r.H || r.H == "" {
			t.Fatalf("%s: line %d (%s %d+%d): the text has hash %s, the data is about %s", name, len(refs)+1, r.File, r.Off, r.Size, got, r.H)
		}
		r.Source = string(text)
		refs = append(refs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(refs) != want {
		t.Fatalf("%s: %d references, want %d", name, len(refs), want)
	}
	return refs
}

// checkReferences compares the parser with the references: the same inputs accepted, with the same
// ast.dump and the same positions, and ParseAST and the interpreter in agreement.
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
		agreeWithInterpreter(t, r.Source)
		_, aerr := python.ParseAST(r.Source)
		if _, rerr := python.ParseAST(r.Source, python.WithUnit(python.Bytes)); utf8.ValidString(r.Source) && (rerr == nil) != (aerr == nil) {
			report("%q: ParseAST: %v, in bytes: %v", name, aerr, rerr)
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
	checkReferences(t, readReferences(t, "testdata/cpython-snippets.jsonl.gz", 61540))
}

func TestReferenceFiles(t *testing.T) {
	checkReferences(t, readReferences(t, "testdata/cpython-files.jsonl.gz", 13))
}
