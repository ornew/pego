package bench

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/ornew/pego/internal/bench/inputs"
	"go/parser"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego"
	gcalc "github.com/ornew/pego/internal/bench/gen/calc"
	gcalclr "github.com/ornew/pego/internal/bench/gen/calclr"
	gcsv "github.com/ornew/pego/internal/bench/gen/csv"
	gcsvdoc "github.com/ornew/pego/internal/bench/gen/csvdoc"
	gcsvstream "github.com/ornew/pego/internal/bench/gen/csvstream"
	gjson "github.com/ornew/pego/internal/bench/gen/json"
	gminilang "github.com/ornew/pego/internal/bench/gen/minilang"
	gminilangdoc "github.com/ornew/pego/internal/bench/gen/minilangdoc"
	goutline "github.com/ornew/pego/internal/bench/gen/outline"
	gxml "github.com/ornew/pego/internal/bench/gen/xml"
	rt "github.com/ornew/pego/runtime"
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
	// ast parses into typed values with the generated parser's ParseAST (nil if not generated).
	ast func(input string) error
	// std is the standard library parser to compare with (nil if there is none).
	std     func(input string) error
	stdName string
	// partial treats a parse that recovered from errors as successful when it returns a tree.
	partial bool
}

func genErr[N any](_ N, err error) error { return err }

var workloads = []workload{
	{
		name: "JSON", grammar: "../parsers/json/json.pego", input: func(k int) string { return inputs.JSONInput(256 << 10 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gjson.Parse(s, gjson.WithUnit(gjson.Bytes)))
			}
			return genErr(gjson.Parse(s))
		},
		rec:     func(s string) error { return gjson.Recognize(s) },
		ast:     func(s string) error { return genErr(gjson.ParseAST(s)) },
		stdName: "encoding_json", std: func(s string) error {
			var v any
			return json.Unmarshal([]byte(s), &v)
		},
	},
	{
		name: "CSV", grammar: "../parsers/csv/csv.pego", input: func(k int) string { return inputs.CSVInput(5000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gcsv.Parse(s, gcsv.WithUnit(gcsv.Bytes)))
			}
			return genErr(gcsv.Parse(s))
		},
		rec:     func(s string) error { return gcsv.Recognize(s) },
		ast:     func(s string) error { return genErr(gcsv.ParseAST(s)) },
		stdName: "encoding_csv", std: func(s string) error {
			r := csv.NewReader(strings.NewReader(s))
			r.FieldsPerRecord = -1
			_, err := r.ReadAll()
			return err
		},
	},
	{
		name: "XML", grammar: "../parsers/xml/xml.pego", input: func(k int) string { return inputs.XMLInput(256 << 10 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gxml.Parse(s, gxml.WithUnit(gxml.Bytes)))
			}
			return genErr(gxml.Parse(s))
		},
		rec:     func(s string) error { return gxml.Recognize(s) },
		ast:     func(s string) error { return genErr(gxml.ParseAST(s)) },
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
		name: "Arith_OperatorsRule", grammar: "../examples/calculator/calc.pego", input: func(k int) string { return inputs.ArithInput(20000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gcalc.Parse(s, gcalc.WithUnit(gcalc.Bytes)))
			}
			return genErr(gcalc.Parse(s))
		},
		rec:     func(s string) error { return gcalc.Recognize(s) },
		ast:     func(s string) error { return genErr(gcalc.ParseAST(s)) },
		stdName: "go_parser", std: func(s string) error { return genErr(parser.ParseExpr(s)) },
	},
	{
		name: "Arith_LeftRec", grammar: "../examples/calculator/calc_lr.pego", input: func(k int) string { return inputs.ArithInput(20000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gcalclr.Parse(s, gcalclr.WithUnit(gcalclr.Bytes)))
			}
			return genErr(gcalclr.Parse(s))
		},
		rec:     func(s string) error { return gcalclr.Recognize(s) },
		ast:     func(s string) error { return genErr(gcalclr.ParseAST(s)) },
		stdName: "go_parser", std: func(s string) error { return genErr(parser.ParseExpr(s)) },
	},
	{
		name: "Minilang", grammar: "../examples/minilang/minilang.pego", input: func(k int) string { return inputs.MinilangInput(300/k, 0) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(gminilang.Parse(s, gminilang.WithUnit(gminilang.Bytes)))
			}
			return genErr(gminilang.Parse(s))
		},
		rec: func(s string) error { return gminilang.Recognize(s) },
		ast: func(s string) error { return genErr(gminilang.ParseAST(s)) },
	},
	{
		name: "Recovery", grammar: "../examples/minilang/minilang.pego", input: func(k int) string { return inputs.MinilangInput(300/k, 7) },
		partial: true,
		gen: func(s string, b bool) error {
			u := gminilang.CodePoints
			if b {
				u = gminilang.Bytes
			}
			if n, _ := gminilang.Parse(s, gminilang.WithUnit(u)); n == nil {
				return io.ErrUnexpectedEOF
			}
			return nil
		},
		rec: func(s string) error { return gminilang.Recognize(s) },
		ast: func(s string) error {
			if v, _ := gminilang.ParseAST(s); v == nil {
				return io.ErrUnexpectedEOF
			}
			return nil
		},
	},
	{
		name: "Outline", grammar: "../examples/outline/outline.pego", input: func(k int) string { return inputs.OutlineInput(5000 / k) },
		gen: func(s string, b bool) error {
			if b {
				return genErr(goutline.Parse(s, goutline.WithUnit(goutline.Bytes)))
			}
			return genErr(goutline.Parse(s))
		},
		rec: func(s string) error { return goutline.Recognize(s) },
		ast: func(s string) error { return genErr(goutline.ParseAST(s)) },
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
	u    pego.PositionUnit
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
			if w.ast != nil {
				if err := w.ast(input); err != nil {
					t.Errorf("generated ParseAST: %v", err)
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

// TestGeneratedDocumentAndStream checks, on reduced inputs, that the generated Document parsers of
// BenchmarkIncremental and BenchmarkIncrementalLong return the tree of a fresh parse after each edit,
// and that the generated stream parser of BenchmarkStream emits the records of the engine's stream.
func TestGeneratedDocumentAndStream(t *testing.T) {
	type document interface {
		Edit(start, end int, text string) error
		Parse(...rt.ParseOption) (*rt.Node, error)
	}
	check := func(t *testing.T, input, marker string, newDoc func(string) (document, error), parse func(string, ...rt.ParseOption) (*rt.Node, error)) {
		at := utf8.RuneCountInString(input[:strings.Index(input, marker)+len(marker)])
		doc, err := newDoc(input)
		if err != nil {
			t.Fatal(err)
		}
		text := []rune(input)
		same := func(step string) {
			t.Helper()
			got, err := doc.Parse()
			if err != nil {
				t.Fatalf("%s: %v", step, err)
			}
			want, err := parse(string(text))
			if err != nil {
				t.Fatalf("%s: fresh parse: %v", step, err)
			}
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			if string(g) != string(w) {
				t.Errorf("%s: Document tree differs from a fresh parse", step)
			}
		}
		same("initial parse")
		for i, e := range []struct {
			start, end int
			text       string
		}{{at, at, "z"}, {at, at + 1, ""}, {at, at, "zz"}} {
			if err := doc.Edit(e.start, e.end, e.text); err != nil {
				t.Fatal(err)
			}
			text = append(text[:e.start:e.start], append([]rune(e.text), text[e.end:]...)...)
			same(fmt.Sprintf("edit %d", i))
		}
	}
	t.Run("Incremental", func(t *testing.T) {
		check(t, inputs.MinilangInput(30, 0), "fn f15(",
			func(s string) (document, error) { return gminilangdoc.NewDocument(s) }, gminilang.Parse)
	})
	t.Run("IncrementalLong", func(t *testing.T) {
		check(t, inputs.CSVInput(500), "\n250,",
			func(s string) (document, error) { return gcsvdoc.NewDocument(s) }, gcsv.Parse)
	})
	t.Run("Stream", func(t *testing.T) {
		input := inputs.CSVInput(500)
		p := load(t, "../parsers/csv/csv.pego")
		var want, got []string
		if err := p.ParseStream(strings.NewReader(input), func(n *pego.Node) error {
			b, err := json.Marshal(n)
			want = append(want, string(b))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := gcsvstream.ParseStream(strings.NewReader(input), func(n *gcsvstream.Node) error {
			b, err := json.Marshal(n)
			got = append(got, string(b))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(got) != 501 || !slices.Equal(got, want) {
			t.Errorf("generated stream emits %d records, engine %d; equal %v", len(got), len(want), slices.Equal(got, want))
		}
	})
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
			if w.ast != nil {
				b.Run("generated_ast", func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						if err := w.ast(input); err != nil {
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

// documentEdits repeats a one-character edit at a followed by a parse: it adds a character, then
// removes it, keeping the text size stable.
func documentEdits(b *testing.B, at int, edit func(start, end int, text string) error, parse func() error) {
	b.Helper()
	if err := parse(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		var err error
		if i%2 == 0 {
			err = edit(at, at, "z")
		} else {
			err = edit(at, at+1, "")
		}
		i++
		if err != nil {
			b.Fatal(err)
		}
		if err := parse(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIncremental repeats a one-character edit to a large program followed by a parse, with a
// Document (incremental) and with a fresh parse each time, on every backend and with the generated
// parser (generated with -document; its fresh parse is the same package's Parse).
func BenchmarkIncremental(b *testing.B) {
	p := load(b, "../examples/minilang/minilang.pego")
	input := inputs.MinilangInput(300, 0)
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
	b.Run("generated/document", func(b *testing.B) {
		doc, err := gminilangdoc.NewDocument(input)
		if err != nil {
			b.Fatal(err)
		}
		documentEdits(b, at, doc.Edit, func() error { return genErr(doc.Parse()) })
	})
	b.Run("generated/fresh", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := gminilangdoc.Parse(input); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkIncrementalLong repeats a one-character edit in the middle of a long CSV file (50000
// records) followed by a parse with a Document, on every backend and with the generated parser
// (generated with -document).
func BenchmarkIncrementalLong(b *testing.B) {
	p := load(b, "../parsers/csv/csv.pego")
	input := inputs.CSVInput(50000)
	at := utf8.RuneCountInString(input[:strings.Index(input, "\n25000,")+len("\n25000,")])
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
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
	}
	b.Run("generated", func(b *testing.B) {
		doc, err := gcsvdoc.NewDocument(input)
		if err != nil {
			b.Fatal(err)
		}
		documentEdits(b, at, doc.Edit, func() error { return genErr(doc.Parse()) })
	})
}

// BenchmarkStream parses CSV as a stream, receiving one record at a time, on every backend, with the
// generated parser (generated with -stream) and with encoding/csv.
func BenchmarkStream(b *testing.B) {
	p := load(b, "../parsers/csv/csv.pego")
	input := inputs.CSVInput(50000)
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
	b.Run("generated", func(b *testing.B) {
		b.SetBytes(int64(len(input)))
		b.ReportAllocs()
		for b.Loop() {
			n := 0
			err := gcsvstream.ParseStream(strings.NewReader(input), func(*gcsvstream.Node) error { n++; return nil })
			if err != nil || n != 50001 {
				b.Fatalf("%d records: %v", n, err)
			}
		}
	})
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

// BenchmarkPrepare compares compiling a grammar from source with loading a compiled grammar of each mode,
// including backend setup on the first parse after loading.
func BenchmarkPrepare(b *testing.B) {
	for _, path := range []string{"../parsers/json/json.pego", "../examples/minilang/minilang.pego"} {
		name := strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".pego")
		src, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		p := load(b, path)
		b.Run(name+"/source", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := pego.CompileSource(string(src), "main"); err != nil {
					b.Fatal(err)
				}
			}
		})
		for _, c := range []struct {
			mode pego.CompiledMode
			opts []pego.ParseOption // a parse the mode serves
		}{
			{pego.CompiledParse, nil},
			{pego.CompiledRecognize, []pego.ParseOption{pego.RecognizeOnly()}},
			{pego.CompiledDocument, []pego.ParseOption{pego.WithTrace(func(pego.TraceEvent) {})}},
		} {
			data, err := p.Marshal(c.mode)
			if err != nil {
				b.Fatal(err)
			}
			opts := append([]pego.ParseOption{pego.WithBackend(pego.Bytecode)}, c.opts...)
			b.Run(name+"/pegoc/"+c.mode.String()+"/bytecode", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					lp, err := pego.LoadParser(data)
					if err != nil {
						b.Fatal(err)
					}
					lp.Parse("", opts...) // set up the backend (the parse may fail)
				}
				b.ReportMetric(float64(len(data)), "file-bytes") // after the loop, which resets metrics
			})
		}
	}
}
