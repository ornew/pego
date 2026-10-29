package engine

import (
	"testing"

	gjson "github.com/ornew/pego/internal/bench/gen/json"
	gxml "github.com/ornew/pego/internal/bench/gen/xml"
	"github.com/ornew/pego/internal/bench/inputs"
)

// BenchmarkSparseMemoDenseGenerated exercises large inputs whose generated
// rule tables stay below the sparse cutoff, including recognition in both units.
func BenchmarkSparseMemoDenseGenerated(b *testing.B) {
	for _, w := range []struct{ name, input string }{
		{"JSON", inputs.JSONInput(256 << 10)},
		{"XML", inputs.XMLInput(256 << 10)},
	} {
		for _, unit := range []struct {
			name string
			byte bool
		}{{"CodePoints", false}, {"Bytes", true}} {
			for _, api := range []string{"Node", "AST", "Recognize"} {
				b.Run(w.name+"/"+api+"/"+unit.name, func(b *testing.B) {
					run := func() error {
						if w.name == "JSON" {
							u := gjson.CodePoints
							if unit.byte {
								u = gjson.Bytes
							}
							switch api {
							case "Node":
								_, err := gjson.Parse(w.input, gjson.WithUnit(u))
								return err
							case "AST":
								_, err := gjson.ParseAST(w.input, gjson.WithUnit(u))
								return err
							default:
								return gjson.Recognize(w.input, gjson.WithUnit(u))
							}
						}
						u := gxml.CodePoints
						if unit.byte {
							u = gxml.Bytes
						}
						switch api {
						case "Node":
							_, err := gxml.Parse(w.input, gxml.WithUnit(u))
							return err
						case "AST":
							_, err := gxml.ParseAST(w.input, gxml.WithUnit(u))
							return err
						default:
							return gxml.Recognize(w.input, gxml.WithUnit(u))
						}
					}
					if err := run(); err != nil {
						b.Fatal(err)
					}
					b.SetBytes(int64(len(w.input)))
					b.ReportAllocs()
					for b.Loop() {
						if err := run(); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
