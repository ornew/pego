package sourceast

import "testing"

func TestClaim(t *testing.T) {
	// "a /*l*/ b /*t*/ c": a gap before b with a leading group and one after b
	// with a trailing group.
	//  0123456789012345678
	in := "a /*l*/ b /*t*/ c"
	gaps := []Gap{
		{Start: 1, End: 8, Leading: "l", HasNext: true},
		{Start: 9, End: 16, Trailing: "t", HasTrailing: true, HasNext: true},
	}
	nodes := []Node{
		{Events: []Event{{8, 9}}, Created: Unset},   // b
		{Events: []Event{{0, 9}}, Created: Unset},   // a b: ends where b does, starts elsewhere
		{Events: []Event{{8, 9}}, Created: Unset},   // an outer node that starts and ends like b
		{Events: []Event{{16, 17}}, Created: Unset}, // c
	}
	got, err := NewPositions(in).Claim(gaps, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].LeadingComments != "" || got[0].TrailingComments != "" {
		t.Fatalf("an inner node keeps groups an outer node takes: %+v", got[0])
	}
	if got[2].LeadingComments != "l" || got[2].TrailingComments != "t" {
		t.Fatalf("the outermost node takes the groups at its tokens: %+v", got[2])
	}
	if got[1].TrailingComments != "" {
		t.Fatalf("a node that ends where a later node does loses the trailing group: %+v", got[1])
	}
	if got[3].LeadingComments != "" {
		t.Fatalf("a group is owned once: %+v", got[3])
	}
}

func TestClaimBoundaries(t *testing.T) {
	// "(a /*t*/ ; /*u*/ )": nothing follows the separator's gap, and the node
	// before the separator keeps its trailing group unless the gap after the
	// separator has one.
	in := "(a /*t*/ ; /*u*/ )"
	gaps := []Gap{
		{Start: 2, End: 9, Trailing: "t", HasTrailing: true, HasNext: true},
		{Start: 10, End: 17, Trailing: "u", HasTrailing: true, HasNext: false},
	}
	nodes := []Node{{Events: []Event{{1, 2}}, Later: []int{10}, Created: Unset}}
	got, err := NewPositions(in).Claim(gaps, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TrailingComments != "u" {
		t.Fatalf("a trailing group after the separator replaces the one before it: %+v", got[0])
	}
	gaps[1].HasTrailing = false
	if got, _ = NewPositions(in).Claim(gaps, nodes); got[0].TrailingComments != "t" {
		t.Fatalf("a separator without a trailing group keeps the earlier one: %+v", got[0])
	}
	gaps[0].HasNext = false
	nodes = append(nodes, Node{Events: []Event{{9, 10}}, Created: Unset})
	if got, _ = NewPositions(in).Claim(gaps, nodes); got[1].LeadingComments != "" {
		t.Fatalf("a gap with no next token leads nobody: %+v", got[1])
	}
}

func TestClaimDiagnosticPosition(t *testing.T) {
	// A binary node starts at its left operand and was created at its operator.
	in := "a + b"
	nodes := []Node{
		{Events: []Event{{0, 5}}, Created: 2},
		{Events: []Event{{0, 5}}, Created: 0},
		{Events: []Event{{2, 3}, {0, 3}}, Created: 2}, // completed at the operator, then from its operand
	}
	got, err := NewPositions(in).Claim(nil, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].DiagnosticPos.Col != 3 || got[0].Pos.Col != 1 {
		t.Fatalf("operator position: %+v", got[0])
	}
	if got[1].DiagnosticPos.IsValid() {
		t.Fatalf("a node created at its start has no separate diagnostic position: %+v", got[1])
	}
	if got[2].DiagnosticPos.Col != 3 || got[2].Pos.Col != 1 {
		t.Fatalf("a node completed at its operator then from its operand: %+v", got[2])
	}
	if _, err := NewPositions(in).Claim(nil, []Node{{Events: []Event{{0, 99}}, Created: Unset}}); err == nil {
		t.Fatal("accepted invalid span")
	}
	if _, err := NewPositions(in).Claim(nil, []Node{{Created: Unset}}); err == nil {
		t.Fatal("accepted a node that is never completed")
	}
}
