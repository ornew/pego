package xml_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ornew/pego/parsers/xml"
)

func TestLongEncodingDeclarations(t *testing.T) {
	encode16 := func(s string, little, bom bool) []byte {
		var data []byte
		if bom {
			if little {
				data = append(data, 0xff, 0xfe)
			} else {
				data = append(data, 0xfe, 0xff)
			}
		}
		for _, u := range utf16.Encode([]rune(s)) {
			if little {
				data = append(data, byte(u), byte(u>>8))
			} else {
				data = append(data, byte(u>>8), byte(u))
			}
		}
		return data
	}
	for _, padding := range []int{1, 240, 8192} {
		t.Run(fmt.Sprint(padding), func(t *testing.T) {
			decl := func(enc string) string {
				return `<?xml version="1.0"` + strings.Repeat(" \t\r\n", padding) + `encoding="` + enc + `"?>`
			}
			for _, tc := range []struct {
				name string
				data []byte
				want string
				text string
				err  string
			}{
				{"UTF-8", []byte(decl("UTF-8") + "<a>é😀</a>"), decl("UTF-8") + "<a>é😀</a>", "é😀", ""},
				{"UTF-8 BOM", []byte("\ufeff" + decl("utf8") + "<a>é</a>"), decl("utf8") + "<a>é</a>", "é", ""},
				{"Latin-1", []byte(decl("ISO-8859-1") + "<a>\xe9</a>"), decl("ISO-8859-1") + "<a>é</a>", "é", ""},
				{"ASCII", []byte(decl("US-ASCII") + "<a>x</a>"), decl("US-ASCII") + "<a>x</a>", "x", ""},
				{"not ASCII", []byte(decl("US-ASCII") + "<a>é</a>"), "", "", "not US-ASCII"},
				{"unsupported", []byte(decl("NO-SUCH-ENCODING") + "<a>é</a>"), "", "", "unsupported encoding: NO-SUCH-ENCODING"},
				{"Latin-1 BOM mismatch", []byte("\ufeff" + decl("ISO-8859-1") + "<a/>"), "", "", "byte order mark"},
				{"ASCII BOM mismatch", []byte("\ufeff" + decl("US-ASCII") + "<a/>"), "", "", "byte order mark"},
				{"UTF-16 declared in UTF-8", []byte(decl("UTF-16") + "<a/>"), "", "", "not in UTF-16"},
				{"UTF-16BE", encode16(decl("UTF-16BE")+"<a>é😀</a>", false, false), decl("UTF-16BE") + "<a>é😀</a>", "é😀", ""},
				{"UTF-16LE", encode16(decl("UTF-16LE")+"<a>é😀</a>", true, false), decl("UTF-16LE") + "<a>é😀</a>", "é😀", ""},
				{"UTF-16 BE BOM", encode16(decl("UTF-16")+"<a>é</a>", false, true), decl("UTF-16") + "<a>é</a>", "é", ""},
				{"UTF-16 LE BOM", encode16(decl("UTF-16")+"<a>é</a>", true, true), decl("UTF-16") + "<a>é</a>", "é", ""},
				{"BE declares LE", encode16(decl("UTF-16LE")+"<a/>", false, false), "", "", "but the document is in UTF-16BE"},
				{"LE BOM declares BE", encode16(decl("UTF-16BE")+"<a/>", true, true), "", "", "but the document is in UTF-16LE"},
				{"UTF-16 declares UTF-8", encode16(decl("UTF-8")+"<a/>", false, true), "", "", "but the document is in UTF-16"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got, err := xml.Transcode(tc.data)
					if tc.err != "" {
						if err == nil || !strings.Contains(err.Error(), tc.err) {
							t.Fatalf("Transcode error = %v; want %q", err, tc.err)
						}
					} else if err != nil || got != tc.want {
						t.Fatalf("Transcode = %q, %v; want %q", got, err, tc.want)
					}
					tree, err := xml.DecodeBytes(tc.data)
					if tc.err != "" {
						if err == nil || !strings.Contains(err.Error(), tc.err) {
							t.Fatalf("DecodeBytes error = %v; want %q", err, tc.err)
						}
					} else if err != nil || tree.Root.Text() != tc.text {
						t.Fatalf("DecodeBytes = %#v, %v; want text %q", tree, err, tc.text)
					}
				})
			}
		})
	}
}

func TestEncodingDeclarationBoundaries(t *testing.T) {
	for _, input := range []string{
		`<?xml version='1.0'?><a encoding='NO-SUCH-ENCODING'>é</a>`,
		`<?xml-stylesheet encoding='NO-SUCH-ENCODING'?><a>é</a>`,
		"<?xml\tversion \r=\n'1.0'\tencoding \n=\r'UTF-8'?><a>é</a>",
	} {
		if got, err := xml.Transcode([]byte(input)); err != nil || got != input {
			t.Fatalf("Transcode = %q, %v; want %q", got, err, input)
		}
		if _, err := xml.DecodeBytes([]byte(input)); err != nil {
			t.Fatal(err)
		}
	}
	// Transcode detects encoding but leaves malformed XML to DecodeBytes.
	for _, input := range []string{"<?xml", `<?xml version='1.0' encoding='UTF-8'`} {
		if got, err := xml.Transcode([]byte(input)); err != nil || got != input {
			t.Fatalf("Transcode incomplete = %q, %v", got, err)
		}
		if _, err := xml.DecodeBytes([]byte(input)); err == nil {
			t.Fatal("DecodeBytes accepted an incomplete declaration")
		}
	}
	if _, err := xml.Transcode([]byte(`<?xml version='1.0' encoding='Shift_JIS'`)); err == nil || !strings.Contains(err.Error(), "unsupported encoding") {
		t.Fatalf("incomplete unsupported declaration: %v", err)
	}
}

// Only UTF-8 inputs are compared, so parent and candidate return identical text.
func BenchmarkTranscodeDeclarations(b *testing.B) {
	for _, tc := range []struct{ name, input string }{
		{"short", `<?xml version='1.0' encoding='UTF-8'?><a>é</a>`},
		{"long", `<?xml version='1.0'` + strings.Repeat(" ", 1024) + `encoding='UTF-8'?><a>é</a>`},
		{"no encoding catalog", benchInput},
	} {
		b.Run(tc.name, func(b *testing.B) {
			data := []byte(tc.input)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := xml.Transcode(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeBytes(b *testing.B) {
	data := []byte(benchInput)
	// The existing Decode benchmark starts from a string and does not transcode.
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := xml.DecodeBytes(data); err != nil {
			b.Fatal(err)
		}
	}
}
