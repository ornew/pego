package csv_test

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ornew/pego/parsers/csv"
)

func ExampleRecords() {
	recs, err := csv.Records("name,note\r\nalice,\"likes \"\"quotes\"\", commas\"\r\n\r\nbob,\"two\nlines\"\r\n")
	if err != nil {
		panic(err)
	}
	for _, r := range recs {
		fmt.Printf("%q\n", r)
	}
	// Output:
	// ["name" "note"]
	// ["alice" "likes \"quotes\", commas"]
	// ["bob" "two\nlines"]
}

func ExampleTable() {
	header, rows, err := csv.Table("id,name\n1,alice\n2,bob\n")
	if err != nil {
		panic(err)
	}
	name := slices.Index(header, "name")
	for _, r := range rows {
		fmt.Println(r[name])
	}

	_, _, err = csv.Table("id,name\n1,alice\n2\n")
	fmt.Println(err)
	// Output:
	// alice
	// bob
	// csv: line 3: record has 1 fields, the header has 2
}

func ExampleParseAST() {
	f, err := csv.ParseAST("a,\"b\"\"c\"\n\nd\n")
	if err != nil {
		panic(err)
	}
	for _, r := range f.Records {
		fmt.Printf("record %d-%d blank=%v\n", r.Start, r.End, r.Blank())
		for _, fl := range r.Fields {
			fmt.Printf("  %d-%d %s quoted=%v value=%q\n", fl.Start, fl.End, fl.Text, fl.Quoted(), fl.Value())
		}
	}
	// Output:
	// record 0-9 blank=false
	//   0-1 a quoted=false value="a"
	//   2-8 "b""c" quoted=true value="b\"c"
	// record 9-10 blank=true
	//   9-9  quoted=false value=""
	// record 10-12 blank=false
	//   10-11 d quoted=false value="d"
}

func ExampleRecords_syntaxError() {
	_, err := csv.Records("a,b\nc,d\"e\n")
	var se *csv.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, err)
	}
	// Output: 2 4 2:4: syntax error: expected ",", "\n", "\r", "\r\n", (?^,"\r\n), end of input
}

func ExampleValid() {
	fmt.Println(csv.Valid("a,\"b\"\n"), csv.Valid("a,\"b\" \n"))
	// Output: true false
}
