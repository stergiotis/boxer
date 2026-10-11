package agentrunstore

// CompactIntent is a summariser's intent (ADR-0325 §SD1, ledger): the cut,
// the previous cut and how many entries the summary spans.
type CompactIntent struct {
	_    struct{} `kind:"agentrunCompact"`
	ID   uint64   `lw:",id"`
	Cut  uint32   `lw:"agentrunCompactCut,u32Array,unit"`
	From uint32   `lw:"agentrunCompactFrom,u32Array,unit"`
	Span uint32   `lw:"agentrunCompactSpan,u32Array,unit"`
}
