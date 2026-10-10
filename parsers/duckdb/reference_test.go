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

	"github.com/ornew/pego/parsers/duckdb"
)

// The reference data in testdata/reference is what DuckDB 1.5.6 says about each text: whether its parser accepts
// it, how it splits it into statements and, for a SELECT, the AST that json_serialize_sql returns (reduced to a
// canonical form). internal/refgen makes it; the README of testdata explains the files.

// A refStmt is a statement as DuckDB splits a script. Text is omitted when it is the whole text.
type refStmt struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// A refCase is a text and what DuckDB says about it.
type refCase struct {
	SQL    string            `json:"sql"`
	File   string            `json:"file"`
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

// loadRef reads testdata/reference/<name>.jsonl.gz.
func loadRef(t testing.TB, name string) []refCase {
	t.Helper()
	refMu.Lock()
	defer refMu.Unlock()
	if c, ok := refCache[name]; ok {
		return c
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
		for i := range c.Stmts {
			if c.Stmts[i].Text == "" && len(c.Stmts) == 1 {
				c.Stmts[i].Text = c.SQL
			}
		}
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	refCache[name] = cases
	return cases
}

// refCorpora are the reference files that the tests compare with.
var refCorpora = []string{"tests", "exprs", "lexical", "keywords", "mutants", "found", "limit_percent"}

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
	for _, name := range refCorpora {
		t.Run(name, func(t *testing.T) {
			checkAcceptance(t, sample(loadRef(t, name), 8), dev, deviated)
		})
	}
	if testing.Short() {
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
		recognizeDiffers                                            int
		failures                                                    []string
	)
	for _, c := range cases {
		_, err := duckdb.ParseAST(c.SQL)
		rerr := duckdb.Recognize(c.SQL)
		if (err == nil) != (rerr == nil) {
			recognizeDiffers++
			failures = append(failures, fmt.Sprintf("ParseAST and Recognize differ on %q: %v, %v", c.SQL, err, rerr))
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
func expanded(s duckdb.Statement) bool {
	switch s := s.(type) {
	case *duckdb.CopyDatabaseStmt:
		return true
	case *duckdb.AlterTableStmt:
		return true
	case *duckdb.PrepareStmt:
		return expanded(s.Stmt)
	case *duckdb.CreateTableAsStmt, *duckdb.CreateViewStmt:
		found := false
		duckdb.Walk(s, func(n any) bool {
			switch n.(type) {
			case *duckdb.PivotQuery, *duckdb.UnpivotQuery:
				found = true
			}
			return !found
		})
		return found
	case *duckdb.Select:
		found := false
		duckdb.Walk(s, func(n any) bool {
			switch n.(type) {
			case *duckdb.PivotQuery, *duckdb.UnpivotQuery:
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
	for _, name := range []string{"tests", "lexical", "mutants", "found", "limit_percent"} {
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
					_, isPragma := s.Statement.(*duckdb.PragmaStmt)
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
// gzipped JSON lines file, named by REFERENCE_FILE), to run a larger or another corpus than the vendored ones, for
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
