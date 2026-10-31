//go:build zp_reference_document_vm_values

package document

// The reference build allocates a fresh VM value stack on every parse.
const reuseDocumentVMValues = false
