package pushoutstore

// LogEntry is one applied-log row (key "log"): the hex hash of the patch
// applied at this position; the row's ts is the synthetic append
// sequence. See envelope_dto.go for the component-file layout rationale.
type LogEntry struct {
	_    struct{} `kind:"pushoutLogEntry"`
	ID   string   `lw:",id"`
	Hash string   `lw:"pushoutHash,logHash"`
}
