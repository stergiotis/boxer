package agentrunstore

// Endorsement is an endorse entry's record (ADR-0325 §SD1, vault: it names
// content by digest, ADR-0320 §SD6): the digest of what was shown, the
// rendering it was shown under, the act classes it covers, the previous
// endorsement's digest and what changed since.
type Endorsement struct {
	_        struct{} `kind:"agentrunEndorse"`
	ID       string   `lw:",id"`
	Digest   string   `lw:"agentrunEndorseDigest,stringArray,unit"`
	Epoch    string   `lw:"agentrunEndorseEpoch,stringArray,unit"`
	Classes  []string `lw:"agentrunEndorseClasses,symbolArray"`
	Previous string   `lw:"agentrunEndorsePrevious,stringArray,unit"`
	Diff     string   `lw:"agentrunEndorseDiff,textArray,unit"`
}
