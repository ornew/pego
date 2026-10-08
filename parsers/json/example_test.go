package json_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/json"
)

func ExampleDecode() {
	v, err := json.Decode(`{"name": "pego", "tags": ["peg", "pratt"], "stars": 42}`)
	if err != nil {
		panic(err)
	}
	m := v.(map[string]any)
	fmt.Println(m["name"], m["tags"], m["stars"])
	// Output: pego [peg pratt] 42
}

func ExampleParseAST() {
	v, err := json.ParseAST(`{"a": [1, true], "b": "x\ty"}`)
	if err != nil {
		panic(err)
	}
	for _, m := range v.(*json.Object).Members {
		fmt.Printf("%q at %d-%d: %T\n", m.Key.Value(), m.Span.Start, m.Span.End, m.Value)
	}
	// Output:
	// "a" at 1-15: *json.Array
	// "b" at 17-28: *json.String
}

func ExampleParseAST_syntaxError() {
	_, err := json.ParseAST(`{"a": [1, 2,]}`)
	var se *json.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, se.Message())
	}
	// Output: 1 13 syntax error: expected "-", "0", "[", "\"", "false", "null", "true", "{", (? \t\r\n), (?1-9)
}

func ExampleValid() {
	fmt.Println(json.Valid(`[1, 2, 3]`), json.Valid(`[1, 2, 3,]`))
	// Output: true false
}
