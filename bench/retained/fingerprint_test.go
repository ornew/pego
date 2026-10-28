package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/value"
)

func TestFingerprintTerminalFlag(t *testing.T) {
	p, err := pego.CompileSource("@start\nrule main = v1:\"x\" => $v1\n", "main")
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
	record := `{"ID":0,"Kind":"string","Type":"string","Rule":"","Start":0,"End":1,"Text":"x","Elems":0,"Fields":0,"Terminal":true}`
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
	leaf := stringNode("x")
	shared := tupleNode(leaf, leaf)
	separate := tupleNode(stringNode("x"), stringNode("x"))
	if hash(shared) == hash(separate) {
		t.Fatal("fingerprint loses node sharing")
	}
	cycle := tupleNode()
	value.SetNodeElems(cycle, []*pego.Node{cycle})
	copyCycle := tupleNode()
	value.SetNodeElems(copyCycle, []*pego.Node{copyCycle})
	if hash(cycle) != hash(copyCycle) {
		t.Fatal("fingerprint depends on pointer addresses")
	}
}

func TestFingerprintDeepValuesAndCanonicalFields(t *testing.T) {
	leaf := stringNode("x")
	root := leaf
	for range 20000 {
		root = tupleNode(root)
	}
	before, err := fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	value.SetNodeText(leaf, "changed")
	after, err := fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("fingerprint omits deep descendants")
	}
	a, b := structNode(), structNode()
	add := value.SetNodeField
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
	value.SetNodeField(a, "a", []any{1, "different", nil})
	z, err := fingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	if x == z {
		t.Fatal("fingerprint omits field values")
	}
}

func stringNode(text string) *pego.Node {
	return value.NewNode(pego.KindString, 0, len(text), text, nil, "", nil, nil)
}

func tupleNode(elems ...*pego.Node) *pego.Node {
	return value.NewNode(pego.KindTuple, 0, 0, "", elems, "", nil, nil)
}

func structNode() *pego.Node {
	return value.NewNode(pego.KindStruct, 0, 0, "", nil, "S", nil, nil)
}
