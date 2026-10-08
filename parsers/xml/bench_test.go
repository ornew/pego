package xml_test

import (
	stdxml "encoding/xml"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/xml"
)

// benchInput is a catalog of about 256 KB, like the XML workload of the benchmarks of PEGO (bench/ in
// github.com/ornew/pego): elements, attributes, text with references, comments and CDATA sections.
var benchInput = func() string {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	r := rand.New(rand.NewPCG(1, 2))
	w := func() string { return words[r.IntN(len(words))] }
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?>\n<catalog>\n")
	for i := 0; b.Len() < 256<<10; i++ {
		fmt.Fprintf(&b, "  <book id=\"b%d\" lang='%s'>\n", i, w())
		fmt.Fprintf(&b, "    <title>%s &amp; %s</title>\n", w(), w())
		fmt.Fprintf(&b, "    <price currency=\"JPY\">%d</price>\n", r.IntN(10000))
		b.WriteString("    <authors>\n")
		for k := r.IntN(3) + 1; k > 0; k-- {
			fmt.Fprintf(&b, "      <author><name>%s</name><role>%s</role></author>\n", w(), w())
		}
		b.WriteString("    </authors>\n")
		switch r.IntN(3) {
		case 0:
			b.WriteString("    <!-- reviewed -->\n")
		case 1:
			b.WriteString("    <note><![CDATA[<raw> & text]]></note>\n")
		}
		b.WriteString("    <empty/>\n  </book>\n")
	}
	b.WriteString("</catalog>\n")
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := xml.ParseAST(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := xml.Decode(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecognize(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if err := xml.Recognize(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := xml.Parse(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

// The standard library, for comparison: Decoder.Token over the whole document (which checks the syntax
// and decodes names, attributes and text, but builds no tree).

func BenchmarkEncodingXMLToken(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		d := stdxml.NewDecoder(strings.NewReader(benchInput))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}
	}
}
