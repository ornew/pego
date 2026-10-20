//go:build ignore

// The generator splices this file into parsers generated without
// observability, in place of trace.go and profile.go. Such a parser has no
// options that observe a parse, so p.Observation is always nil and the hooks
// below do nothing.

package genrt

type observeOptions struct{}

// Of its fields and methods, Enter, Evaluated, Exit, Skipped are for generated parsers; not a stable API.
type observation struct{}

type tracePanic struct{ value any }

func newObservation(*observeOptions) *observation { return nil }

func (o *observation) Enter(string, int, int)    {}
func (o *observation) Exit(bool, int, int, bool) {}
func (o *observation) Evaluated()                {}
func (o *observation) memoHit()                  {}
func (o *observation) dropDiscarded(int)         {}
func (o *observation) release()                  {}
