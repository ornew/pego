package genrt

import "testing"

func TestActionTrackingReleasesReferences(t *testing.T) {
	// Any value but a function is a result: a value other than a node is boxed.
	for _, name := range []string{"nil", "node", "value", "invalid", "error"} {
		t.Run(name, func(t *testing.T) {
			prefix := &Node{text: "keep"}
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
					case "value":
						return 1
					case "invalid":
						return func(...any) any { return nil }
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
			if name == "node" && (got != made || got.start != 3 || got.end != 4 || got.fresh) {
				t.Fatal("returned node changed identity or has wrong span/freshness")
			}
		})
	}
}

func TestTypedActionTrackingReleasesReferences(t *testing.T) {
	for _, name := range []string{"nil", "node", "value", "invalid"} {
		t.Run(name, func(t *testing.T) {
			prefix := &tnode{typ: "keep"}
			made := &tnode{fresh: true}
			p := &tparser{parser: &parser{}, created: []tval{prefix, made}}
			c := &tctx{p: p, cbase: 1, start: 3, end: 4}
			var got any
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				var value any
				if name == "node" {
					value = made
				} else if name == "value" {
					value = 1
				} else if name == "invalid" {
					value = func(...any) any { return nil }
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
