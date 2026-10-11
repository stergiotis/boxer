package agentrunstore

// ApprovalContent is the part of an approval that is content (ADR-0325
// §SD1, vault): the justification given and the payload's content digest.
type ApprovalContent struct {
	_             struct{} `kind:"agentrunApprovalContent"`
	ID            string   `lw:",id"`
	Justification string   `lw:"agentrunApprovalJustification,textArray,unit"`
	Digest        string   `lw:"agentrunApprovalDigest,stringArray,unit"`
}
