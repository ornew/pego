package gentest

// LRBindingSource re-enters a left-recursive rule with another variable environment: the
// binding before the left call changes k, which the rule reads. Only compiling it with
// leftRecursionBindings accepts it. PlainLRSource is the same rule without the binding.
const (
	LRBindingSource = "var k int\n@start\nrule main = R $$\nrule R = {k := k + 1} R \"x\" / {k < 1000} \"x\""
	PlainLRSource   = "var k int\n@start\nrule main = R $$\nrule R = R \"x\" / {k < 1000} \"x\""
)

// LRBindingInputs are the inputs of LRBindingSource and PlainLRSource.
var LRBindingInputs = []string{"x", "xxx", "xxxxxxxxxx", "", "y"}
