package agentrunstore

// VerdictRecord is a verdict entry's record (ADR-0340 §SD4,
// ledger): the assertion's form and outcome, the keys of the calls it
// rests on, the result it was checked against, the digests of its
// statement and of the result's, the endpoint the check ran on, and the
// reason when it did not hold or did not run. The assertion and the
// observed value are the entry's content, in the vault.
type VerdictRecord struct {
	_        struct{} `kind:"agentrunVerdict"`
	ID       uint64   `lw:",id"`
	Form     string   `lw:"agentrunVerdictForm,symbol"`
	Outcome  string   `lw:"agentrunVerdictOutcome,symbol"`
	RestsOn  []string `lw:"agentrunVerdictRestsOn,stringArray"`
	Result   uint64   `lw:"agentrunVerdictResult,u64Array,unit"`
	Digest   string   `lw:"agentrunVerdictDigest,stringArray,unit"`
	Source   string   `lw:"agentrunVerdictSource,stringArray,unit"`
	Endpoint string   `lw:"agentrunVerdictEndpoint,stringArray,unit"`
	Reason   string   `lw:"agentrunVerdictReason,stringArray,unit"`
}
