package language_test

import (
	"reflect"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/spec/language/internal/sourceast"
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
			manual, manualErr := zp.ParseGrammar(src)
			for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
				n, err := p.Parse(src, zp.WithBackend(backend))
				if (err == nil) != (manualErr == nil) {
					t.Fatalf("acceptance differs: manual=%v canonical=%v", manualErr, err)
				}
				if err != nil {
					continue
				}
				pkg := sourceField(t, n, "Pkg")
				metadata := sourceField(t, pkg, "Source")
				span := sourceField(t, metadata, "Span")
				var detached []string
				for _, comment := range elems(sourceField(t, metadata, "Detached")) {
					detached = append(detached, comment.Field("Text").(string))
				}
				m := sourceast.Metadata{Start: int(span.Start()), End: int(span.End()), Leading: metadata.Field("Leading").(string), Trailing: stringList(metadata, "Trailing"), Detached: detached}
				adapted := &grammar.Grammar{}
				if err := sourceast.NewPositions(src).PackageClause(adapted, sourceText(t, pkg, "Name"), m); err != nil {
					t.Fatal(err)
				}
				if adapted.File.Pkg.Name != manual.File.Pkg.Name || !reflect.DeepEqual(adapted.File.Pkg.SourceInfo, manual.File.Pkg.SourceInfo) {
					t.Fatalf("package metadata differs: canonical=%+v manual=%+v", adapted.File.Pkg.SourceInfo, manual.File.Pkg.SourceInfo)
				}
			}
		})
	}
}
