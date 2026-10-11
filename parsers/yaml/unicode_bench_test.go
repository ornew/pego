package yaml_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

func TestUnicodeScalarContent(t *testing.T) {
	for _, r := range []rune{0xd7ff, 0xe000, 0xfdd0, 0xfffd, 0x10000, 0x10ffff} {
		input := "key: " + string(r) + "\n"
		for _, unit := range []yaml.Unit{yaml.CodePoints, yaml.Bytes} {
			if _, err := yaml.Parse(input, yaml.WithUnit(unit)); err != nil {
				t.Fatalf("Parse %U unit %v: %v", r, unit, err)
			}
			if _, err := yaml.ParseAST(input, yaml.WithUnit(unit)); err != nil {
				t.Fatalf("ParseAST %U unit %v: %v", r, unit, err)
			}
			if err := yaml.Recognize(input, yaml.WithUnit(unit)); err != nil {
				t.Fatalf("Recognize %U unit %v: %v", r, unit, err)
			}
		}
	}
	for _, r := range []rune{0xfffe, 0xffff} {
		if err := yaml.Recognize("key: " + string(r) + "\n"); err == nil {
			t.Fatalf("accepted excluded YAML character %U", r)
		}
	}
}

// BenchmarkUnicodeClasses measures the grammar migration's generated runtime
// paths on 500 mapping entries in a sequence, with ASCII and Unicode text.
func BenchmarkUnicodeClasses(b *testing.B) {
	for _, tc := range []struct{ name, text string }{{"ASCII", "value"}, {"Unicode", "日本語😀"}} {
		input := strings.Repeat("- name: "+tc.text+" # comment\n  text: "+tc.text+"\n", 500)
		for _, unit := range []yaml.Unit{yaml.CodePoints, yaml.Bytes} {
			uname := "CodePoints"
			if unit == yaml.Bytes {
				uname = "Bytes"
			}
			for _, api := range []string{"Parse", "ParseAST", "Recognize"} {
				b.Run(tc.name+"/"+uname+"/"+api, func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(len(input)))
					for b.Loop() {
						var err error
						switch api {
						case "Parse":
							_, err = yaml.Parse(input, yaml.WithUnit(unit))
						case "ParseAST":
							_, err = yaml.ParseAST(input, yaml.WithUnit(unit))
						case "Recognize":
							err = yaml.Recognize(input, yaml.WithUnit(unit))
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
