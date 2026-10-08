package yaml_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/yaml"
)

func ExampleLoad() {
	v, err := yaml.Load(`
name: pego
tags: [peg, pratt]
stars: 42
license: ~
`)
	if err != nil {
		panic(err)
	}
	m := v.(map[string]any)
	fmt.Println(m["name"], m["tags"], m["stars"], m["license"])
	// Output: pego [peg pratt] 42 <nil>
}

func ExampleLoadAll() {
	docs, err := yaml.LoadAll("--- a\n--- [b, c]\n")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%q\n", docs)
	// Output: ["a" ["b" "c"]]
}

func ExampleParseAST() {
	s, err := yaml.ParseAST("key: &x value # comment\nlist:\n- 'single'\n- *x\n")
	if err != nil {
		panic(err)
	}
	for _, p := range s.Documents[0].Root.(*yaml.Mapping).Pairs {
		k := p.Key.(*yaml.Scalar)
		fmt.Printf("%s at %d-%d: %T\n", k.Value(), k.Start, k.End, p.Value)
	}
	// Output:
	// key at 0-3: *yaml.Scalar
	// list at 24-28: *yaml.Sequence
}

func ExampleScalar_Value() {
	s, err := yaml.ParseAST(`
plain: a
  b
quoted: "tab\there"
literal: |
  line 1
  line 2
folded: >-
  one
  two
`)
	if err != nil {
		panic(err)
	}
	for _, p := range s.Documents[0].Root.(*yaml.Mapping).Pairs {
		v := p.Value.(*yaml.Scalar)
		fmt.Printf("%s: %q from %q\n", p.Key.(*yaml.Scalar).Text, v.Value(), v.Text)
	}
	// Output:
	// plain: "a b" from "a\n  b"
	// quoted: "tab\there" from "\"tab\\there\""
	// literal: "line 1\nline 2\n" from "|\n  line 1\n  line 2\n"
	// folded: "one two" from ">-\n  one\n  two\n"
}

func ExampleEvents() {
	events, err := yaml.Events("--- !!map\nkey: [a, 'b']\n")
	if err != nil {
		panic(err)
	}
	fmt.Print(events)
	// Output:
	// +STR
	// +DOC ---
	// +MAP <tag:yaml.org,2002:map>
	// =VAL :key
	// +SEQ []
	// =VAL :a
	// =VAL 'b
	// -SEQ
	// -MAP
	// -DOC
	// -STR
}

func ExampleParseAST_syntaxError() {
	_, err := yaml.ParseAST("key: [a, b}\n") // a closing brace for a sequence
	var se *yaml.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, se.Expected[:3])
	}
	// Output: 1 11 ["," ":" "\n"]
}

func ExampleDocument_ResolveTag() {
	s, err := yaml.ParseAST("%TAG !e! tag:example.com,2000:\n--- !e!point%21 &p {x: 1, y: !!int 2}\n")
	if err != nil {
		panic(err)
	}
	d := s.Documents[0]
	props := yaml.PropertiesOf(d.Root)
	tag, err := d.ResolveTag(props.Tag)
	fmt.Println(tag, props.Anchor.Name(), err)
	y := d.Root.(*yaml.Mapping).Pairs[1].Value
	fmt.Println(d.ResolveTag(yaml.PropertiesOf(y).Tag))
	// Output:
	// tag:example.com,2000:point! p <nil>
	// tag:yaml.org,2002:int <nil>
}

func ExampleValid() {
	fmt.Println(yaml.Valid("a: b"), yaml.Valid("a: b: c"), yaml.Valid("a: *undefined"))
	// Output: true false false
}
