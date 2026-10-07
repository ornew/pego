package bench

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Inputs are generated from a fixed random seed so that results are reproducible.
func newRand() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

var words = []string{"alpha", "beta", "gamma", "delta", "日本語", "naïve", "x", "value", "item", "東京"}

// JSONInput returns about size bytes of JSON: an array of objects.
func JSONInput(size int) string {
	r := newRand()
	var b strings.Builder
	b.WriteString("[\n")
	for i := 0; b.Len() < size; i++ {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `  {"id": %d, "name": "%s \"%s\"", "score": %d.%02d, "active": %t, "tags": [`,
			i, words[r.IntN(len(words))], words[r.IntN(len(words))], r.IntN(1000), r.IntN(100), r.IntN(2) == 0)
		for k := r.IntN(4); k > 0; k-- {
			fmt.Fprintf(&b, `"%s"`, words[r.IntN(len(words))])
			if k > 1 {
				b.WriteString(", ")
			}
		}
		fmt.Fprintf(&b, `], "parent": null, "pos": {"x": %d, "y": -%d.5e3}}`, r.IntN(100), r.IntN(100))
	}
	b.WriteString("\n]\n")
	return b.String()
}

// CSVInput returns CSV with rows records, including quoted fields with commas, newlines and quotes.
func CSVInput(rows int) string {
	r := newRand()
	var b strings.Builder
	b.WriteString("id,name,city,amount,note,flag\n")
	for i := 0; i < rows; i++ {
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
}

// XMLInput returns about size bytes of XML with nesting, attributes, references, comments and CDATA.
func XMLInput(size int) string {
	r := newRand()
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?>\n<catalog>\n")
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "  <book id=\"b%d\" lang='%s'>\n", i, words[r.IntN(len(words))])
		fmt.Fprintf(&b, "    <title>%s &amp; %s</title>\n", words[r.IntN(len(words))], words[r.IntN(len(words))])
		fmt.Fprintf(&b, "    <price currency=\"JPY\">%d</price>\n", r.IntN(10000))
		b.WriteString("    <authors>\n")
		for k := r.IntN(3) + 1; k > 0; k-- {
			fmt.Fprintf(&b, "      <author><name>%s</name><role>%s</role></author>\n", words[r.IntN(len(words))], words[r.IntN(len(words))])
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
}

// ArithInput returns an arithmetic expression with terms terms, nested parentheses and unary minus.
// It uses only operators that also form a valid Go expression.
func ArithInput(terms int) string {
	r := newRand()
	ops := []string{"+", "-", "*", "/", "%"}
	var b strings.Builder
	depth := 0
	for i := 0; i < terms; i++ {
		if i > 0 {
			b.WriteString(" " + ops[r.IntN(len(ops))] + " ")
		}
		for r.IntN(4) == 0 && depth < 40 {
			b.WriteString("(")
			depth++
		}
		if r.IntN(8) == 0 {
			b.WriteString("-")
		}
		fmt.Fprintf(&b, "%d", r.IntN(1000))
		for depth > 0 && r.IntN(3) == 0 {
			b.WriteString(")")
			depth--
		}
	}
	b.WriteString(strings.Repeat(")", depth))
	return b.String()
}

// MinilangInput returns a minilang program with funcs functions. If broken is positive, about one line in
// broken is corrupted to exercise error recovery.
func MinilangInput(funcs, broken int) string {
	r := newRand()
	var b strings.Builder
	line := 0
	stmt := func(indent, s string) {
		line++
		if broken > 0 && line%broken == 0 {
			s = strings.Replace(s, "=", "= ) $", 1)
		}
		b.WriteString(indent + s + "\n")
	}
	for i := 0; i < funcs; i++ {
		fmt.Fprintf(&b, "// function %d\nfn f%d(a, b, c) {\n", i, i)
		stmt("    ", fmt.Sprintf("let x = a * %d + b[%d] - c.size;", r.IntN(100), r.IntN(10)))
		stmt("    ", fmt.Sprintf("let s = \"%s\";", words[r.IntN(len(words))]))
		b.WriteString("    while x < 100 && !done(x) {\n")
		stmt("        ", fmt.Sprintf("x = x + f%d(x, [1, 2, x %% 3], s);", r.IntN(i+1)))
		b.WriteString("        if x == 42 {\n")
		stmt("            ", "return x > 10 ? -x : x;")
		b.WriteString("        } else {\n")
		stmt("            ", "print(s, x);")
		b.WriteString("        }\n    }\n")
		stmt("    ", "return x;")
		b.WriteString("}\n")
	}
	return b.String()
}

// OutlineInput returns an outline with lines lines, nested at most 10 levels deep.
func OutlineInput(lines int) string {
	r := newRand()
	var b strings.Builder
	depth := 0
	for i := 0; i < lines; i++ {
		switch {
		case r.IntN(3) == 0 && depth < 10:
			depth++
		case r.IntN(3) == 0:
			depth = r.IntN(depth + 1)
		}
		if i == 0 {
			depth = 0
		}
		fmt.Fprintf(&b, "%s%s %d\n", strings.Repeat("  ", depth), words[r.IntN(len(words))], i)
	}
	return b.String()
}
