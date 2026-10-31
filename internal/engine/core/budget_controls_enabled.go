//go:build !zp_budget_controls_reference

package core

// BudgetControlsEnabled keeps invocation controls removable for measurements.
// The reference build still includes the new state layout; an exact parent is
// also required to assess layout and GC effects.
const BudgetControlsEnabled = true
