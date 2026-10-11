//go:build !pego_reference_stream_frames

package engine

// Build-time selection keeps the reference control out of matching loops.
const streamFrameScratch = true
