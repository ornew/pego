package duckdb_test

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/duckdb"
)

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := duckdb.Parse(string(data))
		if n != nil {
			got = n.String()
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

// find returns the nodes of type T of the tree that parsing sql gives.
func find[T any](t *testing.T, sql string) []*T {
	t.Helper()
	script, err := duckdb.ParseAST(sql, duckdb.Bytes)
	if err != nil {
		t.Fatalf("%q: %v", sql, err)
	}
	var out []*T
	duckdb.Walk(script, func(n any) bool {
		if v, ok := n.(*T); ok {
			out = append(out, v)
		}
		return true
	})
	return out
}

func TestIdent(t *testing.T) {
	for _, tc := range []struct {
		sql    string
		name   string
		fold   string
		quoted bool
	}{
		{`SELECT FooBar`, "FooBar", "foobar", false},
		{`SELECT "FooBar"`, "FooBar", "foobar", true},
		{`SELECT "a""b"`, `a"b`, `a"b`, true},
		{`SELECT "Ünï"`, "Ünï", "Ünï", true},
		{`SELECT ÄB`, "ÄB", "Äb", false},
		{`SELECT U&"d\0061ta"`, "data", "data", true},
		{`SELECT U&"d!0061ta" UESCAPE '!'`, "data", "data", true},
		{`SELECT "x" FROM t AS 'Alias'`, "x", "x", true},
	} {
		ids := find[duckdb.Ident](t, tc.sql)
		if len(ids) == 0 {
			t.Fatalf("%q: no identifier", tc.sql)
		}
		id := ids[0]
		if id.Name() != tc.name || id.Fold() != tc.fold || id.Quoted() != tc.quoted {
			t.Errorf("%q: Name %q, Fold %q, Quoted %v; want %q, %q, %v", tc.sql, id.Name(), id.Fold(), id.Quoted(), tc.name, tc.fold, tc.quoted)
		}
	}
	ids := find[duckdb.Ident](t, `SELECT a.B FROM "A"`)
	if !ids[0].EqualFold(ids[2]) || ids[1].EqualFold(ids[0]) {
		t.Error("EqualFold")
	}
	names := find[duckdb.Name](t, `SELECT 1 FROM c."S".t`)
	if len(names) != 1 || strings.Join(names[0].Names(), "|") != "c|S|t" {
		t.Errorf("Names: %v", names)
	}
}

func TestStringValue(t *testing.T) {
	for _, tc := range []struct {
		lit  string
		want string
		kind duckdb.StringKind
	}{
		{`'it''s'`, "it's", duckdb.StandardString},
		{`'a\nb'`, `a\nb`, duckdb.StandardString},
		{"'a'\n'b'", "ab", duckdb.StandardString},
		{`E'a\nb\x41B\101'`, "a\nbAB" + "A", duckdb.EscapeString},
		{`e'\''`, "'", duckdb.EscapeString},
		{`U&'d\0061t\+000061'`, "data", duckdb.UnicodeString},
		{`$$a'b$$`, "a'b", duckdb.DollarQuoted},
		{`$tag$a$$b$tag$`, "a$$b", duckdb.DollarQuoted},
		{`''`, "", duckdb.StandardString},
	} {
		lits := find[duckdb.StringLit](t, "SELECT "+tc.lit)
		if len(lits) != 1 {
			t.Fatalf("%s: %d literals", tc.lit, len(lits))
		}
		if got := lits[0].Value(); got != tc.want {
			t.Errorf("%s: Value %q, want %q", tc.lit, got, tc.want)
		}
		if lits[0].Kind() != tc.kind {
			t.Errorf("%s: Kind %v, want %v", tc.lit, lits[0].Kind(), tc.kind)
		}
	}
}

func TestBitLit(t *testing.T) {
	for _, tc := range []struct {
		lit    string
		digits string
		hex    bool
	}{{`B'0101'`, "0101", false}, {`x'FF'`, "FF", true}, {"b'01'\n'10'", "0110", false}} {
		lits := find[duckdb.BitLit](t, "SELECT "+tc.lit)
		if len(lits) != 1 {
			t.Fatalf("%s: %d literals", tc.lit, len(lits))
		}
		if d, h := lits[0].Bits(); d != tc.digits || h != tc.hex {
			t.Errorf("%s: Bits %q, %v; want %q, %v", tc.lit, d, h, tc.digits, tc.hex)
		}
	}
}

func TestNumber(t *testing.T) {
	for _, tc := range []struct {
		lit   string
		typ   string
		i     int64
		isInt bool
		w, s  int
	}{
		{"1", "INTEGER", 1, true, 0, 0},
		{"1_000", "INTEGER", 1000, true, 0, 0},
		{"2147483648", "BIGINT", 2147483648, true, 0, 0},
		{"9223372036854775808", "HUGEINT", 0, false, 0, 0},
		{"123456789012345678901234567890123456789012", "DOUBLE", 0, false, 0, 0},
		{"1.50", "DECIMAL", 0, false, 3, 2},
		{"0.05", "DECIMAL", 0, false, 3, 2},
		{"00.5", "DECIMAL", 0, false, 3, 1},
		{".5", "DECIMAL", 0, false, 1, 1},
		{"1e3", "DOUBLE", 0, false, 0, 0},
		{"1.5E-3", "DOUBLE", 0, false, 0, 0},
	} {
		ns := find[duckdb.Number](t, "SELECT "+tc.lit)
		if len(ns) != 1 {
			t.Fatalf("%s: %d numbers", tc.lit, len(ns))
		}
		n := ns[0]
		if n.Type() != tc.typ {
			t.Errorf("%s: Type %s, want %s", tc.lit, n.Type(), tc.typ)
		}
		if v, ok := n.Int64(); ok != (tc.isInt && tc.typ != "HUGEINT") || ok && v != tc.i {
			t.Errorf("%s: Int64 %d, %v", tc.lit, v, ok)
		}
		if w, s := n.DecimalWidthScale(); w != tc.w || s != tc.s {
			t.Errorf("%s: DecimalWidthScale %d, %d; want %d, %d", tc.lit, w, s, tc.w, tc.s)
		}
	}
	n := find[duckdb.Number](t, "SELECT 1_5.25")[0]
	if f, err := n.Float64(); err != nil || f != 15.25 {
		t.Errorf("Float64 %v, %v", f, err)
	}
	n = find[duckdb.Number](t, "SELECT 1e999")[0]
	if f, err := n.Float64(); err == nil || !math.IsInf(f, 1) {
		t.Errorf("Float64 of an overflow: %v, %v", f, err)
	}
}

func TestParam(t *testing.T) {
	ps := find[duckdb.Param](t, "SELECT ?, ?2, $3, $name")
	if len(ps) != 4 {
		t.Fatalf("%d parameters", len(ps))
	}
	if !ps[0].Positional() {
		t.Error("? is positional")
	}
	if i, ok := ps[1].Index(); !ok || i != 2 {
		t.Errorf("?2: %d, %v", i, ok)
	}
	if i, ok := ps[2].Index(); !ok || i != 3 {
		t.Errorf("$3: %d, %v", i, ok)
	}
	if n, ok := ps[3].ParamName(); !ok || n != "name" {
		t.Errorf("$name: %q, %v", n, ok)
	}
	if _, ok := ps[3].Index(); ok {
		t.Error("$name has no index")
	}
}

func TestKeywords(t *testing.T) {
	for _, tc := range []struct {
		word string
		cat  duckdb.KeywordCategory
	}{
		{"select", duckdb.Reserved},
		{"SELECT", duckdb.Reserved},
		{"year", duckdb.Unreserved},
		{"int", duckdb.ColumnName},
		{"left", duckdb.TypeFunction},
	} {
		if c, ok := duckdb.Keyword(tc.word); !ok || c != tc.cat {
			t.Errorf("Keyword(%q) = %v, %v; want %v", tc.word, c, ok, tc.cat)
		}
	}
	if _, ok := duckdb.Keyword("frobnicate"); ok {
		t.Error("frobnicate is not a keyword")
	}
	if n := len(duckdb.Keywords()); n != 489 {
		t.Errorf("%d keywords, DuckDB 1.5.6 has 489", n)
	}
	for w, c := range duckdb.Keywords() {
		if w != strings.ToLower(w) || c.String() == "none" {
			t.Errorf("keyword %q: %v", w, c)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"abc", "abc"},
		{"a_1$", "a_1$"},
		{"year", "year"},
		{"select", `"select"`},
		{"left", `"left"`},
		{"has space", `"has space"`},
		{`a"b`, `"a""b"`},
		{"1a", `"1a"`},
		{"", `""`}, // DuckDB does not accept it, there is no way to write an empty name
	} {
		got := duckdb.QuoteIdent(tc.name)
		if got != tc.want {
			t.Errorf("QuoteIdent(%q) = %s, want %s", tc.name, got, tc.want)
			continue
		}
		if tc.name == "" {
			continue
		}
		// what it returns reads back as the name
		ids := find[duckdb.Ident](t, "SELECT "+got)
		if len(ids) != 1 || ids[0].Name() != tc.name {
			t.Errorf("QuoteIdent(%q) = %s does not read back", tc.name, got)
		}
	}
}

func TestKind(t *testing.T) {
	for _, tc := range []struct{ sql, kind string }{
		{"SELECT 1", "SELECT"},
		{"VALUES (1)", "SELECT"},
		{"FROM t", "SELECT"},
		{"WITH a AS (SELECT 1) SELECT * FROM a", "SELECT"},
		{"INSERT INTO t VALUES (1)", "INSERT"},
		{"UPDATE t SET a = 1", "UPDATE"},
		{"DELETE FROM t", "DELETE"},
		{"CREATE TABLE t (a INT)", "CREATE"},
		{"CREATE VIEW v AS SELECT 1", "CREATE"},
		{"ALTER TABLE t ADD COLUMN b INT", "ALTER"},
		{"DROP TABLE t", "DROP"},
		{"COPY t TO 'f.csv'", "COPY"},
		{"SET threads = 1", "SET"},
		{"USE db", "SET"},
		{"PRAGMA version", "PRAGMA"},
		{"EXPLAIN SELECT 1", "EXPLAIN"},
		{"BEGIN", "TRANSACTION"},
		{"COMMIT", "TRANSACTION"},
		{"PREPARE p AS SELECT 1", "PREPARE"},
		{"EXECUTE p", "EXECUTE"},
		{"ATTACH 'f.db'", "ATTACH"},
		{"LOAD httpfs", "LOAD"},
		{"VACUUM", "VACUUM"},
		{"CHECKPOINT", "CALL"},
		{"CALL f()", "CALL"},
	} {
		script, err := duckdb.ParseAST(tc.sql)
		if err != nil {
			t.Errorf("%q: %v", tc.sql, err)
			continue
		}
		if len(script.Statements) != 1 {
			t.Errorf("%q: %d statements", tc.sql, len(script.Statements))
			continue
		}
		if k := duckdb.Kind(script.Statements[0]); k != tc.kind {
			t.Errorf("%q: Kind %s, want %s", tc.sql, k, tc.kind)
		}
	}
}

func TestSplit(t *testing.T) {
	// DuckDB records for each statement the text between the semicolons that surround it; the last runs to the end
	// of the script.
	sts, err := duckdb.Split("SELECT 1; -- two\n  SELECT 2 ;SELECT 3;;  ")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, st := range sts {
		got = append(got, st.Text)
	}
	want := []string{"SELECT 1", " -- two\n  SELECT 2 ", "SELECT 3;;  "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("texts %q, want %q", got, want)
	}
	if _, err := duckdb.Split("SELECT 1; SELECT FROM FROM"); err == nil {
		t.Error("a script with a syntax error is not split")
	}
	sts, err = duckdb.Split("")
	if err != nil || len(sts) != 0 {
		t.Errorf("empty script: %v, %v", sts, err)
	}
	// white space of Unicode is a space in the text of a statement, but not in a string
	sts, err = duckdb.Split("SELECT 'a b' ; SELECT 2 ")
	if err != nil {
		t.Fatal(err)
	}
	if sts[0].Text != "SELECT 'a b' " || sts[1].Text != " SELECT 2 " {
		t.Errorf("texts %q, %q", sts[0].Text, sts[1].Text)
	}
}

// TestSpans checks the spans of the nodes against the input, and that a node covers its children.
func TestSpans(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		script, err := duckdb.ParseAST(src, duckdb.Bytes)
		if err != nil {
			continue // syntax_error.txt
		}
		var check func(parent duckdb.Span, n any)
		check = func(parent duckdb.Span, n any) {
			duckdb.Walk(n, func(c any) bool {
				sp, ok := duckdb.SpanOf(c)
				if !ok {
					return true
				}
				if c == n {
					return true
				}
				if sp.Start < parent.Start || sp.End > parent.End || sp.Start > sp.End || sp.End > len(src) {
					t.Errorf("%s: %T spans %d-%d, outside %d-%d", in, c, sp.Start, sp.End, parent.Start, parent.End)
					return false
				}
				check(sp, c)
				return false
			})
		}
		sp, _ := duckdb.SpanOf(script)
		check(sp, script)
		// a terminal covers the text it holds
		duckdb.Walk(script, func(n any) bool {
			if id, ok := n.(*duckdb.Ident); ok {
				if got := src[id.Span.Start:id.Span.End]; got != id.Text {
					t.Errorf("%s: identifier %q at %d-%d covers %q", in, id.Text, id.Span.Start, id.Span.End, got)
				}
			}
			return true
		})
	}
}

func TestWalk(t *testing.T) {
	script, err := duckdb.ParseAST("SELECT a + 1 AS x FROM t WHERE b")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	duckdb.Walk(script, func(n any) bool {
		if id, ok := n.(*duckdb.Ident); ok {
			order = append(order, id.Text)
		}
		return true
	})
	if strings.Join(order, " ") != "a x t b" {
		t.Errorf("identifiers in the order of the input: %v", order)
	}
	// returning false skips the children
	var count int
	duckdb.Walk(script, func(n any) bool {
		if _, ok := n.(*duckdb.Select); ok {
			return false
		}
		count++
		return true
	})
	if count != 1 {
		t.Errorf("visited %d nodes outside the SELECT, want only the script", count)
	}
}

func TestSyntaxError(t *testing.T) {
	_, err := duckdb.ParseAST("SELECT 1\nFROM")
	se, ok := err.(*duckdb.SyntaxError)
	if !ok {
		t.Fatalf("error %T: %v", err, err)
	}
	if se.Line != 2 || se.Col != 5 {
		t.Errorf("error at %d:%d: %v", se.Line, se.Col, se)
	}
	if duckdb.Recognize("SELECT 1\nFROM") == nil || duckdb.Recognize("SELECT 1") != nil {
		t.Error("Recognize")
	}
}

// FuzzParse checks that ParseAST and Recognize agree, and that Split and Walk do not panic.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"SELECT 1", "SELECT a, b FROM t WHERE c IN (1, 2) ORDER BY 1", "FROM t SELECT [x for x in l]",
		"CREATE TABLE t (a INT DEFAULT 1, b VARCHAR[])", "SELECT $$a$$, U&'\\0041', E'\\n'", "SELECT 1; SELECT 2",
		"SELECT x -> x + 1", "SELECT a::INT, CAST(b AS DECIMAL(10, 2)) -- c", "/* a /* b */ c */ SELECT 1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		script, perr := duckdb.ParseAST(s)
		rerr := duckdb.Recognize(s)
		if (perr == nil) != (rerr == nil) {
			t.Fatalf("%q: ParseAST: %v, Recognize: %v", s, perr, rerr)
		}
		if perr == nil {
			duckdb.Walk(script, func(any) bool { return true })
			// Split counts bytes; a text that is not UTF-8 is read as U+FFFD by the other functions, where two different
			// bytes are the same character (the tags of a dollar-quoted string, for example), so it is left out
			if !utf8.ValidString(s) {
				return
			}
			if _, err := duckdb.Split(s); err != nil {
				t.Fatalf("%q: Split: %v", s, err)
			}
		}
	})
}
