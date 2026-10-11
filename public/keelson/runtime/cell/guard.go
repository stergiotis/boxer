package cell

import (
	"encoding/json/v2"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/harness"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ApprovalTierE is the approval path for an exit above a sink's clearance
// (ADR-0312 §SD3).
type ApprovalTierE uint8

const (
	// ApprovalTierFree: the exit needs no approval. Above a clearance it
	// opens nothing: the clearance is a ceiling, and a mapping to free
	// there is a declaration error the guard refuses.
	ApprovalTierFree ApprovalTierE = iota
	// ApprovalTierPerson: the person approves with a recorded justification.
	ApprovalTierPerson
	// ApprovalTierOrganisation: an organisation approver approves.
	ApprovalTierOrganisation
	// ApprovalTierNever: the exit is refused whoever asks.
	ApprovalTierNever
)

var approvalNames = []string{"free", "person", "organisation", "never"}

func (inst ApprovalTierE) String() (s string) {
	if int(inst) < len(approvalNames) {
		return approvalNames[inst]
	}
	return "unknown"
}

// ParseApprovalTier is the tier a name spells.
func ParseApprovalTier(s string) (t ApprovalTierE, ok bool) {
	for i, n := range approvalNames {
		if strings.EqualFold(n, strings.TrimSpace(s)) {
			return ApprovalTierE(i), true
		}
	}
	return
}

// ApprovalPolicy maps a payload's tier to the approval path an exit above
// the sink's clearance follows.
type ApprovalPolicy map[agentrun.TierE]ApprovalTierE

// DefaultApprovalsSpec is the shipped mapping.
const DefaultApprovalsSpec = "CLEAR=free,GREEN=free,AMBER=person,AMBER+STRICT=organisation,RED=never"

// DefaultApprovalPolicy is the shipped mapping as a policy.
func DefaultApprovalPolicy() (p ApprovalPolicy) {
	p, _ = ParseApprovalPolicy(DefaultApprovalsSpec)
	return
}

// ParseApprovalPolicy reads "TIER=path" pairs; a tier not named maps to
// never, since a policy that forgets a tier must not open it.
func ParseApprovalPolicy(s string) (p ApprovalPolicy, err error) {
	p = ApprovalPolicy{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tierName, pathName, found := strings.Cut(part, "=")
		if !found {
			err = eb.Build().Str("pair", part).Errorf("an approval pair is TIER=path")
			return
		}
		tier, ok := agentrun.ParseTier(strings.TrimSpace(tierName))
		if !ok {
			err = eb.Build().Str("tier", tierName).Errorf("no such TLP tier")
			return
		}
		path, ok := ParseApprovalTier(pathName)
		if !ok {
			err = eb.Build().Str("path", pathName).Errorf("no such approval path")
			return
		}
		p[tier] = path
	}
	return
}

// Path is the approval path for a tier; a tier the policy does not name
// is never.
func (inst ApprovalPolicy) Path(t agentrun.TierE) ApprovalTierE {
	if p, ok := inst[t]; ok {
		return p
	}
	return ApprovalTierNever
}

// Decide is the sink's guard (ADR-0312 §SD2, law L14): the clearance is a
// ceiling. A payload at or below it passes. Above it the approval path
// for the payload's tier decides: the person's path asks, and the exit
// goes only under an approval the sink verifies; every other path denies
// — never by policy, free because a free mapping above the ceiling opens
// nothing, organisation because no organisation approver is wired.
//
// deferred: the organisation path (ADR-0312 §SD3) needs an approver
// outside the host; until one is wired it denies, which is stricter than
// the ADR and recorded here.
//
// The decision is one of the harness's three (ADR-0310 §SD3); the guard
// only refuses or asks, it never widens what a caller may do.
func Decide(label agentrun.Label, sink Sink, policy ApprovalPolicy) (d harness.DecisionE, tier ApprovalTierE, reason string) {
	if label.Tier <= sink.Clearance {
		return harness.DecisionAllow, ApprovalTierFree, ""
	}
	where := label.Tier.String() + " content leaves by " + sink.Kind.String() + " " + sink.Edge + " (cleared " + sink.Clearance.String() + ")"
	switch tier = policy.Path(label.Tier); tier {
	case ApprovalTierFree:
		return harness.DecisionDeny, tier, where + " only below the clearance: the policy maps " + label.Tier.String() + " to free, which opens nothing above it"
	case ApprovalTierPerson:
		return harness.DecisionAsk, tier, where + " only with the person's recorded justification"
	case ApprovalTierOrganisation:
		return harness.DecisionDeny, tier, where + " only with an organisation approver's decision, and no organisation path is wired"
	}
	return harness.DecisionDeny, ApprovalTierNever, label.Tier.String() + " content never leaves by " + sink.Kind.String() + " " + sink.Edge + " (cleared " + sink.Clearance.String() + ")"
}

// Approval is an exit above a channel's free tier that was approved: the
// body of the journal's approval entry (ADR-0308 §SD2, ADR-0312 §SD3),
// written before the exit leaves.
type Approval struct {
	// Channel is the sink kind and Edge the edge the exit crosses.
	Channel string `json:"channel"`
	Edge    string `json:"edge,omitempty"`
	// Tier is the payload's tier; Path the approval path taken.
	Tier string `json:"tier"`
	Path string `json:"path"`
	// Approver is who decided, as AgentGrant.DecidedBy names a decider:
	// "person" or the organisation approver's identity.
	Approver      string    `json:"approver"`
	Justification string    `json:"justification,omitempty"`
	At            time.Time `json:"at"`
	// Digest is the payload's content digest when the approver had one;
	// informational. The sink binds the approval to the request it saw
	// by its own digest, and the laws join on the entry's key.
	Digest string `json:"digest,omitempty"`
}

// Body is the approval as an entry body.
func (inst Approval) Body() (body string, err error) {
	b, err := json.Marshal(inst, json.Deterministic(true))
	if err != nil {
		err = eh.Errorf("encode approval: %w", err)
		return
	}
	return string(b), nil
}

// ParseApproval reads an approval entry's body.
func ParseApproval(body string) (a Approval, err error) {
	if err = json.Unmarshal([]byte(body), &a); err != nil {
		err = eh.Errorf("parse approval: %w", err)
	}
	return
}

// ApprovalKey is the key a resent exit carries for its approval entry
// (ADR-0312 §SD3): the run and the entry's ordinal, "<run>#<ordinal>",
// what the sink verifies against the journal and what law L33 joins on.
func ApprovalKey(run string, ordinal uint32) string {
	return run + "#" + strconv.FormatUint(uint64(ordinal), 10)
}

// ParseApprovalKey is ApprovalKey's inverse; ok is false for anything
// else.
func ParseApprovalKey(key string) (run string, ordinal uint32, ok bool) {
	i := strings.LastIndexByte(key, '#')
	if i <= 0 || i == len(key)-1 {
		return
	}
	n, err := strconv.ParseUint(key[i+1:], 10, 32)
	if err != nil || n == 0 {
		return
	}
	return key[:i], uint32(n), true
}
