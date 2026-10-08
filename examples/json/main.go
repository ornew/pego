// Command json parses JSON with json.pego, converts it to Go values and pretty-prints it.
//
//	echo '{"a": [1, true, null]}' | go run ./examples/json
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ornew/pego"
)

//go:embed json.pego
var grammar string

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	v, err := decode(p, string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
}

func decode(p *pego.Parser, src string) (any, error) {
	n, err := p.Parse(src)
	if err != nil {
		return nil, err
	}
	return toGo(n)
}

// toGo converts the AST of json.pego into Go values of the same shape that encoding/json produces.
func toGo(n *pego.Node) (any, error) {
	switch n.Type() {
	case "Object":
		obj := map[string]any{}
		for _, m := range n.Field("Members").(*pego.Node).Children {
			key, err := strconv.Unquote(`"` + m.Field("Key").(*pego.Node).Text + `"`)
			if err != nil {
				return nil, err
			}
			if obj[key], err = toGo(m.Field("Value").(*pego.Node)); err != nil {
				return nil, err
			}
		}
		return obj, nil
	case "Array":
		arr := []any{}
		for _, e := range n.Field("Elements").(*pego.Node).Children {
			v, err := toGo(e)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case "String":
		// JSON allows \/, which Go string literals do not, so replace it first.
		var s []byte
		for i := 0; i < len(n.Text); i++ {
			if n.Text[i] == '\\' && i+1 < len(n.Text) && n.Text[i+1] == '/' {
				s = append(s, '/')
				i++
				continue
			}
			s = append(s, n.Text[i])
		}
		return strconv.Unquote(`"` + string(s) + `"`)
	case "Number":
		return strconv.ParseFloat(n.Text, 64)
	case "Bool":
		return n.Text == "true", nil
	case "Null":
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected node %s", n.Type())
}
