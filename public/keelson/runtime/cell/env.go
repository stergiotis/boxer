package cell

import (
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// The policy data (ADR-0312 §SD1, §SD3), declared per ADR-0009. Tier names
// are TLP 2.0 as agentrun spells them; the variables are read
// case-insensitively and fail closed (FromEnv).

var tierNames = func() (names []string) {
	for t := agentrun.TierClear; t <= agentrun.TierRed; t++ {
		names = append(names, t.String())
	}
	return
}()

// RouteClearanceEnv is the clearance of a remote model route: the highest
// tier a request may carry to a provider outside the cell.
var RouteClearanceEnv = env.NewCategorialString(env.Spec{
	Name:        "BOXER_CELL_ROUTE_CLEARANCE",
	Default:     "GREEN",
	Description: "the TLP clearance of a remote model route (ADR-0312 §SD1): the highest tier a request may carry to a model host outside the cell; a loopback or trusted host is inside the cell and has none",
	Category:    env.CategoryE("boxer-cell"),
}, tierNames)

// EgressClearanceEnv is the clearance of an HTTP egress destination outside
// the cell.
var EgressClearanceEnv = env.NewCategorialString(env.Spec{
	Name:        "BOXER_CELL_EGRESS_CLEARANCE",
	Default:     "GREEN",
	Description: "the TLP clearance of an HTTP egress destination outside the cell (ADR-0312 §SD1): the highest tier the outbound half of a fetch may carry",
	Category:    env.CategoryE("boxer-cell"),
}, tierNames)

// ClipboardClearanceEnv is the clearance of the clipboard out of boxer and
// ExportClearanceEnv that of file export and printing.
var ClipboardClearanceEnv = env.NewCategorialString(env.Spec{
	Name:        "BOXER_CELL_CLIPBOARD_CLEARANCE",
	Default:     "GREEN",
	Description: "the TLP clearance of the clipboard out of boxer (ADR-0312 §SD1, ADR-0317): the highest tier that copies as plaintext without an approval",
	Category:    env.CategoryE("boxer-cell"),
}, tierNames)

var ExportClearanceEnv = env.NewCategorialString(env.Spec{
	Name:        "BOXER_CELL_EXPORT_CLEARANCE",
	Default:     "GREEN",
	Description: "the TLP clearance of file export and printing (ADR-0312 §SD1): the highest tier that leaves as a file without an approval",
	Category:    env.CategoryE("boxer-cell"),
}, tierNames)

// PeersEnv lists peer cells of the organisation with a clearance each,
// "name=TIER" comma-separated.
var PeersEnv = env.NewString(env.Spec{
	Name:        "BOXER_CELL_PEERS",
	Default:     "",
	Description: "peer cells of the same organisation and the TLP clearance of what is sent to each, as name=TIER pairs separated by commas (ADR-0307 §SD2, ADR-0312 §SD1)",
	Category:    env.CategoryE("boxer-cell"),
})

// ConfinedTierEnv is the tier the compile-date `confined` bit maps to.
var ConfinedTierEnv = env.NewCategorialString(env.Spec{
	Name:        "BOXER_CELL_CONFINED_TIER",
	Default:     "AMBER",
	Description: "the TLP tier content labelled confined (derived from sealed data) carries at the sinks (ADR-0311 §SD1): a deployment default, not fixed by boxer; a value that names no tier fails closed to RED, where a clearance that names none fails to CLEAR",
	Category:    env.CategoryE("boxer-cell"),
}, tierNames)

// ApprovalsEnv is the approval path per tier for an exit above a
// channel's free tier.
var ApprovalsEnv = env.NewString(env.Spec{
	Name:        "BOXER_CELL_APPROVALS",
	Default:     DefaultApprovalsSpec,
	Description: "the approval path for an exit above a sink's clearance, per TLP tier, as TIER=path pairs separated by commas; a path is free, person (a recorded justification), organisation (an approver) or never (ADR-0312 §SD3)",
	Category:    env.CategoryE("boxer-cell"),
})

// CanariesEnv plants the exact canaries (ADR-0312 §SD6).
var CanariesEnv = env.NewString(env.Spec{
	Name:        "BOXER_CELL_CANARIES",
	Default:     "",
	Description: "planted canary tokens (ADR-0312 §SD6) as scope=token@TIER triples separated by commas: an outbound payload that contains a token of a tier above the sink's clearance is refused; a triple without a tier is RED",
	Category:    env.CategoryE("boxer-cell"),
})

// ManagedEnv says the host is managed by the organisation: exits above
// policy are prevented; on a host the person administers they are
// recorded (ADR-0307 §SD6).
var ManagedEnv = env.NewBool(env.Spec{
	Name:        "BOXER_CELL_MANAGED",
	Default:     "false",
	Description: "the host is managed by the organisation (ADR-0307 §SD6): exits above the policy on relay channels are prevented, where on a host the person administers they are recorded and not prevented",
	Category:    env.CategoryE("boxer-cell"),
})
