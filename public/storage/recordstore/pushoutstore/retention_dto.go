package pushoutstore

// Retention is one retention-ledger row (key "retention"): since
// ADR-0221 a DELTA — four aligned arrays, one element per entry — the
// node's introducing patch hash (hex), its index within that patch, the
// first-observed-deleted unix nanos, and the op (RetentionOp*). The
// ledger is the fold of the key's rows after its last state-view
// tombstone; a compaction writes that tombstone and one full row of
// upserts. See envelope_dto.go for the component-file layout rationale.
type Retention struct {
	_       struct{} `kind:"pushoutRetention"`
	ID      string   `lw:",id"`
	Hashes  []string `lw:"pushoutRetHash,retHash"`
	Indices []uint64 `lw:"pushoutRetIdx,retIndex"`
	Times   []int64  `lw:"pushoutRetTime,retTime"`
	Ops     []uint16 `lw:"pushoutRetOp,retOp"`
}
