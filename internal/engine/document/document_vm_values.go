//go:build !zp_reference_document_vm_values

package document

// Selection happens at build time, outside the VM matching loops.
const reuseDocumentVMValues = true
