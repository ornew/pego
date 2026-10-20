//go:build ignore

// The generator splices this file into parsers generated without
// observability, in place of trace.go and profile.go. Such a parser has no
// options that observe a parse, so p.observation is always nil and the hooks
// below do nothing.

package genrt

type observeOptions struct{}

type observation struct{}

type tracePanic struct{ value any }

func newObservation(*observeOptions) *observation { return nil }

func (o *observation) enter(string, int, int)    {}
func (o *observation) exit(bool, int, int, bool) {}
func (o *observation) evaluated()                {}
func (o *observation) memoHit()                  {}
func (o *observation) dropDiscarded(int)         {}
func (o *observation) release()                  {}
