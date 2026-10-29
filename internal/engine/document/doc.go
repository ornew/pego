// Package document is the incremental Document of the engine: it holds an input, applies edits
// and edit batches to it, and parses again reusing the results that the edits did not touch. It
// also owns what a Document publishes: immutable snapshots of the tree, change reports between
// versions, diagnostics and the node cursor.
//
// The package depends only on core among the engine packages. A Document parses through a
// Runner, which the engine's Program implements, so it never sees the backends; New takes one.
// Package engine re-exports the types of this package, and the public API reaches them only
// through it.
package document
