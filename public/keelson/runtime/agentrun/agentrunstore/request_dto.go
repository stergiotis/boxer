package agentrunstore

// RequestMeta is what a request entry records beside its key (ADR-0325
// §SD1, ledger): the request's digest and its size.
type RequestMeta struct {
	_        struct{} `kind:"agentrunRequest"`
	ID       string   `lw:",id"`
	Digest   string   `lw:"agentrunRequestDigest,stringArray,unit"`
	Messages uint32   `lw:"agentrunRequestMessages,u32Array,unit"`
	Tools    uint32   `lw:"agentrunRequestTools,u32Array,unit"`
	Round    uint32   `lw:"agentrunRequestRound,u32Array,unit"`
	Label    string   `lw:"agentrunRequestLabel,symbol"`
}
