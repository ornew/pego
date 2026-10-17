package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestFingerprintTerminalFlag(t *testing.T) {
	p, err := pego.CompileSource("rule main = v1:\"x\" => $v1\n", "main")
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := p.Parse("x")
	if err != nil {
		t.Fatal(err)
	}
	if !terminal.IsTerminal() {
		t.Fatal("fixture is not terminal")
	}
	// These explicit records specify the encoding contract independently of
	// fingerprint's node-header construction. Only Terminal differs.
	record := `{"ID":0,"Type":"Match","Rule":"","Start":0,"End":1,"Text":"x","Children":0,"Fields":0,"NilKids":true,"NilFields":true,"Terminal":true}`
	reference := func(header string) string {
		h := sha256.New()
		for _, item := range []string{fingerprintFormat, "node", header} {
			var size [binary.MaxVarintLen64]byte
			n := binary.PutUvarint(size[:], uint64(len(item)))
			h.Write(size[:n])
			h.Write([]byte(item))
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	got, err := fingerprint(terminal)
	if err != nil {
		t.Fatal(err)
	}
	if got != reference(record) {
		t.Fatalf("terminal encoding: got %s, want %s", got, reference(record))
	}
	nonterminalRecord := strings.Replace(record, `"Terminal":true`, `"Terminal":false`, 1)
	if got == reference(nonterminalRecord) {
		t.Fatal("fingerprint ignores terminal state")
	}
}

func TestFingerprintSharingAndNodeCycles(t *testing.T) {
	hash := func(n *pego.Node) string {
		t.Helper()
		h, err := fingerprint(n)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	leaf := &pego.Node{Text: "x"}
	shared := &pego.Node{Children: []*pego.Node{leaf, leaf}}
	separate := &pego.Node{Children: []*pego.Node{{Text: "x"}, {Text: "x"}}}
	if hash(shared) == hash(separate) {
		t.Fatal("fingerprint loses node sharing")
	}
	cycle := &pego.Node{}
	cycle.Children = []*pego.Node{cycle}
	copyCycle := &pego.Node{}
	copyCycle.Children = []*pego.Node{copyCycle}
	if hash(cycle) != hash(copyCycle) {
		t.Fatal("fingerprint depends on pointer addresses")
	}
}

func TestFingerprintDeepValuesAndCanonicalFields(t *testing.T) {
	leaf := &pego.Node{Text: "x"}
	root := leaf
	for range 20000 {
		root = &pego.Node{Children: []*pego.Node{root}}
	}
	before, err := fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	leaf.Text = "changed"
	after, err := fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("fingerprint omits deep descendants")
	}
	a, b := &pego.Node{}, &pego.Node{}
	add := func(n *pego.Node, name string, value any) {
		n.Fields = append(n.Fields, struct {
			Name  string
			Value any
		}{name, value})
	}
	add(a, "a", []any{1, "x", nil})
	add(a, "b", map[string]any{"x": 1, "y": true})
	add(b, "b", map[string]any{"y": true, "x": 1})
	add(b, "a", []any{1, "x", nil})
	x, err := fingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := fingerprint(b)
	if err != nil {
		t.Fatal(err)
	}
	if x != y {
		t.Fatal("field or map order changes fingerprint")
	}
	a.Fields[0].Value = []any{1, "different", nil}
	z, err := fingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	if x == z {
		t.Fatal("fingerprint omits field values")
	}
}
