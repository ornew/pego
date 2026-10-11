//go:build pego_reference_document_vm_values

package engine

// The reference build allocates a fresh VM value stack on every parse.
const reuseDocumentVMValues = false
