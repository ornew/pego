//go:build !pego_reference_document_vm_values

package engine

// Selection happens at build time, outside the VM matching loops.
const reuseDocumentVMValues = true
