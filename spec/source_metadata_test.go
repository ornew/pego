package spec_test

import (
	"reflect"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/sourceast"
)

// Package ownership is compared at the public metadata boundary. The adapter
// receives the grammar-constructed ownership, never the raw comment list.
func TestCanonicalPackageMetadataParity(t *testing.T) {
	p, _ := sourceGrammar(t)
	for _, src := range []string{
		"package p",
		"// orphan\n\n// package\npackage p // trailing\n// detached\n\n// rule\nrule main = \"x\"",
		"/**/\n\n/* package */ package p\nrule main = \"x\"",
		"// 😀\r\npackage p /* previous */\r\nrule main = \"x\"",
		"package p\n/* previous\n * paragraph */\n\nrule main = \"x\"",
		"package p; // trailing\nrule main = \"x\"",
		"package p /* detached */ rule main = \"x\"",
		"package p // previous\n// unowned\n",
		"package p\n\n// unowned\n",
	} {
		t.Run(src, func(t *testing.T) {
			manual, manualErr := pego.ParseGrammar(src)
			for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
				n, err := p.Parse(src, pego.WithBackend(backend))
				if (err == nil) != (manualErr == nil) {
					t.Fatalf("acceptance differs: manual=%v canonical=%v", manualErr, err)
				}
				if err != nil {
					continue
				}
				pkg := sourceField(t, n, "Package")
				metadata := sourceField(t, pkg, "Source")
				span := sourceField(t, metadata, "Span")
				var detached []string
				for _, comment := range sourceField(t, metadata, "Detached").Children {
					detached = append(detached, comment.Field("Text").(string))
				}
				m := sourceast.Metadata{Start: int(span.Start), End: int(span.End), Leading: metadata.Field("Leading").(string), Trailing: metadata.Field("Trailing").(string), Detached: detached}
				adapted := &grammar.Grammar{}
				if err := sourceast.NewPositions(src).PackageClause(adapted, sourceField(t, pkg, "Name").Text, m); err != nil {
					t.Fatal(err)
				}
				if adapted.Package != manual.Package || !reflect.DeepEqual(adapted.PackageSource, manual.PackageSource) {
					t.Fatalf("package metadata differs: canonical=%+v manual=%+v", adapted.PackageSource, manual.PackageSource)
				}
			}
		})
	}
}
