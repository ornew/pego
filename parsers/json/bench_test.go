package json_test

import (
	stdjson "encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/json"
)

// benchInput is an array of objects of about 256 KB, like the JSON workload of the benchmarks of PEGO
// (bench/ in github.com/ornew/pego).
var benchInput = func() string {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	r := rand.New(rand.NewPCG(1, 2))
	var b strings.Builder
	b.WriteString("[\n")
	for i := 0; b.Len() < 256<<10; i++ {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `  {"id": %d, "name": "%s \"%s\"", "score": %d.%02d, "active": %t, "tags": ["%s", "%s"], "parent": null, "pos": {"x": %d, "y": -%d.5e3}}`,
			i, words[r.IntN(8)], words[r.IntN(8)], r.IntN(1000), r.IntN(100), r.IntN(2) == 0, words[r.IntN(8)], words[r.IntN(8)], r.IntN(100), r.IntN(100))
	}
	b.WriteString("\n]\n")
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := json.ParseAST(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := json.Decode(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValid(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if !json.Valid(benchInput) {
			b.Fatal("invalid")
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := json.Parse(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

// The standard library, for comparison.

func BenchmarkEncodingJSONUnmarshal(b *testing.B) {
	data := []byte(benchInput)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var v any
		if err := stdjson.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodingJSONValid(b *testing.B) {
	data := []byte(benchInput)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if !stdjson.Valid(data) {
			b.Fatal("invalid")
		}
	}
}
