package json_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/json"
	"github.com/ornew/pego/parsers/json/types"
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
	for _, m := range v.(*types.Object).Members {
		fmt.Printf("%q at %d-%d: %T\n", m.Key.Value(), m.Span.Start, m.Span.End, m.Value)
	}
	// Output:
	// "a" at 1-15: *types.Array
	// "b" at 17-28: *types.String
}

func ExampleParseAST_syntaxError() {
	_, err := json.ParseAST(`{"a": [1, 2,]}`)
	var se *json.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, se.Message())
	}
	// Output: 1 13 syntax error
}

func ExampleValid() {
	fmt.Println(json.Valid(`[1, 2, 3]`), json.Valid(`[1, 2, 3,]`))
	// Output: true false
}
