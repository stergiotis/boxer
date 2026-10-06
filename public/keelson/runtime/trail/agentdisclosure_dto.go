package trail

// AgentDisclosure is one view of a screenshot's pixels a coordinator's model
// asked for (ADR-0287 §SD6): shown, declined or refused, and what decided
// it. The coordinator window is the row's [Origin], the conversation and
// turn its [Conversation], the model call that asked its [Cause], and the
// task, when one still holds the coordinator, its [Delegation]. The pixels
// reach the model on a later call of the turn, whose LlmMessage row names
// the same digest.
type AgentDisclosure struct {
	_ struct{} `kind:"agentDisclosure"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAgentDisclosure,symbol"`
	// Image is the screenshot's name in the conversation's artefact.
	Image string `lw:"agentDisclosureImage,stringArray,unit"`
	// Digest is the BLAKE3-256 of the PNG bytes, as the LlmMessage row of the
	// call that carried them names it. RootDigest is that of the capture the
	// image descends from by copies and crops — an AgentCapture row's
	// Digest — and Source how it came to be: "capture", "copy" or "crop".
	Digest     string `lw:"agentDisclosureDigest,stringArray,unit"`
	RootDigest string `lw:"agentDisclosureRootDigest,stringArray,unit"`
	Source     string `lw:"agentDisclosureSource,symbol"`
	Width      uint32 `lw:"agentDisclosureWidth,u32Array,unit"`
	Height     uint32 `lw:"agentDisclosureHeight,u32Array,unit"`
	Bytes      uint64 `lw:"agentDisclosureBytes,u64Array,unit"`
	// Level is the Pixels level in force, LocalOnly its switch.
	Level     string `lw:"agentDisclosureLevel,symbol"`
	LocalOnly bool   `lw:"agentDisclosureLocalOnly,bool"`
	// Decision is "shown", "declined" or "refused"; DecidedBy who decided:
	// "person", "consent" (one the person gave this content earlier),
	// "setting" (a level that does not ask) or "chat" (a refusal of the
	// setting or the host's cap).
	Decision  string `lw:"agentDisclosureDecision,symbol"`
	DecidedBy string `lw:"agentDisclosureDecidedBy,symbol"`
	// Endpoint names the model the pixels go to, as the coordinator states
	// it; Reason a refusal's, one element when there is one.
	Endpoint []string `lw:"agentDisclosureEndpoint,stringArray"`
	Reason   []string `lw:"agentDisclosureReason,stringArray"`
}
