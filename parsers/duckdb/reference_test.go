package duckdb_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/internal/corpora"
	"github.com/ornew/zeroparse/internal/corpora/corporatest"
	"github.com/ornew/zeroparse/parsers/duckdb"
	"github.com/ornew/zeroparse/parsers/duckdb/types"
)

// The reference data in testdata/reference is what DuckDB 1.5.6 says about each text: whether its parser accepts
// it, how it splits it into statements and, for a SELECT, the AST that json_serialize_sql returns (reduced to a
// canonical form). internal/refgen makes it; the README of testdata explains the files.
//
// The texts of the tests of DuckDB (tests.jsonl.gz) and the mutants made of them (mutants.jsonl.gz) are DuckDB's, so the
// files do not hold them: each line says where its text is in test/sql of the fetched corpus "duckdb" (a mutant: in
// which test, and how it was changed), and has the start of its hash, which is checked.

// A refStmt is a statement as DuckDB splits a script. Text is omitted when it is the whole text; where DuckDB
// rewrote the statement, the file has the pieces that make Text from the text of the case instead.
type refStmt struct {
	Type   string          `json:"type"`
	Text   string          `json:"text"`
	Pieces []corpora.Piece `json:"p"`
}

// A refCase is a text and what DuckDB says about it.
type refCase struct {
	SQL    string            `json:"sql"`
	File   string            `json:"file"`
	Off    int               `json:"o"` // of a test: the offset and size of its lines in File, a file of the corpus
	Size   int               `json:"n"`
	Base   string            `json:"base"` // of a mutant: the hash of the test that it is made from
	Pieces []corpora.Piece   `json:"p"`    // how the text is made from its lines (a test) or from the test (a mutant)
	Hash   string            `json:"h"`    // the start of the SHA-256 of the text
	Gen    string            `json:"gen"`
	OK     bool              `json:"ok"`
	Kind   string            `json:"kind"` // of a rejection: "syntax" or "semantic"
	Err    string            `json:"err"`
	Stmts  []refStmt         `json:"stmts"`
	Shapes []json.RawMessage `json:"shapes"`
}

var (
	refMu    sync.Mutex
	refCache = map[string][]refCase{}
)

// refCounts are the numbers of texts of the reference files, which the tests require: a file that lost lines must not
// pass with fewer cases.
var refCounts = map[string]int{"tests": 45912, "exprs": 7655, "lexical": 1219, "keywords": 83619, "mutants": 20000, "found": 200}

var testTexts = map[string]string{} // the texts of tests.jsonl.gz by hash, which the mutants are made of

// loadRef reads testdata/reference/<name>.jsonl.gz and, for the tests and the mutants, makes their texts from the
// fetched corpus "duckdb". The test is skipped if the corpus is not fetched.
func loadRef(t testing.TB, name string) []refCase {
	t.Helper()
	refMu.Lock()
	defer refMu.Unlock()
	return loadRefLocked(t, name)
}

func loadRefLocked(t testing.TB, name string) []refCase {
	t.Helper()
	if c, ok := refCache[name]; ok {
		return c
	}
	var files *corpora.Files
	if name == "tests" || name == "mutants" {
		files = &corpora.Files{Dir: corporatest.Dir(t, "duckdb")}
		if name == "mutants" {
			loadRefLocked(t, "tests")
		}
	}
	f, err := os.Open(filepath.Join("testdata", "reference", name+".jsonl.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var cases []refCase
	for sc.Scan() {
		var c refCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if files != nil {
			var text []byte
			var err error
			if name == "tests" {
				text, err = files.Text(corpora.Place{File: c.File, Off: c.Off, Size: c.Size, Pieces: c.Pieces})
			} else if base, ok := testTexts[c.Base]; !ok {
				err = fmt.Errorf("no test has the hash %q", c.Base)
			} else {
				text, err = corpora.Assemble([]byte(base), c.Pieces)
			}
			if err != nil {
				t.Fatalf("%s: line %d: %v", name, len(cases)+1, err)
			}
			if got := corpora.Hash(text, len(c.Hash)); c.Hash == "" || got != c.Hash {
				t.Fatalf("%s: line %d (%s): the text has hash %s, the data is about %s", name, len(cases)+1, c.File, got, c.Hash)
			}
			c.SQL = string(text)
			if name == "tests" {
				testTexts[c.Hash] = c.SQL
			}
		}
		for i := range c.Stmts {
			switch {
			case c.Stmts[i].Pieces != nil:
				text, err := corpora.Assemble([]byte(c.SQL), c.Stmts[i].Pieces)
				if err != nil {
					t.Fatalf("%s: line %d: statement %d: %v", name, len(cases)+1, i, err)
				}
				c.Stmts[i].Text = string(text)
			case c.Stmts[i].Text == "" && len(c.Stmts) == 1:
				c.Stmts[i].Text = c.SQL
			}
		}
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if want, ok := refCounts[name]; ok && len(cases) != want {
		t.Fatalf("%s: %d texts, want %d", name, len(cases), want)
	}
	refCache[name] = cases
	return cases
}

// refCorpora are the reference files that the tests compare with.
var refCorpora = []string{"tests", "exprs", "lexical", "keywords", "mutants", "found"}

// readDeviations reads testdata/deviations.jsonl: one JSON object per line, {"sql": ..., "why": ...}, for a text on
// which the parser and DuckDB's parser are known to differ in acceptance (the README explains each).
func readDeviations(t testing.TB) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "deviations.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var d struct{ SQL, Why string }
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		m[d.SQL] = d.Why
	}
	return m
}

// sample returns every n-th case in -short mode.
func sample(cases []refCase, n int) []refCase {
	if !testing.Short() || n <= 1 {
		return cases
	}
	var out []refCase
	for i := 0; i < len(cases); i += n {
		out = append(out, cases[i])
	}
	return out
}

// TestReferenceAcceptance compares the acceptance of every text with DuckDB's. A text that DuckDB rejects with a
// syntax error and the parser accepts, or the other way round, is a failure unless testdata/deviations.jsonl lists
// it. A text that DuckDB rejects after parsing (the transformer, for example, refuses a window function
// with EXCLUDE) is counted: the grammar cannot know.
func TestReferenceAcceptance(t *testing.T) {
	dev := readDeviations(t)
	deviated := map[string]bool{}
	complete := true // a corpus that is skipped cannot show that a deviation is gone
	for _, name := range refCorpora {
		ran := false
		t.Run(name, func(t *testing.T) {
			cases := sample(loadRef(t, name), 8)
			ran = true
			checkAcceptance(t, cases, dev, deviated)
		})
		complete = complete && ran
	}
	if testing.Short() || !complete {
		return
	}
	// a deviation that is no longer one must be removed from the list
	var gone []string
	for sql := range dev {
		if !deviated[sql] {
			gone = append(gone, sql)
		}
	}
	sort.Strings(gone)
	for _, sql := range gone {
		t.Errorf("testdata/deviations.jsonl lists %q, on which the parser and DuckDB agree", sql)
	}
}

// checkAcceptance compares what the parser and DuckDB say about each text, and reports the differences that are not
// known deviations.
func checkAcceptance(t *testing.T, cases []refCase, dev map[string]string, deviated map[string]bool) {
	t.Helper()
	var (
		bothAccept, bothReject, semantic, falseNeg, falsePos, known int
		unitDiffers                                                 int
		failures                                                    []string
	)
	for _, c := range cases {
		_, err := duckdb.ParseAST(c.SQL)
		_, rerr := duckdb.ParseAST(c.SQL, duckdb.WithUnit(duckdb.Bytes))
		if (err == nil) != (rerr == nil) {
			unitDiffers++
			failures = append(failures, fmt.Sprintf("ParseAST differs between code points and bytes on %q: %v, %v", c.SQL, err, rerr))
		}
		mine := err == nil
		_, isKnown := dev[c.SQL]
		switch {
		case mine && c.OK:
			bothAccept++
		case !mine && !c.OK:
			bothReject++
		case !mine && c.OK:
			if isKnown {
				known++
				deviated[c.SQL] = true
				continue
			}
			falseNeg++
			failures = append(failures, fmt.Sprintf("DuckDB accepts, the parser rejects: %q: %v", c.SQL, err))
		case mine && !c.OK && c.Kind == "semantic":
			semantic++
		default:
			if isKnown {
				known++
				deviated[c.SQL] = true
				continue
			}
			falsePos++
			failures = append(failures, fmt.Sprintf("DuckDB rejects (%s), the parser accepts: %q", c.Err, c.SQL))
		}
	}
	t.Logf("%d texts: both accept %d, both reject %d, DuckDB rejects after parsing %d, known deviations %d, "+
		"parser rejects what DuckDB accepts %d, parser accepts what DuckDB rejects %d",
		len(cases), bothAccept, bothReject, semantic, known, falseNeg, falsePos)
	sort.Strings(failures)
	for i, f := range failures {
		if i == 20 {
			t.Errorf("... and %d more", len(failures)-i)
			break
		}
		t.Error(f)
	}
}

// expanded reports whether DuckDB turns the statement into several (PIVOT, COPY FROM DATABASE and ALTER TABLE ...
// ADD COLUMN ... DEFAULT make helper statements).
func expanded(s types.Statement) bool {
	switch s := s.(type) {
	case *types.CopyDatabaseStmt:
		return true
	case *types.AlterTableStmt:
		return true
	case *types.PrepareStmt:
		return expanded(s.Stmt)
	case *types.CreateTableAsStmt, *types.CreateViewStmt:
		found := false
		duckdb.Inspect(s, func(n any) bool {
			switch n.(type) {
			case *types.PivotQuery, *types.UnpivotQuery:
				found = true
			}
			return !found
		})
		return found
	case *types.Select:
		found := false
		duckdb.Inspect(s, func(n any) bool {
			switch n.(type) {
			case *types.PivotQuery, *types.UnpivotQuery:
				found = true
			}
			return !found
		})
		return found
	}
	return false
}

// TestReferenceStatements compares how a script is split into statements, the types DuckDB gives the statements
// and their texts.
func TestReferenceStatements(t *testing.T) {
	for _, name := range []string{"tests", "lexical", "mutants", "found"} {
		t.Run(name, func(t *testing.T) {
			cases := sample(loadRef(t, name), 8)
			var compared, textsCompared, expandedN int
			var failures []string
			for _, c := range cases {
				if !c.OK || c.Stmts == nil {
					continue
				}
				sts, err := duckdb.Split(c.SQL)
				if err != nil {
					continue // acceptance is checked by TestReferenceAcceptance
				}
				anyExpanded := false
				for _, s := range sts {
					if expanded(s.Statement) {
						anyExpanded = true
					}
				}
				if anyExpanded {
					expandedN++
					continue
				}
				compared++
				if len(sts) != len(c.Stmts) {
					failures = append(failures, fmt.Sprintf("%d statements, DuckDB has %d: %q", len(sts), len(c.Stmts), c.SQL))
					continue
				}
				for i, s := range sts {
					want := c.Stmts[i]
					kind := duckdb.Kind(s.Statement)
					_, isPragma := s.Statement.(*types.PragmaStmt)
					if kind != want.Type && !isPragma {
						failures = append(failures, fmt.Sprintf("statement %d of %q is a %s, DuckDB says %s", i, c.SQL, kind, want.Type))
						continue
					}
					if isPragma || want.Text != s.Text {
						// DuckDB rewrites PRAGMA statements: their text is another one
						if !isPragma {
							failures = append(failures, fmt.Sprintf("statement %d of %q has text %q, DuckDB has %q", i, c.SQL, s.Text, want.Text))
						}
						continue
					}
					textsCompared++
				}
			}
			t.Logf("%d scripts compared, %d statements with the same text, %d not compared because DuckDB expands the statement", compared, textsCompared, expandedN)
			sort.Strings(failures)
			for i, f := range failures {
				if i == 20 {
					t.Errorf("... and %d more", len(failures)-i)
					break
				}
				t.Error(f)
			}
		})
	}
}

// TestReferenceFile compares the parser with DuckDB on the texts of another file made by internal/refgen (a plain or
// gzipped JSON lines file, named by REFERENCE_FILE), to run a larger or another corpus than the recorded ones, for
// example the mutations of the tests with another seed (the README of testdata gives the commands).
func TestReferenceFile(t *testing.T) {
	path := os.Getenv("REFERENCE_FILE")
	if path == "" {
		t.Skip("set REFERENCE_FILE to a file that internal/refgen/refgen.py made")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		r = zr
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var cases []refCase
	for sc.Scan() {
		var c refCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	checkAcceptance(t, cases, readDeviations(t), map[string]bool{})
	// the ASTs of the SELECT statements, if the file has them (refgen.py without --no-shapes)
	compared, same := checkShapes(t, cases)
	if compared != same {
		t.Errorf("%d of %d SELECT statements differ from DuckDB's AST", compared-same, compared)
	}
}
