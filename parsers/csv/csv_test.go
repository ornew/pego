package csv_test

import (
	stdcsv "encoding/csv"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/csv"
)

// bom is the UTF-8 encoding of the byte order mark U+FEFF.
const bom = "\xef\xbb\xbf"

// stdRecords reads input with encoding/csv configured as closely to this package as it can be:
// FieldsPerRecord -1 (records may differ in length), and a byte order mark at the start removed (which
// encoding/csv would keep in the first field).
func stdRecords(input string) ([][]string, error) {
	r := stdcsv.NewReader(strings.NewReader(strings.TrimPrefix(input, bom)))
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

// normalize turns each CRLF in the values into LF, as encoding/csv does inside quoted fields (an
// unquoted field cannot contain a CRLF).
func normalize(recs [][]string) [][]string {
	out := make([][]string, len(recs))
	for i, r := range recs {
		out[i] = make([]string, len(r))
		for j, v := range r {
			out[i][j] = strings.ReplaceAll(v, "\r\n", "\n")
		}
	}
	return out
}

// equal compares lists of records, treating nil and empty alike.
func equal(a, b [][]string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// compareStd checks that Records, Valid, Recognize and ParseAST agree with one another and with
// encoding/csv (stdRecords) on input: both accept it or both reject it, and the records are the same
// once CRLFs in values are normalized. It returns this package's records and error.
func compareStd(t *testing.T, input string) ([][]string, error) {
	t.Helper()
	got, err := csv.Records(input)
	valid := csv.Valid(input)
	_, perr := csv.ParseAST(input)
	if valid != (err == nil) || valid != (perr == nil) || valid != (csv.Recognize(input) == nil) {
		t.Fatalf("%q: Records: %v, Valid: %v, ParseAST: %v", input, err, valid, perr)
	}
	want, werr := stdRecords(input)
	if (err == nil) != (werr == nil) {
		t.Fatalf("%q: Records: %v, encoding/csv: %v", input, err, werr)
	}
	if err == nil && !equal(normalize(got), want) {
		t.Fatalf("%q: Records: %q, encoding/csv: %q", input, got, want)
	}
	return got, err
}

// edgeCases are the cases of RFC 4180 (section 2 and its grammar), the cases of csv-spectrum (a common
// collection of CSV test files, rewritten here from their descriptions) and the points where the RFC
// and practice differ. want is the result of Records (nil with err). Unless std or stdErr is set,
// encoding/csv (as configured by stdRecords) gives the same result.
var edgeCases = []struct {
	name   string
	in     string
	want   [][]string
	err    bool
	std    [][]string // encoding/csv's records, where they differ
	stdErr bool       // encoding/csv rejects the input that this package accepts
}{
	// RFC 4180, section 2.
	{name: "rfc/1 CRLF", in: "aaa,bbb,ccc\r\nzzz,yyy,xxx\r\n", want: [][]string{{"aaa", "bbb", "ccc"}, {"zzz", "yyy", "xxx"}}},
	{name: "rfc/2 no final line break", in: "aaa,bbb,ccc\r\nzzz,yyy,xxx", want: [][]string{{"aaa", "bbb", "ccc"}, {"zzz", "yyy", "xxx"}}},
	{name: "rfc/3 header", in: "field_name,field_name,field_name\r\naaa,bbb,ccc\r\nzzz,yyy,xxx\r\n",
		want: [][]string{{"field_name", "field_name", "field_name"}, {"aaa", "bbb", "ccc"}, {"zzz", "yyy", "xxx"}}},
	{name: "rfc/4 spaces are data", in: " aaa , bbb ,ccc \r\n", want: [][]string{{" aaa ", " bbb ", "ccc "}}},
	{name: "rfc/4 trailing comma is an empty field", in: "aaa,bbb,\r\n", want: [][]string{{"aaa", "bbb", ""}}},
	{name: "rfc/5 quoted", in: "\"aaa\",\"bbb\",\"ccc\"\r\nzzz,yyy,xxx", want: [][]string{{"aaa", "bbb", "ccc"}, {"zzz", "yyy", "xxx"}}},
	{name: "rfc/6 CRLF in quotes", in: "\"aaa\",\"b\r\nbb\",\"ccc\"\r\nzzz,yyy,xxx",
		want: [][]string{{"aaa", "b\r\nbb", "ccc"}, {"zzz", "yyy", "xxx"}},
		std:  [][]string{{"aaa", "b\nbb", "ccc"}, {"zzz", "yyy", "xxx"}}},
	{name: "rfc/6 comma in quotes", in: "\"a,b\",c", want: [][]string{{"a,b", "c"}}},
	{name: "rfc/7 doubled quote", in: "\"aaa\",\"b\"\"bb\",\"ccc\"", want: [][]string{{"aaa", "b\"bb", "ccc"}}},

	// csv-spectrum.
	{name: "spectrum/comma_in_quotes", in: "first,last,address,city,zip\nJohn,Doe,120 any st.,\"Anytown, WW\",08123\n",
		want: [][]string{{"first", "last", "address", "city", "zip"}, {"John", "Doe", "120 any st.", "Anytown, WW", "08123"}}},
	{name: "spectrum/empty", in: "a,b,c\n1,\"\",\"\"\n2,3,4\n", want: [][]string{{"a", "b", "c"}, {"1", "", ""}, {"2", "3", "4"}}},
	{name: "spectrum/empty_crlf", in: "a,b,c\r\n1,\"\",\"\"\r\n2,3,4\r\n", want: [][]string{{"a", "b", "c"}, {"1", "", ""}, {"2", "3", "4"}}},
	{name: "spectrum/escaped_quotes", in: "a,b\n1,\"ha \"\"ha\"\" ha\"\n3,4\n", want: [][]string{{"a", "b"}, {"1", "ha \"ha\" ha"}, {"3", "4"}}},
	{name: "spectrum/json", in: "key,val\n1,\"{\"\"type\"\": \"\"Point\"\", \"\"coordinates\"\": [102.0, 0.5]}\"\n",
		want: [][]string{{"key", "val"}, {"1", `{"type": "Point", "coordinates": [102.0, 0.5]}`}}},
	{name: "spectrum/location_coordinates", in: "Location,Coordinates\nRegion,\"-1.234, 5.678\"\n",
		want: [][]string{{"Location", "Coordinates"}, {"Region", "-1.234, 5.678"}}},
	{name: "spectrum/newlines", in: "a,b,c\n1,2,3\n\"Once upon \na time\",5,6\n7,8,9\n",
		want: [][]string{{"a", "b", "c"}, {"1", "2", "3"}, {"Once upon \na time", "5", "6"}, {"7", "8", "9"}}},
	{name: "spectrum/newlines_crlf", in: "a,b,c\r\n1,2,3\r\n\"Once upon \r\na time\",5,6\r\n7,8,9\r\n",
		want: [][]string{{"a", "b", "c"}, {"1", "2", "3"}, {"Once upon \r\na time", "5", "6"}, {"7", "8", "9"}},
		std:  [][]string{{"a", "b", "c"}, {"1", "2", "3"}, {"Once upon \na time", "5", "6"}, {"7", "8", "9"}}},
	{name: "spectrum/quotes_and_newlines", in: "a,b\n1,\"ha \n\"\"ha\"\" \nha\"\n3,4\n",
		want: [][]string{{"a", "b"}, {"1", "ha \n\"ha\" \nha"}, {"3", "4"}}},
	{name: "spectrum/simple", in: "a,b,c\n1,2,3\n", want: [][]string{{"a", "b", "c"}, {"1", "2", "3"}}},
	{name: "spectrum/simple_crlf", in: "a,b,c\r\n1,2,3\r\n", want: [][]string{{"a", "b", "c"}, {"1", "2", "3"}}},
	{name: "spectrum/utf8", in: "a,b,c\n1,2,3\n4,5,ʤ\n", want: [][]string{{"a", "b", "c"}, {"1", "2", "3"}, {"4", "5", "ʤ"}}},

	// Empty input, empty lines and the final line break.
	{name: "empty input", in: "", want: nil},
	{name: "only LF", in: "\n", want: nil},
	{name: "only CRLF", in: "\r\n", want: nil},
	{name: "only CR", in: "\r", want: nil},
	{name: "empty lines", in: "\n\na\n\n\nb\n\n", want: [][]string{{"a"}, {"b"}}},
	{name: "empty lines CRLF", in: "a\r\n\r\nb\r\n\r\n", want: [][]string{{"a"}, {"b"}}},
	{name: "single field", in: "a", want: [][]string{{"a"}}},
	{name: "single field LF", in: "a\n", want: [][]string{{"a"}}},
	{name: "space is not empty", in: "a\n \n", want: [][]string{{"a"}, {" "}}},
	{name: "quoted empty is not empty", in: "\"\"\n\"\"", want: [][]string{{""}, {""}}},
	{name: "only commas", in: ",\n,,\r\n", want: [][]string{{"", ""}, {"", "", ""}}},
	{name: "empty line in quotes", in: "\"a\n\nb\"", want: [][]string{{"a\n\nb"}}},

	// Records of different lengths.
	{name: "ragged", in: "a,b,c\nd\ne,f\n", want: [][]string{{"a", "b", "c"}, {"d"}, {"e", "f"}}},

	// Line breaks.
	{name: "LF", in: "a,b\nc,d", want: [][]string{{"a", "b"}, {"c", "d"}}},
	{name: "mixed", in: "a\r\nb\nc\r\n", want: [][]string{{"a"}, {"b"}, {"c"}}},
	{name: "CR is data", in: "a\rb,c\r\n", want: [][]string{{"a\rb", "c"}}},
	{name: "CRs before CRLF", in: "a\r\r\nb\r\r\r\n", want: [][]string{{"a\r"}, {"b\r\r"}}},
	{name: "CR at the end", in: "a,b\r", want: [][]string{{"a", "b"}}},
	{name: "CR at the end after quotes", in: "\"a\"\r", want: [][]string{{"a"}}},
	{name: "CR after quotes", in: "\"a\"\rb", err: true},
	{name: "CR at the start", in: "\ra", want: [][]string{{"\ra"}}},
	{name: "CR in quotes", in: "\"a\rb\"", want: [][]string{{"a\rb"}}},
	{name: "CR before CRLF in quotes", in: "\"a\r\r\nb\"", want: [][]string{{"a\r\r\nb"}}, std: [][]string{{"a\r\nb"}}},
	{name: "LF CR in quotes", in: "\"a\n\rb\"", want: [][]string{{"a\n\rb"}}},

	// Quotes.
	{name: "quote", in: "\"\"\"\"", want: [][]string{{"\""}}},
	{name: "two quotes", in: "\"\"\"\"\"\"", want: [][]string{{"\"\""}}},
	{name: "quotes inside", in: "\"a\"\"b\"\"c\",\"\"\"\"", want: [][]string{{"a\"b\"c", "\""}}},
	{name: "quoted then unquoted", in: "\"a\",b,\"c\"\n", want: [][]string{{"a", "b", "c"}}},
	{name: "bare quote", in: "a\"b", err: true},
	{name: "bare quotes at the end", in: "a\"\"", err: true},
	{name: "bare quote after comma", in: "a,b\"", err: true},
	{name: "text after closing quote", in: "\"a\"b", err: true},
	{name: "single quote character", in: "\"", err: true},
	{name: "unterminated", in: "\"a", err: true},
	{name: "unterminated after doubled", in: "\"a\"\"", err: true},
	{name: "unterminated over lines", in: "a\n\"b\nc\n", err: true},
	{name: "space before quote", in: " \"a\"", err: true},
	{name: "space after quote", in: "\"a\" ", err: true},
	{name: "space after quote before comma", in: "\"a\" ,b", err: true},
	{name: "space before quote after comma", in: "a, \"b\"", err: true},
	{name: "single quotes are data", in: "'a,b'", want: [][]string{{"'a", "b'"}}},

	// Characters.
	{name: "tab", in: "a\tb,\t", want: [][]string{{"a\tb", "\t"}}},
	{name: "NUL", in: "a\x00b,\x00", want: [][]string{{"a\x00b", "\x00"}}},
	{name: "control characters", in: "\x01\x1f\x7f", want: [][]string{{"\x01\x1f\x7f"}}},
	{name: "non-ASCII", in: "日本,語\n\"é,ü\"", want: [][]string{{"日本", "語"}, {"é,ü"}}},
	{name: "invalid UTF-8", in: "\xff,\"\xfe\xc3\"\n\xe6\x97", want: [][]string{{"\xff", "\xfe\xc3"}, {"\xe6\x97"}}},
	{name: "semicolon is data", in: "a;b,c", want: [][]string{{"a;b", "c"}}},

	// The byte order mark.
	{name: "BOM", in: bom + "a,b\n", want: [][]string{{"a", "b"}}, std: [][]string{{bom + "a", "b"}}},
	{name: "BOM before quote", in: bom + "\"a\",b\n", want: [][]string{{"a", "b"}}, stdErr: true},
	{name: "BOM only", in: bom, want: nil, std: [][]string{{bom}}},
	{name: "two BOMs", in: bom + bom + "a", want: [][]string{{bom + "a"}}, std: [][]string{{bom + bom + "a"}}},
	{name: "BOM inside", in: "a" + bom + ",b\n" + bom, want: [][]string{{"a" + bom, "b"}, {bom}}},
}

func TestEdgeCases(t *testing.T) {
	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := csv.Records(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("%q: accepted: %q", tc.in, got)
				}
				var se *csv.SyntaxError
				if !errors.As(err, &se) {
					t.Fatalf("%q: error %T, want *csv.SyntaxError", tc.in, err)
				}
			} else if err != nil {
				t.Fatalf("%q: %v", tc.in, err)
			} else if !equal(got, tc.want) {
				t.Fatalf("%q: got %q, want %q", tc.in, got, tc.want)
			}
			if valid := csv.Valid(tc.in); valid != !tc.err {
				t.Errorf("%q: Valid: %v", tc.in, valid)
			}
			if err := csv.Recognize(tc.in); (err == nil) != !tc.err {
				t.Errorf("%q: Recognize: %v", tc.in, err)
			}
			if _, err := csv.ParseAST(tc.in); (err == nil) != !tc.err {
				t.Errorf("%q: ParseAST: %v", tc.in, err)
			}
			// encoding/csv, as documented by the case.
			std, serr := readStd(tc.in)
			switch {
			case tc.stdErr:
				if serr == nil {
					t.Errorf("%q: encoding/csv accepts it now: %q", tc.in, std)
				}
			case tc.std != nil:
				if serr != nil || !equal(std, tc.std) {
					t.Errorf("%q: encoding/csv: %q, %v; documented %q", tc.in, std, serr, tc.std)
				}
				if equal(std, tc.want) {
					t.Errorf("%q: encoding/csv gives the same records now", tc.in)
				}
			case tc.err:
				if serr == nil {
					t.Errorf("%q: encoding/csv accepts it: %q", tc.in, std)
				}
			default:
				if serr != nil || !equal(std, tc.want) {
					t.Errorf("%q: encoding/csv: %q, %v", tc.in, std, serr)
				}
			}
		})
	}
}

// readStd reads input with encoding/csv with FieldsPerRecord -1 and nothing else changed.
func readStd(input string) ([][]string, error) {
	r := stdcsv.NewReader(strings.NewReader(input))
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

// TestCompareStd runs the edge cases through compareStd, which accounts for the documented differences
// from encoding/csv (the byte order mark and CRLF in quoted fields); only the BOM before a quote, which
// encoding/csv rejects, remains.
func TestCompareStd(t *testing.T) {
	for _, tc := range edgeCases {
		if tc.stdErr {
			continue
		}
		compareStd(t, tc.in)
	}
}

func TestParseAST(t *testing.T) {
	in := bom + "a,\"b\"\"c\"\r\n\nx,\"y\nz\",\n"
	f, err := csv.ParseAST(in)
	if err != nil {
		t.Fatal(err)
	}
	type field struct {
		text       string
		start, end int
		quoted     bool
		value      string
	}
	want := []struct {
		start, end int
		fields     []field
	}{
		{1, 9, []field{{"a", 1, 2, false, "a"}, {"\"b\"\"c\"", 3, 9, true, "b\"c"}}},
		{11, 11, []field{{"", 11, 11, false, ""}}},
		{12, 20, []field{{"x", 12, 13, false, "x"}, {"\"y\nz\"", 14, 19, true, "y\nz"}, {"", 20, 20, false, ""}}},
	}
	if f.Start != 0 || f.End != len([]rune(in)) {
		t.Errorf("file span %v", f.Span)
	}
	if len(f.Records) != len(want) {
		t.Fatalf("%d records, want %d", len(f.Records), len(want))
	}
	for i, r := range f.Records {
		w := want[i]
		if r.Start != w.start || r.End != w.end || len(r.Fields) != len(w.fields) {
			t.Fatalf("record %d: %v with %d fields, want %d-%d with %d", i, r.Span, len(r.Fields), w.start, w.end, len(w.fields))
		}
		if r.Blank() != (i == 1) {
			t.Errorf("record %d: Blank %v", i, r.Blank())
		}
		var values []string
		for j, fl := range r.Fields {
			wf := w.fields[j]
			if fl.Text != wf.text || fl.Start != wf.start || fl.End != wf.end || fl.Quoted() != wf.quoted || fl.Value() != wf.value {
				t.Errorf("record %d field %d: %q %v quoted %v value %q, want %+v", i, j, fl.Text, fl.Span, fl.Quoted(), fl.Value(), wf)
			}
			values = append(values, wf.value)
		}
		if !reflect.DeepEqual(r.Strings(), values) {
			t.Errorf("record %d: Strings %q", i, r.Strings())
		}
	}
	// Byte positions.
	f, err = csv.ParseAST("é,\"ü\"", csv.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if fl := f.Records[0].Fields[1]; fl.Start != 3 || fl.End != 7 {
		t.Errorf("byte span %v", fl.Span)
	}
}

func TestSyntaxError(t *testing.T) {
	for _, tc := range []struct {
		in        string
		line, col int
	}{
		{"a,b\nc\"d\n", 2, 2},
		{"a,\"b\"c", 1, 6},
		{"a\n\"b\nc", 3, 2},
		{"\"a\" ,b", 1, 4},
	} {
		_, err := csv.Records(tc.in)
		var se *csv.SyntaxError
		if !errors.As(err, &se) {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if se.Line != tc.line || se.Col != tc.col {
			t.Errorf("%q: %d:%d (%v), want %d:%d", tc.in, se.Line, se.Col, err, tc.line, tc.col)
		}
	}
}

func TestTable(t *testing.T) {
	header, rows, err := csv.Table("\nname,age\r\nalice,30\n\n\"bob, jr.\",\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(header, []string{"name", "age"}) || !reflect.DeepEqual(rows, [][]string{{"alice", "30"}, {"bob, jr.", ""}}) {
		t.Errorf("header %q, rows %q", header, rows)
	}

	header, rows, err = csv.Table("a,b\n")
	if err != nil || len(header) != 2 || rows == nil || len(rows) != 0 {
		t.Errorf("header only: %q %q %v", header, rows, err)
	}

	for _, in := range []string{"", "\n\r\n", bom} {
		if _, _, err := csv.Table(in); err != csv.ErrNoHeader {
			t.Errorf("%q: %v, want ErrNoHeader", in, err)
		}
	}

	_, _, err = csv.Table("a,b\n1,2\n\n\"x\ny\"\n")
	var fe *csv.FieldCountError
	if !errors.As(err, &fe) || *fe != (csv.FieldCountError{Line: 4, Fields: 1, Want: 2}) {
		t.Errorf("field count: %v", err)
	}
	if err != nil && err.Error() != "csv: line 4: record has 1 fields, the header has 2" {
		t.Errorf("message: %v", err)
	}

	var se *csv.SyntaxError
	if _, _, err := csv.Table("a\n\"b"); !errors.As(err, &se) {
		t.Errorf("syntax error: %v", err)
	}
}

// TestRandom writes random records with encoding/csv's Writer, with LF and with CRLF, and checks that
// Records reads back what was written and what encoding/csv reads.
func TestRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 2000 {
		recs := randomRecords(r)
		var b strings.Builder
		w := stdcsv.NewWriter(&b)
		w.UseCRLF = i%2 == 1
		if err := w.WriteAll(recs); err != nil {
			t.Fatal(err)
		}
		got, err := compareStd(t, b.String())
		if err != nil {
			t.Fatalf("%q: %v", b.String(), err)
		}
		if w.UseCRLF {
			continue // the Writer drops CRs in values and writes LFs as CRLFs
		}
		// The Writer writes a record of one empty field as an empty line, which readers skip.
		var want [][]string
		for _, rec := range recs {
			if len(rec) != 1 || rec[0] != "" {
				want = append(want, rec)
			}
		}
		if !equal(got, want) {
			t.Fatalf("%q: read %q, wrote %q", b.String(), got, want)
		}
	}
}

func randomRecords(r *rand.Rand) [][]string {
	const chars = "aZ09 ,\"\r\n\t'é日😀\x00\xff"
	recs := make([][]string, r.IntN(6))
	for i := range recs {
		recs[i] = make([]string, 1+r.IntN(5))
		for j := range recs[i] {
			var b strings.Builder
			for range r.IntN(6) {
				k := r.IntN(len(chars))
				b.WriteString(chars[k : k+1]) // bytes, so some values are not UTF-8
			}
			recs[i][j] = b.String()
		}
	}
	return recs
}

// FuzzRecords compares the parser with encoding/csv on arbitrary input (see compareStd for the
// differences it accounts for).
func FuzzRecords(f *testing.F) {
	for _, tc := range edgeCases {
		if !tc.stdErr {
			f.Add(tc.in)
		}
	}
	f.Fuzz(func(t *testing.T, input string) {
		compareStd(t, input)
	})
}

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := csv.Parse(string(data))
		if n != nil {
			got = n.String()
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}
