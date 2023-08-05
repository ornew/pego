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
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("grammer.json path is required")
	}
	p := os.Args[1]
	o := path.Join(path.Dir(p), "parser_gen.go")
	var g grammer.Grammer
	peg, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	err = json.Unmarshal(peg, &g)
	if err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
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
		fmt.Println(string(b.Bytes()))
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
