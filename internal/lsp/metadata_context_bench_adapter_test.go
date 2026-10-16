package lsp

import "context"

// Kept separate so the historical comparison can use the parent's entry points.
func metadataBenchmarkCollect(ctx context.Context, a *analysis) error {
	return runMetadataContext(ctx, func(c *renderControl) error {
		a.indexDeclarationStartsControlled(c)
		a.collectControlled(c)
		return nil
	})
}
