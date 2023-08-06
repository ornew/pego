package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"path"

	"github.com/ornew/pego/pkg/generator"
	"github.com/ornew/pego/pkg/grammer"
	grammerparser "github.com/ornew/pego/pkg/grammer/parser"
	"github.com/ornew/pego/pkg/parser"
)

const (
	JSON int = iota
	PEGO
)

func run() error {
	var mode int
	if len(os.Args) < 2 {
		return fmt.Errorf("grammer file path is required")
	}
	p := os.Args[1]
	o := path.Join(path.Dir(p), "parser_gen.go")
	var g grammer.Grammer
	peg, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	mode = PEGO
	if path.Ext(p) == ".json" {
		mode = JSON
	}
	switch mode {
	case JSON:
		err = json.Unmarshal(peg, &g)
		if err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
	case PEGO:
		p, err := parser.NewParser(bytes.NewReader(peg))
		if err != nil {
			panic(err)
		}
		root, err := grammerparser.Parse(p)
		if err != nil {
			panic(err)
		}
		if root == nil {
			panic(fmt.Errorf("root is nil"))
		}
		if root.Code == 0 {
			panic(fmt.Errorf("parse failed: %s", root))
		}
		if len(root.Children) != 1 {
			panic(fmt.Errorf("length of root children is not 1: %s", root))
		}
		e := grammerparser.Extractor{
			GetText: p.GetText,
		}
		g = *e.GetFile(root.Children[0])
	default:
		panic(fmt.Errorf("unknown mode: %d", mode))
	}
	fmt.Println(g.String())
	f, err := os.OpenFile(o, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	var b bytes.Buffer
	err = generator.Generate(&b, &g)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}
	out, err := format.Source(b.Bytes())
	if err != nil {
		fmt.Println(b.String())
		return fmt.Errorf("format: %w", err)
	}
	_, err = io.Copy(f, bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
