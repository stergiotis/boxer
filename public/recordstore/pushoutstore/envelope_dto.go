package pushoutstore

// Envelope, LogEntry (logentry_dto.go), Snapshot (snapshot_dto.go) and
// Retention (retention_dto.go) are the pushout persistence components:
// flat lw:-tagged DTOs, one kind per source file (marshallgen.ParsePlan
// reads a single kind per input), each owning distinct sections so the
// per-kind membership ids cannot collide in storage.
//
// Envelope is one immutable, content-addressed patch envelope: Framed
// carries the PXE1-framed bytes verbatim; the row key is "env/<hex hash>".
type Envelope struct {
	_      struct{} `kind:"pushoutEnvelope"`
	ID     string   `lw:",id"`
	Framed []byte   `lw:"pushoutFramed,envBlob"`
}
