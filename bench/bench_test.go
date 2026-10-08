package bench

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"go/parser"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego"
	gcalc "github.com/ornew/pego/bench/gen/calc"
	gcalclr "github.com/ornew/pego/bench/gen/calclr"
	gcsv "github.com/ornew/pego/bench/gen/csv"
	gjson "github.com/ornew/pego/bench/gen/json"
	gminilang "github.com/ornew/pego/bench/gen/minilang"
	goutline "github.com/ornew/pego/bench/gen/outline"
	gxml "github.com/ornew/pego/bench/gen/xml"
)

// workload is the grammar and input of one benchmark.
type workload struct {
	name    string
	grammar string                 // path of a grammar in examples/
	input   func(scale int) string // input of 1/scale of the full size (benchmarks use 1)
	// gen parses with the generated parser (in byte positions if bytes is set), and rec recognizes
	// with it.
	gen func(input string, bytes bool) error
	rec func(input string) error
	// std is the standard library parser to compare with (nil if there is none).
	std     func(input string) error
	stdName string
	// partial treats a parse that recovered from errors as successful when it returns a tree.
	partial bool
}

func genErr[N any](_ N, err error) error { return err }

var workloads = []workload{
	{
		name: "JSON", grammar: "../examples/json/json.pego", input: func(k int) string { return JSONInput(256 << 10 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gjson.Parse(s, gjson.Bytes))
			}
			return genErr(gjson.Parse(s))
		},
		rec:     func(s string) error { return gjson.Recognize(s) },
		stdName: "encoding_json", std: func(s string) error {
			var v any
			return json.Unmarshal([]byte(s), &v)
		},
	},
	{
		name: "CSV", grammar: "../examples/csv/csv.pego", input: func(k int) string { return CSVInput(5000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gcsv.Parse(s, gcsv.Bytes))
			}
			return genErr(gcsv.Parse(s))
		},
		rec:     func(s string) error { return gcsv.Recognize(s) },
		stdName: "encoding_csv", std: func(s string) error {
			r := csv.NewReader(strings.NewReader(s))
			r.FieldsPerRecord = -1
			_, err := r.ReadAll()
			return err
		},
	},
	{
		name: "XML", grammar: "../examples/xml/xml.pego", input: func(k int) string { return XMLInput(256 << 10 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gxml.Parse(s, gxml.Bytes))
			}
			return genErr(gxml.Parse(s))
		},
		rec:     func(s string) error { return gxml.Recognize(s) },
		stdName: "encoding_xml", std: func(s string) error {
			d := xml.NewDecoder(strings.NewReader(s))
			for {
				if _, err := d.Token(); err == io.EOF {
					return nil
				} else if err != nil {
					return err
				}
			}
		},
	},
	{
		name: "Arith_Pratt", grammar: "../examples/calculator/calc.pego", input: func(k int) string { return ArithInput(20000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gcalc.Parse(s, gcalc.Bytes))
			}
			return genErr(gcalc.Parse(s))
		},
		rec:     func(s string) error { return gcalc.Recognize(s) },
		stdName: "go_parser", std: func(s string) error { return genErr(parser.ParseExpr(s)) },
	},
	{
		name: "Arith_LeftRec", grammar: "../examples/calculator/calc_lr.pego", input: func(k int) string { return ArithInput(20000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gcalclr.Parse(s, gcalclr.Bytes))
			}
			return genErr(gcalclr.Parse(s))
		},
		rec:     func(s string) error { return gcalclr.Recognize(s) },
		stdName: "go_parser", std: func(s string) error { return genErr(parser.ParseExpr(s)) },
	},
	{
		name: "Minilang", grammar: "../examples/minilang/minilang.pego", input: func(k int) string { return MinilangInput(300/k, 0) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gminilang.Parse(s, gminilang.Bytes))
			}
			return genErr(gminilang.Parse(s))
		},
		rec: func(s string) error { return gminilang.Recognize(s) },
	},
	{
		name: "Recovery", grammar: "../examples/minilang/minilang.pego", input: func(k int) string { return MinilangInput(300/k, 7) },
		partial: true,
		gen: func(s string, b bool) error {
			u := gminilang.CodePoints
			if b {
				u = gminilang.Bytes
			}
			if n, _ := gminilang.Parse(s, u); n == nil {
				return io.ErrUnexpectedEOF
			}
			return nil
		},
		rec: func(s string) error { return gminilang.Recognize(s) },
	},
	{
		name: "Outline", grammar: "../examples/outline/outline.pego", input: func(k int) string { return OutlineInput(5000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(goutline.Parse(s, goutline.Bytes))
			}
			return genErr(goutline.Parse(s))
		},
		rec: func(s string) error { return goutline.Recognize(s) },
	},
}

var backends = []struct {
	name string
	b    pego.Backend
}{
	{"closure", pego.Closure},
	{"bytecode", pego.Bytecode},
	{"iterative", pego.BytecodeIterative},
}

var units = []struct {
	name string
	u    pego.Unit
}{
	{"codepoints", pego.CodePoints},
	{"bytes", pego.Bytes},
}

func load(tb testing.TB, path string) *pego.Parser {
	tb.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

func parseOK(p *pego.Parser, w *workload, input string, opts ...pego.ParseOption) error {
	n, err := p.Parse(input, opts...)
	if w.partial && n != nil {
		return nil
	}
	return err
}

// TestWorkloads checks that every input parses on every backend and that the PEGO backends agree.
func TestWorkloads(t *testing.T) {
	for i := range workloads {
		w := &workloads[i]
		t.Run(w.name, func(t *testing.T) {
			p := load(t, w.grammar)
			input := w.input(10)
			var want string
			for _, be := range backends {
				for _, u := range units {
					n, err := p.Parse(input, pego.WithBackend(be.b), pego.WithUnit(u.u))
					if err != nil && !(w.partial && n != nil) {
						t.Fatalf("%s/%s: %v", be.name, u.name, err)
					}
					if u.u == pego.Bytes {
						continue
					}
					got, _ := json.Marshal(n)
					if want == "" {
						want = string(got)
					} else if string(got) != want {
						t.Errorf("%s: result differs from closure", be.name)
					}
				}
			}
			for _, b := range []bool{false, true} {
				if err := w.gen(input, b); err != nil {
					t.Errorf("generated (bytes %v): %v", b, err)
				}
			}
			if w.std != nil {
				if err := w.std(input); err != nil {
					t.Errorf("%s: %v", w.stdName, err)
				}
			}
			if w.partial {
				if _, err := p.Parse(input); err == nil {
					t.Error("expected recovered errors")
				}
			}
		})
	}
}

// BenchmarkParse parses each workload with every PEGO backend (closure, recursive and iterative bytecode,
// generated Go) in both position units, and with the standard library parser where one exists.
func BenchmarkParse(b *testing.B) {
	for i := range workloads {
		w := &workloads[i]
		b.Run(w.name, func(b *testing.B) {
			p := load(b, w.grammar)
			input := w.input(1)
			for _, be := range backends {
				for _, u := range units {
					b.Run(be.name+"/"+u.name, func(b *testing.B) {
						b.SetBytes(int64(len(input)))
						b.ReportAllocs()
						for b.Loop() {
							if err := parseOK(p, w, input, pego.WithBackend(be.b), pego.WithUnit(u.u)); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
			for _, u := range units {
				b.Run("generated/"+u.name, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						if err := w.gen(input, u.u == pego.Bytes); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
			if w.std != nil {
				b.Run(w.stdName, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						if err := w.std(input); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// BenchmarkRecognize measures recognition without a tree (RecognizeOnly) and compares it with the standard
// library validator that also builds nothing (json.Valid) where one exists.
func BenchmarkRecognize(b *testing.B) {
	for i := range workloads {
		w := &workloads[i]
		b.Run(w.name, func(b *testing.B) {
			p := load(b, w.grammar)
			input := w.input(1)
			for _, be := range backends {
				b.Run(be.name, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						if _, err := p.Parse(input, pego.WithBackend(be.b), pego.RecognizeOnly()); err != nil && !w.partial {
							b.Fatal(err)
						}
					}
				})
			}
			b.Run("generated", func(b *testing.B) {
				b.SetBytes(int64(len(input)))
				b.ReportAllocs()
				for b.Loop() {
					if err := w.rec(input); err != nil && !w.partial {
						b.Fatal(err)
					}
				}
			})
			if w.name == "JSON" {
				b.Run("json_Valid", func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					data := []byte(input)
					for b.Loop() {
						if !json.Valid(data) {
							b.Fatal("invalid")
						}
					}
				})
			}
		})
	}
}

// BenchmarkIncremental repeats a one-character edit to a large program followed by a parse, with a
// Document (incremental) and with a fresh parse each time.
func BenchmarkIncremental(b *testing.B) {
	p := load(b, "../examples/minilang/minilang.pego")
	input := MinilangInput(300, 0)
	// Document positions are in code points.
	at := utf8.RuneCountInString(input[:strings.Index(input, "fn f150(")+len("fn f150(")])
	for _, be := range backends {
		b.Run(be.name+"/document", func(b *testing.B) {
			doc, err := p.NewDocument(input, pego.WithBackend(be.b))
			if err != nil {
				b.Fatal(err)
			}
			if _, err := doc.Parse(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				// Add a character to a parameter name, then remove it, keeping the text size stable.
				if i%2 == 0 {
					doc.Edit(at, at, "z")
				} else {
					doc.Edit(at, at+1, "")
				}
				i++
				if _, err := doc.Parse(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(be.name+"/fresh", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := p.Parse(input, pego.WithBackend(be.b)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStream parses CSV as a stream, receiving one record at a time.
func BenchmarkStream(b *testing.B) {
	p := load(b, "../examples/csv/csv.pego")
	input := CSVInput(50000)
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				n := 0
				err := p.ParseStream(strings.NewReader(input), func(*pego.Node) error { n++; return nil }, pego.WithBackend(be.b))
				if err != nil || n != 50001 {
					b.Fatalf("%d records: %v", n, err)
				}
			}
		})
	}
	b.Run("encoding_csv", func(b *testing.B) {
		b.SetBytes(int64(len(input)))
		b.ReportAllocs()
		for b.Loop() {
			r := csv.NewReader(strings.NewReader(input))
			r.FieldsPerRecord = -1
			n := 0
			for {
				_, err := r.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					b.Fatal(err)
				}
				n++
			}
		}
	})
}

// BenchmarkPrepare compares compiling a grammar from source with loading a saved grammar (with and without
// the AST), including backend setup on the first parse after loading.
func BenchmarkPrepare(b *testing.B) {
	for _, path := range []string{"../examples/json/json.pego", "../examples/minilang/minilang.pego"} {
		name := strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".pego")
		src, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		p := load(b, path)
		full, _ := p.MarshalBinary()
		bare, _ := p.Marshal(pego.WithoutAST())
		b.Run(name+"/source", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := pego.CompileSource(string(src), "main"); err != nil {
					b.Fatal(err)
				}
			}
		})
		for _, c := range []struct {
			name string
			data []byte
			b    pego.Backend
		}{{"pegoc/closure", full, pego.Closure}, {"pegoc/bytecode", full, pego.Bytecode}, {"pegoc-noast/bytecode", bare, pego.Bytecode}} {
			b.Run(name+"/"+c.name, func(b *testing.B) {
				b.ReportMetric(float64(len(c.data)), "file-bytes")
				b.ReportAllocs()
				for b.Loop() {
					lp, err := pego.LoadParser(c.data)
					if err != nil {
						b.Fatal(err)
					}
					lp.Parse("", pego.WithBackend(c.b)) // set up the backend (the parse may fail)
				}
			})
		}
	}
}
