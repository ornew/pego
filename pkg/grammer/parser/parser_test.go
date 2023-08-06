package parser

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ornew/pego/pkg/grammer"
	bootparser "github.com/ornew/pego/pkg/grammer/parser/boot"
	"github.com/ornew/pego/pkg/parser"
	"github.com/ornew/pego/pkg/printer"
	"github.com/stretchr/testify/require"
)

//go:embed boot/grammer.json
var grammerJSON []byte

//go:embed grammer.pego
var grammerPEGO []byte

func TestParse(t *testing.T) {
	fmt.Println(string(grammerPEGO))
	var gj grammer.Grammer
	err := json.Unmarshal(grammerJSON, &gj)
	require.NoError(t, err)
	p, err := parser.NewParser(bytes.NewReader(grammerPEGO))
	if err != nil {
		t.Fatal(err)
	}
	root, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	pp := printer.Printer{
		GetText:  p.GetText,
		Demangle: Demangle,
	}
	fmt.Println(pp.Print(root))
	e := Extractor{
		GetText: p.GetText,
	}
	require.Len(t, root.Children, 1)
	g := e.GetFile(root.Children[0])
	b, _ := json.MarshalIndent(g, "", "  ")
	fmt.Println(string(b))
	fmt.Println(g.String())
	if diff := cmp.Diff(&gj, g); diff != "" {
		t.Fatal(diff)
	}
}

func TestBootParse(t *testing.T) {
	fmt.Println(string(grammerPEGO))
	var gj grammer.Grammer
	err := json.Unmarshal(grammerJSON, &gj)
	require.NoError(t, err)
	p, err := parser.NewParser(bytes.NewReader(grammerPEGO))
	if err != nil {
		t.Fatal(err)
	}
	root, err := bootparser.Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	pp := printer.Printer{
		GetText:  p.GetText,
		Demangle: Demangle,
	}
	fmt.Println(pp.Print(root))
	e := Extractor{
		GetText: p.GetText,
	}
	require.Len(t, root.Children, 1)
	g := e.GetFile(root.Children[0])
	b, _ := json.MarshalIndent(g, "", "  ")
	t.Log("get grammer JSON")
	fmt.Println(string(b))
	t.Log("get grammer")
	fmt.Println(g.String())
	t.Log("boot grammer")
	fmt.Println(gj.String())
	if diff := cmp.Diff(&gj, g); diff != "" {
		t.Fatal(diff)
	}
}
