package pushoutstore

// Retention is the whole replica-local retention ledger in one row (key
// "retention", latest-wins): three aligned arrays, one element per
// entry — the node's introducing patch hash (hex), its index within that
// patch, and the first-observed-deleted unix nanos. See envelope_dto.go
// for the component-file layout rationale.
type Retention struct {
	_       struct{} `kind:"pushoutRetention"`
	ID      string   `lw:",id"`
	Hashes  []string `lw:"pushoutRetHash,retHash"`
	Indices []uint64 `lw:"pushoutRetIdx,retIndex"`
	Times   []int64  `lw:"pushoutRetTime,retTime"`
}
