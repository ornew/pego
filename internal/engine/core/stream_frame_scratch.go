//go:build !pego_reference_stream_frames

package core

// Build-time selection keeps the reference control out of matching loops.
const StreamFrameScratch = true
