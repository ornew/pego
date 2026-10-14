//go:build !pego_work_controls_reference

package engine

// workControlsEnabled keeps invocation controls removable for measurements.
// The reference build still includes the new state layout; an exact parent is
// also required to assess layout and GC effects.
const workControlsEnabled = true
