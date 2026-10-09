package genrt

import "testing"

func TestActionTrackingReleasesReferences(t *testing.T) {
	for _, name := range []string{"nil", "node", "invalid", "error"} {
		t.Run(name, func(t *testing.T) {
			prefix := &Node{Text: "keep"}
			made := &Node{fresh: true}
			p := &parser{created: []*Node{prefix}}
			c := &actx{p: p, cbase: 1, start: 3, end: 4}
			var got *Node
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				got = c.result(func(*actx) any {
					p.created = append(p.created, made)
					switch name {
					case "node":
						return made
					case "invalid":
						return 1
					case "error":
						evalErrorf("failed")
					}
					return nil
				}, "test")
			}()
			if (panicValue != nil) != (name == "invalid" || name == "error") {
				t.Fatalf("unexpected panic: %v", panicValue)
			}
			if len(p.created) != 1 || p.created[0] != prefix || p.created[:cap(p.created)][1] != nil {
				t.Fatal("tracking retains a discarded reference or changed its prefix")
			}
			if name == "node" && (got != made || got.Start != 3 || got.End != 4 || got.fresh) {
				t.Fatal("returned node changed identity or has wrong span/freshness")
			}
		})
	}
}

func TestTypedActionTrackingReleasesReferences(t *testing.T) {
	for _, name := range []string{"nil", "node", "invalid"} {
		t.Run(name, func(t *testing.T) {
			prefix := &tnode{typ: "keep"}
			made := &tnode{fresh: true}
			p := &tparser{created: []tval{prefix, made}}
			c := &tctx{p: p, cbase: 1, start: 3, end: 4}
			var got any
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				var value any
				if name == "node" {
					value = made
				} else if name == "invalid" {
					value = 1
				}
				got = c.finish(value)
			}()
			if (panicValue != nil) != (name == "invalid") {
				t.Fatalf("unexpected panic: %v", panicValue)
			}
			if len(p.created) != 1 || p.created[0] != prefix || p.created[:cap(p.created)][1] != nil {
				t.Fatal("tracking retains a discarded reference or changed its prefix")
			}
			if name == "node" && (got != made || made.Start != 3 || made.End != 4 || made.fresh) {
				t.Fatal("returned node changed identity or has wrong span/freshness")
			}
		})
	}
}
