package csv_test

import (
	stdcsv "encoding/csv"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/csv"
)

// benchInput is a file of 5,000 records (about 210 KB) with quoted fields that contain commas, doubled
// quotes and line breaks, like the CSV workload of the benchmarks of PEGO (bench/ in
// github.com/ornew/pego).
var benchInput = func() string {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	r := rand.New(rand.NewPCG(1, 2))
	var b strings.Builder
	b.WriteString("id,name,city,amount,note,flag\n")
	for i := range 5000 {
		note := words[r.IntN(len(words))]
		switch r.IntN(6) {
		case 0:
			note = `"` + note + `, ` + words[r.IntN(len(words))] + `"`
		case 1:
			note = `"say ""` + note + `"""`
		case 2:
			note = "\"" + note + "\nnext line\""
		}
		fmt.Fprintf(&b, "%d,%s,%s,%d.%02d,%s,%t\n", i, words[r.IntN(len(words))], words[r.IntN(len(words))],
			r.IntN(100000), r.IntN(100), note, r.IntN(2) == 0)
	}
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := csv.ParseAST(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseASTBytes(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := csv.ParseAST(benchInput, csv.WithUnit(csv.Bytes)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecords(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := csv.Records(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValid(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if !csv.Valid(benchInput) {
			b.Fatal("invalid")
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := csv.Parse(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

// The standard library, for comparison: ReadAll returns the records as Records does.

func BenchmarkEncodingCSVReadAll(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		r := stdcsv.NewReader(strings.NewReader(benchInput))
		r.FieldsPerRecord = -1
		if _, err := r.ReadAll(); err != nil {
			b.Fatal(err)
		}
	}
}
