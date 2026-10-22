// Package corporatest finds the fetched third-party corpora for tests. It never uses the network.
package corporatest

import (
	"testing"

	"github.com/ornew/pego/internal/corpora"
)

// FetchCommand is what a skipped test tells the reader to run.
const FetchCommand = "go run ./hack/corpora fetch"

// Dir returns the directory of the fetched corpus name. If it was not fetched, the test is skipped with the command
// that fetches it. A name that the manifest does not list is a failure.
func Dir(t testing.TB, name string) string {
	t.Helper()
	s, ok := corpora.Lookup(name)
	if !ok {
		t.Fatalf("corpus %q is not in internal/corpora/manifest.json", name)
	}
	dir, ok := corpora.Dir(s)
	if !ok {
		t.Skipf("corpus %q is not fetched; run `%s %s` from the root of the repository", name, FetchCommand, name)
	}
	return dir
}
