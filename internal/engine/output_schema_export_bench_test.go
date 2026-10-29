package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/schema"
)

var outputSchemaExportSink *schema.Output
var outputSchemaExportJSONSink []byte

func BenchmarkOutputSchemaExport(b *testing.B) {
	cases := []struct{ name, source string }{
		{"Scalar", `type Holder = { Value string }; rule main = "x" => Holder{Value: "x"}`},
		{"Repetition", `rule main = "x"*`},
	}
	for _, count := range []int{1, 128} {
		var source strings.Builder
		source.WriteString("type Token = { Text string }; type Holder = { ")
		for i := range count {
			fmt.Fprintf(&source, "F%03d []Token; ", i)
		}
		source.WriteString(`}; rule main = "x" => Holder{}`)
		cases = append(cases, struct{ name, source string }{fmt.Sprintf("ListFields%d", count), source.String()})
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			g, err := parseTest(tc.source)
			if err != nil {
				b.Fatal(err)
			}
			p, err := Compile(g, compile.Options{})
			if err != nil {
				b.Fatal(err)
			}
			schema, err := outputSchemaPrototype(p, "main", false)
			if err != nil {
				b.Fatal(err)
			}
			encoded, err := json.Marshal(schema)
			if err != nil {
				b.Fatal(err)
			}
			for _, serialize := range []bool{false, true} {
				b.Run(fmt.Sprintf("JSON%t", serialize), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						s, err := outputSchemaPrototype(p, "main", false)
						if err != nil {
							b.Fatal(err)
						}
						outputSchemaExportSink = s
						if serialize {
							data, err := json.Marshal(s)
							if err != nil {
								b.Fatal(err)
							}
							outputSchemaExportJSONSink = data
						}
					}
					b.ReportMetric(float64(len(encoded)), "B/schema")
				})
			}
		})
	}
}
