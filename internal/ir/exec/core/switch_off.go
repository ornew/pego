//go:build !zp_exec

package core

// Enabled reports whether the generators print the compositions into the runtimes they emit. It is
// the private switch of the migration to Exec IR: the build tag zp_exec sets it.
const Enabled = false
