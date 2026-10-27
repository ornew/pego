package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"os"
	"regexp"
	"strings"
)

// The oracle compares, function by function, the ordinary form that the paired fragments select
// with the one that specializeOrdinaryMethods makes. It is a development aid for moving the ordinary
// form into the printer: PEGO_ORACLE=1 prints the functions that differ to standard error.
var (
	oracleHook func(pkg, paired string)
	oracleOld  func(fn string, text []byte)
)

func init() {
	if os.Getenv("PEGO_ORACLE") == "" {
		return
	}
	old := map[string][]byte{}
	oracleOld = func(fn string, text []byte) { old[fn] = text }
	digits := regexp.MustCompile(`[0-9]+`)
	oracleHook = func(pkg, paired string) {
		defer func() { old = map[string][]byte{} }()
		src, err := format.Source([]byte("package " + pkg + "\n" + selectForm(paired, formOrdinaryText)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ORACLE ordinary text does not format: %v\n", err)
			return
		}
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, "o.go", src, goparser.SkipObjectResolution)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ORACLE %v\n", err)
			return
		}
		same, differ, layout := 0, 0, 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*goast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			want, ok := old[fn.Name.Name]
			if !ok {
				continue
			}
			var b bytes.Buffer
			if err := format.Node(&b, fset, fn.Body); err != nil {
				continue
			}
			got := b.Bytes()
			if !multilineLiteral(fn.Body) {
				got = bytes.ReplaceAll(got, []byte("\n\n"), []byte("\n"))
				got = bytes.ReplaceAll(got, []byte("\n\n"), []byte("\n"))
			}
			if bytes.Equal(got, want) {
				same++
				continue
			}
			if oracleTokens(got) == oracleTokens(want) {
				layout++
				continue
			}
			differ++
			gl, wl := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
			i := 0
			for i < len(gl) && i < len(wl) && gl[i] == wl[i] {
				i++
			}
			line := func(l []string) string {
				if i < len(l) {
					return strings.TrimSpace(l[i])
				}
				return "<end>"
			}
			fmt.Fprintf(os.Stderr, "ORACLE DIFF %s\n  new: %s\n  old: %s\n", fn.Name.Name, digits.ReplaceAllString(line(gl), "N"), digits.ReplaceAllString(line(wl), "N"))
		}
		fmt.Fprintf(os.Stderr, "ORACLE %s same=%d layout=%d differ=%d\n", pkg, same, layout, differ)
	}
}

// oracleTokens is the token stream of a function body, which does not depend on how gofmt broke its lines.
func oracleTokens(src []byte) string {
	var sc scanner.Scanner
	fset := token.NewFileSet()
	sc.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var b strings.Builder
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return b.String()
		}
		if tok == token.SEMICOLON {
			continue
		}
		b.WriteString(tok.String() + lit + " ")
	}
}
