package agentrunstore

// CopyRecord is a copy entry's record (ADR-0325 §SD1, vault: it names
// content by digest; ADR-0317): what was copied, at which label, how, and
// whether it reached the clipboard.
type CopyRecord struct {
	_         struct{} `kind:"agentrunCopy"`
	ID        uint64   `lw:",id"`
	Digest    string   `lw:"agentrunCopyDigest,stringArray,unit"`
	Level     uint8    `lw:"agentrunCopyLevel,u8Array,unit"`
	Tier      uint8    `lw:"agentrunCopyTier,u8Array,unit"`
	Source    string   `lw:"agentrunCopySource,symbol"`
	Handle    string   `lw:"agentrunCopyHandle,stringArray,unit"`
	Plaintext bool     `lw:"agentrunCopyPlaintext,bool"`
	Recorded  bool     `lw:"agentrunCopyRecorded,bool"`
	Approval  string   `lw:"agentrunCopyApproval,stringArray,unit"`
	Raw       bool     `lw:"agentrunCopyRaw,bool"`
	Refused   bool     `lw:"agentrunCopyRefused,bool"`
	Reason    string   `lw:"agentrunCopyReason,textArray,unit"`
	Bytes     uint32   `lw:"agentrunCopyBytes,u32Array,unit"`
}
