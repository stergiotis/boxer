package agentrunstore

import "time"

// ApprovalDecision is an approval's decision and approver (ADR-0325 §SD1,
// ledger): the channel and edge the exit crosses, the payload's tier, the
// path taken, who decided and when.
type ApprovalDecision struct {
	_        struct{}  `kind:"agentrunApprovalDecision"`
	ID       uint64    `lw:",id"`
	Channel  string    `lw:"agentrunApprovalChannel,symbol"`
	Edge     string    `lw:"agentrunApprovalEdge,symbol"`
	Tier     string    `lw:"agentrunApprovalTier,symbol"`
	Path     string    `lw:"agentrunApprovalPath,symbol"`
	Approver string    `lw:"agentrunApprovalApprover,stringArray,unit"`
	At       time.Time `lw:"agentrunApprovalAt,timeArray,unit"`
}
