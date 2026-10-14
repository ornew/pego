package pego_test

import (
	"testing"

	"github.com/ornew/pego"
)

func TestPublicDocumentCacheControls(t *testing.T) {
	p, err := pego.CompileSource(`rule main = line* $$
rule line = [a-z]+ "\n"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		d, err := p.NewDocument("ab\ncd\n", pego.WithBackend(backend))
		if err != nil {
			t.Fatal(err)
		}
		beforeVersion := d.Version()
		if _, err := d.Parse(); err != nil {
			t.Fatal(err)
		}
		if got := d.CacheStats(); got.LiveMemoEntries == 0 || got.EstimatedBytes == 0 {
			t.Fatalf("%s empty history: %+v", backend, got)
		}
		if err := d.Edit(3, 4, "x"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(); err != nil {
			t.Fatal(err)
		}
		last := d.Stats()
		if last.Reused != last.MemoHits+last.ResumedElements {
			t.Fatalf("%s inconsistent reuse counts: %+v", backend, last)
		}
		version := d.Version()
		d.ResetHistory()
		if got := d.CacheStats(); got != (pego.CacheStats{}) {
			t.Fatalf("%s reset history: %+v", backend, got)
		}
		if d.Stats() != last || d.Text() != "ab\nxd\n" || d.Version() != version || version == beforeVersion {
			t.Fatalf("%s reset changed document state", backend)
		}
		if _, err := d.Parse(); err != nil || d.CacheStats().LiveMemoEntries == 0 {
			t.Fatalf("%s history did not repopulate: %v", backend, err)
		}
	}
}
