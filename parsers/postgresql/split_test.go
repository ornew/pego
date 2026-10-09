package postgresql_test

import (
	"reflect"
	"testing"

	"github.com/ornew/pego/parsers/postgresql"
)

func TestSplit(t *testing.T) {
	for _, c := range []struct {
		src  string
		want []string
	}{
		{"", nil},
		{" ;; \n", nil},
		{"select 1", []string{"select 1"}},
		{"select 1; select 2;", []string{"select 1", "select 2"}},
		{"select ';' ; select \";\"", []string{"select ';'", `select ";"`}},
		{"select 'it''s;' ; x", []string{"select 'it''s;'", "x"}},
		{`select E'a\';b'; x`, []string{`select E'a\';b'`, "x"}},
		{"select $$a;b$$; x", []string{"select $$a;b$$", "x"}},
		{"select $a$ $$; $a$; x", []string{"select $a$ $$; $a$", "x"}},
		{"select 1 -- ;\n; x", []string{"select 1", "x"}},
		{"select /* ; /* ; */ ; */ 1; x", []string{"select /* ; /* ; */ ; */ 1", "x"}},
		{"select (1;2); x", []string{"select (1;2)", "x"}},
		{"create function f() returns int begin atomic select 1; select case when true then 1 end; end; select 2",
			[]string{"create function f() returns int begin atomic select 1; select case when true then 1 end; end", "select 2"}},
		{"begin; select 1; commit", []string{"begin", "select 1", "commit"}},
		{"select 'unterminated; x", []string{"select 'unterminated; x"}},
		{"select a$1; b", []string{"select a$1", "b"}},
	} {
		var got []string
		for _, s := range postgresql.Split(c.src) {
			got = append(got, c.src[s.Start:s.End])
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

// TestSplitCorpus checks that the statements of the regression scripts, which refgen split with its own
// splitter, are not split further (or dropped) by Split.
func TestSplitCorpus(t *testing.T) {
	bad := 0
	for _, s := range loadCorpus(t) {
		spans := postgresql.Split(s.SQL)
		if len(spans) != 1 {
			if bad++; bad <= 10 {
				t.Errorf("%s:%d: %d statements in %.80q", s.File, s.Line, len(spans), s.SQL)
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d statements are split differently", bad)
	}
}
