package genrt

import (
	"encoding/json"
	"testing"
)

func TestNodeFrameScratchBoundaries(t *testing.T) {
	p := &parser{}
	if p.newFrame(0) != emptyFrame || len(p.captureScratch.frames) != 0 {
		t.Fatal("empty captures allocated scratch")
	}
	var frames []*frame
	for _, size := range []int{1, 7, 255, 256, 256, 256, 1, 256, 257, 1025} {
		f := p.newFrame(size)
		if len(f.vals) != size || cap(f.vals) != size {
			t.Fatal("capture extent changed")
		}
		for _, old := range frames {
			if old == f || &old.vals[0] == &f.vals[0] {
				t.Fatal("live capture frames or slots reused")
			}
		}
		f.vals[size-1] = &Node{text: "live"}
		frames = append(frames, f)
	}
	for _, f := range frames {
		if f.vals[len(f.vals)-1].text != "live" {
			t.Fatal("later allocation changed an earlier capture")
		}
	}
	if len(p.ptrSlab) != 0 || len(p.frameSlab) != 0 {
		t.Fatal("capture scratch consumed result or reference slabs")
	}
	large := &parser{}
	large.newFrame(ptrChunk/4 + 1)
	if len(large.captureScratch.frames) != 0 || len(large.captureScratch.slots) != 0 {
		t.Fatal("large individual scope was pooled")
	}
}

func TestNodeFrameScratchChunkStability(t *testing.T) {
	p := &parser{}
	var first *frame
	for i := 0; i < nodeChunk*3+1; i++ {
		f := p.newFrame(7)
		if i == 0 {
			first = f
			f.vals[0] = &Node{text: "first"}
		}
	}
	if first.vals[0].text != "first" || len(p.captureScratch.frames) != 4 || len(p.captureScratch.slots) < 2 {
		t.Fatal("growing directories invalidated live frames")
	}
	p.captureScratch.reset()
	if got := p.newFrame(7); got != first || got.vals[0] != nil {
		t.Fatal("reset did not reuse a cleared frame")
	}
}

func TestNodeFrameScratchReleaseOwnership(t *testing.T) {
	p := &parser{}
	f := p.newFrame(2)
	captured := p.newNode(Node{kind: kindMatch, text: "é", start: 1, end: 2, terminal: true})
	f.vals[0] = captured
	result := p.attachCaptures(nil, []string{"x", "missing"}, f, 0, 2)
	before, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	chunks, slots := p.captureScratch.frames, p.captureScratch.slots
	// Even unused backing must not keep overwritten or abandoned values alive.
	chunks[0][nodeChunk-1].vals = []*Node{captured}
	slots[0][ptrChunk-1] = captured
	p.trail = append(p.trail, undo{f: f, old: captured})
	p.saved = append(p.saved, captured)
	p.release()
	for _, chunk := range chunks {
		for _, f := range chunk {
			if f.vals != nil {
				t.Fatal("released frame retains slots")
			}
		}
	}
	for _, chunk := range slots {
		for _, v := range chunk {
			if v != nil {
				t.Fatal("released slots retain values")
			}
		}
	}
	// Use the arena directly: release returned p to its pool.
	a := p.captureScratch
	a.alloc(2).vals[0] = &Node{text: "replacement"}
	defer a.reset()
	after, err := json.Marshal(result)
	if err != nil || string(before) != string(after) || captured.text != "é" {
		t.Fatal("scratch reuse changed a retained Node")
	}
}

func TestNodeFrameScratchCompleteBackingBound(t *testing.T) {
	for _, extra := range []int{0, 1} {
		frameChunk, slotChunk := new([nodeChunk]frame), new([ptrChunk]*Node)
		f := make([]*[nodeChunk]frame, 1)
		f[0] = frameChunk
		// The combined complete backing is exactly at or one word above the cap.
		s := make([]*[ptrChunk]*Node, 1, frameScratchLimit-9-3*nodeChunk-ptrChunk-cap(f)+extra)
		s[0] = slotChunk
		a := frameScratch{frames: f, slots: s, frameN: 1, slotOff: 1}
		frameChunk[0].vals = slotChunk[:1]
		slotChunk[0] = &Node{text: "old"}
		a.reset()
		if frameChunk[0].vals != nil || slotChunk[0] != nil {
			t.Fatal("discard or reuse retained old values")
		}
		if extra == 0 && (len(a.frames) != 1 || len(a.slots) != 1) {
			t.Fatal("scratch at its combined budget was discarded")
		}
		if extra == 1 && (a.frames != nil || a.slots != nil) {
			t.Fatal("scratch above its combined budget was retained")
		}
	}
	for _, a := range []frameScratch{
		{frames: make([]*[nodeChunk]frame, 0, frameScratchLimit+1)},
		{slots: make([]*[ptrChunk]*Node, 0, frameScratchLimit+1)},
	} {
		a.reset()
		if a.frames != nil || a.slots != nil {
			t.Fatal("oversized unused directory retained")
		}
	}
}
