package provenance

// Provenance is the descriptor fact and the dimension.Store's D: the hostname
// and the symbolicated Go call-stack a surrogate id was minted for. The lw tags
// make it a recordstore component of the descriptor table — one symbol section
// (Host) and one symbolArray section (Stack), the shapes device Identity/Tagged
// already prove. ID is plain-bound to the envelope key: ignored on write (the
// store keys on Begin(id)), read back on Resolve.
type Provenance struct {
	_     struct{} `kind:"provenance"`
	ID    uint64   `lw:",id"`
	Host  string   `lw:"provHost,symbol"`
	Stack []string `lw:"provStack,symbolArray"`
}
