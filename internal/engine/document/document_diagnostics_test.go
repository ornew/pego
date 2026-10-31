package document

import (
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
)

func TestDocumentDiagnosticContainers(t *testing.T) {
	p := &core.Parser{}
	if copyDocumentDiagnostics(p, nil) != nil {
		t.Fatal("nil diagnostics became nonnil")
	}
	if got := copyDocumentDiagnostics(p, core.SyntaxErrors{}); got == nil || len(got) != 0 {
		t.Fatal("empty diagnostics lost their shape")
	}
	src := core.SyntaxErrors{
		nil,
		{Expected: []string{}, Messages: nil},
		{Expected: []string{"token"}, Messages: []string{"original"}},
	}
	got := copyDocumentDiagnostics(p, src)
	if got[0] != nil || got[1] == src[1] || got[1].Expected == nil || got[1].Messages != nil || got[2] == src[2] {
		t.Fatal("diagnostic identity or nil/empty shape was not preserved")
	}
	got[2].Expected[0], got[2].Messages[0] = "mutated", "mutated"
	got[1] = nil
	if src[1] == nil || src[2].Expected[0] != "token" || src[2].Messages[0] != "original" {
		t.Fatal("publication shares mutable source containers")
	}
}
