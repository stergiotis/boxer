package pushoutstore

// Snapshot is one persisted acceleration point (key "snapshot",
// latest-wins): the applied prefix as hex hashes plus the opaque pushoutgraph
// bytes. See envelope_dto.go for the component-file layout rationale.
type Snapshot struct {
	_            struct{} `kind:"pushoutSnapshot"`
	ID           string   `lw:",id"`
	Applied      []string `lw:"pushoutApplied,snapApplied"`
	PushoutGraph []byte   `lw:"pushoutGraph,snapPushoutGraph"`
}
