package agentrunstore

// ProfileRecord is a profile entry's record (ADR-0325 §SD1, ledger): the
// fingerprint of the harness profile the run is pinned to.
type ProfileRecord struct {
	_           struct{} `kind:"agentrunProfile"`
	ID          string   `lw:",id"`
	Fingerprint string   `lw:"agentrunProfileFingerprint,stringArray,unit"`
}
