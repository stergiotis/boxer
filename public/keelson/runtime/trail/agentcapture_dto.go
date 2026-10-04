package trail

// AgentCapture is one window capture (ADR-0281 §SD6): what the policy
// enforcement point decided and what it handed out. The coordinator window
// is the row's [Origin], the task and the dispatcher's call its
// [Delegation], the conversation its [Conversation].
type AgentCapture struct {
	_ struct{} `kind:"agentCapture"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAgentCapture,symbol"`
	// Format is "svg" or "png"; Windows the instance keys captured.
	Format  string   `lw:"agentCaptureFormat,symbol"`
	Windows []uint64 `lw:"agentCaptureWindows,u64Array"`
	// Decision is "permit" or "deny", Policy the policy that decided, and
	// Obligations those applied, "name@version" each.
	Decision    string   `lw:"agentCaptureDecision,symbol"`
	Policy      string   `lw:"agentCapturePolicy,symbol"`
	Obligations []string `lw:"agentCaptureObligations,stringArray"`
	// SpansDigest is a digest of the stream replayed for a pixel capture,
	// Digest a digest of the bytes handed out, after every obligation, and
	// Bytes their length. Empty and zero for a capture that handed nothing
	// out.
	SpansDigest string `lw:"agentCaptureSpansDigest,stringArray,unit"`
	Digest      string `lw:"agentCaptureDigest,stringArray,unit"`
	Bytes       uint64 `lw:"agentCaptureBytes,u64Array,unit"`
	// Phase is the capture's final phase; Reason its reason, one element
	// when there is one.
	Phase  string   `lw:"agentCapturePhase,symbol"`
	Reason []string `lw:"agentCaptureReason,stringArray"`
	// Confined says the capture carried confined content (ADR-0145).
	Confined bool `lw:"agentCaptureConfined,bool"`
}
