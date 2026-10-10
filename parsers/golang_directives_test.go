package parsers_test

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/ornew/pego"
)

// TestGoDirectiveNumbers checks native-width numeric predicates in every
// engine backend against the executing Go standard parser.
func TestGoDirectiveNumbers(t *testing.T) {
	src, err := os.ReadFile("golang/golang.pego")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	numbers := []string{"0", "00", "000", "1073741824", "1073741825", "2147483647", "2147483648",
		"4294967295", "4294967296", "9223372036854775807", "9223372036854775808",
		"18446744073709551615", "18446744073709551616", "0004294967296"}
	forms := []string{"//line é.go:%s\npackage p", "//line é.go:%s:1\npackage p",
		"//line é.go:1:%s\npackage p", "//line é.go:%s:4294967295\npackage p",
		"//line é.go:%s:18446744073709551616\npackage p", "/*line é.go:%s*/ package p",
		"package p; /*line é.go:%s:1*/ var x int", "package p; /*line é.go:1:%s*/ var x int"}
	for _, num := range numbers {
		for _, form := range forms {
			input := fmt.Sprintf(form, num)
			_, referenceErr := goparser.ParseFile(token.NewFileSet(), "x.go", input, goparser.SkipObjectResolution)
			for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
				for _, u := range []pego.Unit{pego.CodePoints, pego.Bytes} {
					_, err := p.Parse(input, pego.WithBackend(b), pego.WithUnit(u))
					if (err == nil) != (referenceErr == nil) {
						t.Errorf("%q backend %v unit %v: error = %v; go/parser = %v", input, b, u, err, referenceErr)
					}
				}
			}
		}
	}
}
