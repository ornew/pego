package golang_test

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// TestNativeDirectiveNumbers compares both integer-width boundaries with the
// executing toolchain, including the filename/line/column ambiguity when an
// unsigned field overflows strconv.ParseUint(..., 0).
func TestNativeDirectiveNumbers(t *testing.T) {
	numbers := []string{"0", "00", "000", "1", "1073741824", "1073741825", "2147483647", "2147483648",
		"4294967295", "4294967296", "9223372036854775807", "9223372036854775808",
		"18446744073709551615", "18446744073709551616", "0002147483648", "0004294967296"}
	forms := []string{
		"//line é.go:%s\npackage p\nvar x int\n",
		"//line é.go:%s:1\r\npackage p\nvar x int\n",
		"//line é.go:1:%s\npackage p\nvar x int\n",
		"//line é.go:%s:4294967295\npackage p\nvar x int\n",
		"//line é.go:%s:18446744073709551616\npackage p\nvar x int\n",
		"/*line é.go:%s*/ package p; var x int",
		"/*line é.go:%s:1*/ package p; var x int",
		"/*line é.go:1:%s*/ package p; var x int",
		"package p; /*line é.go:%s*/ var x int",
		"package p; /*line é.go:%s:1*/ var x int",
		"package p; /*line é.go:1:%s*/ var x int",
	}
	for _, num := range numbers {
		for _, form := range forms {
			src := fmt.Sprintf(form, num)
			_, referenceErr := goparser.ParseFile(token.NewFileSet(), "x.go", src, goparser.SkipObjectResolution)
			for _, unit := range []golang.Unit{golang.CodePoints, golang.Bytes} {
				_, astErr := golang.ParseAST(src, unit)
				_, nodeErr := golang.Parse(src, unit)
				recognizeErr := golang.Recognize(src, unit)
				for name, err := range map[string]error{"AST": astErr, "Node": nodeErr, "recognize": recognizeErr} {
					if (err == nil) != (referenceErr == nil) {
						t.Errorf("%q %s/%v: error = %v; go/parser = %v", src, name, unit, err, referenceErr)
					}
				}
			}
			for _, mode := range []golang.Mode{0, golang.ParseComments} {
				if d := compare("x.go", []byte(src), mode); d != "" {
					t.Errorf("%q mode %v: %s", src, mode, d)
				}
			}
		}
	}
}
