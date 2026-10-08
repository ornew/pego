package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"sort"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd/v3"
)

// literalInputs is the number of generated inputs of each family that writeLiterals reads literals from.
const literalInputs = 30000

// writeLiterals writes, compressed, the values that the reference implementation (cue/literal) decodes from the
// literals of the sources and of generated inputs, one line per distinct literal: its kind (S for a string or
// bytes literal, I for an integer, F for a float), the literal as a Go quoted string, and "ok" and the value as a Go
// quoted string (the text of a decimal number in positional notation) or "err". The fragments of an interpolation
// are not decoded. The generated inputs are those of the families (generate) numbered from the seed. A number
// with an exponent of more than 1,000 in magnitude is left out: the reference does not decode it (it ignores the
// errors of its decimal arithmetic).
func writeLiterals(path string, srcs []source, seed uint64) error {
	seen := map[string]string{}
	add := func(name string, src []byte) {
		f, err := parser.ParseFile(name, src)
		if err != nil || f == nil {
			return
		}
		frag := map[ast.Node]bool{}
		ast.Walk(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Interpolation:
				for _, e := range n.Elts {
					frag[e] = true
				}
			case *ast.BasicLit:
				if !frag[n] {
					if k, line, ok := decodeLit(n); ok {
						seen[k+"\t"+fmt.Sprintf("%q", n.Value)] = line
					}
				}
			}
			return true
		}, nil)
	}
	for _, s := range srcs {
		add(s.Name, s.Data)
	}
	for _, fam := range families {
		for i := range literalInputs {
			add(fam, []byte(generate(fam, seed+uint64(i))))
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	fmt.Fprintf(gz, "# count=%d seed=%d\n", literalInputs, seed)
	for _, k := range keys {
		fmt.Fprintf(gz, "%s\t%s\n", k, seen[k])
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// decodeLit decodes a basic literal, and returns its kind and the result as "ok\tvalue" or "err".
func decodeLit(n *ast.BasicLit) (kind, result string, ok bool) {
	switch n.Kind {
	case token.STRING:
		s, err := literal.Unquote(n.Value)
		if err != nil {
			return "S", "err", true
		}
		return "S", fmt.Sprintf("ok\t%q", s), true
	case token.INT, token.FLOAT:
		kind = "I"
		if n.Kind == token.FLOAT {
			kind = "F"
		}
		if hugeExponent(n.Value) {
			return "", "", false
		}
		var ni literal.NumInfo
		if err := literal.ParseNum(n.Value, &ni); err != nil {
			return kind, "err", true
		}
		var d apd.Decimal
		if err := ni.Decimal(&d); err != nil {
			return kind, "err", true
		}
		return kind, fmt.Sprintf("ok\t%q", strings.TrimSuffix(d.Text('f'), ".")), true
	}
	return "", "", false
}

// hugeExponent reports whether a number has an exponent of more than 1,000 in magnitude, which cue/literal does
// not decode (its decimal arithmetic fails, and the error is ignored).
func hugeExponent(lit string) bool {
	i := strings.IndexAny(lit, "eE")
	if i < 0 || strings.HasPrefix(lit, "0x") || strings.HasPrefix(lit, "0X") {
		return false
	}
	digits := strings.TrimLeft(strings.NewReplacer("_", "", "+", "", "-", "").Replace(lit[i+1:]), "0")
	return len(digits) > 4 || len(digits) == 4 && digits > "1000"
}
