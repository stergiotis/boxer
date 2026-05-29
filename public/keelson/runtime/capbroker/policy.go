//go:build llm_generated_opus47

package capbroker

// GrantPolicyI is the decision interface — the broker delegates yes/no on
// each request to a policy. M2.3 ships AutoApprovePolicy, DenyAllPolicy,
// and FuncPolicy (test adapter). The M3 dock host will provide a dialog
// policy that prompts the user via an egui::Window in the overlay layer
// (ADR-0026 §SD7).
type GrantPolicyI interface {
	Decide(req GrantRequest) (d GrantDecision)
}

// GrantDecision is the policy's verdict. Reason is shown in the audit log
// (post-M2.5) and reflected in GrantReply.Reason.
type GrantDecision struct {
	Granted bool
	Reason  string
}

// AutoApprovePolicy approves every request. Useful for development and for
// hosts that have already gated grants via other means.
type AutoApprovePolicy struct{}

var _ GrantPolicyI = (*AutoApprovePolicy)(nil)

func (inst *AutoApprovePolicy) Decide(req GrantRequest) (d GrantDecision) {
	d = GrantDecision{Granted: true, Reason: "auto-approve policy"}
	return
}

// DenyAllPolicy rejects every request. Default until the host explicitly
// configures a permissive policy.
type DenyAllPolicy struct{}

var _ GrantPolicyI = (*DenyAllPolicy)(nil)

func (inst *DenyAllPolicy) Decide(req GrantRequest) (d GrantDecision) {
	d = GrantDecision{Granted: false, Reason: "deny-all policy"}
	return
}

// FuncPolicy adapts a function as a policy. Tests use this to inject
// per-request decisions; production hosts wire a real policy implementation.
type FuncPolicy func(req GrantRequest) (d GrantDecision)

var _ GrantPolicyI = (FuncPolicy)(nil)

func (inst FuncPolicy) Decide(req GrantRequest) (d GrantDecision) {
	d = inst(req)
	return
}
