package capslock

import (
	"fmt"
	"sort"
)

// baseline is the set of (app, capability) findings accepted as of 2026-07-15,
// with the reason each is accepted. It is the reference for `compare` mode
// (ADR-0026 §SD10 adoption phase M3): a finding in this list is reported but
// does not fail; a finding outside it is drift and fails.
//
// This is a record of what is *tolerated today*, not of what is *fine*. Each
// entry is a debt with a stated reason, and the list is expected to shrink. Two
// rules keep it honest:
//
//   - Adding an entry is a decision, not a formality: it means an app performs a
//     privileged operation its manifest does not justify, and that shipped.
//   - An entry that no longer reproduces is reported as stale and must be
//     removed, so the list cannot quietly accumulate entries that describe code
//     nobody has anymore.
//
// Keys are app ids (== the Go package path, per the l12manifestid rule).
//
// # The shape of the current debt
//
// The remaining entry is real filesystem access from app code. It is not
// dangerous; it is an app reaching past the §SD7 picker substrate to touch the
// disk directly, which is the thing §SD10 exists to make visible.
//
// This list was six entries when the gate was first enforced, and each way one
// left is worth remembering, because they are three different things:
//
//   - Four were CAPABILITY_READ_SYSTEM_STATE incurred by os.Getwd or os.Getpid,
//     retired by splitting that mapping row rather than by four manifest edits
//     — see [refineCapability] and ADR-0026's 2026-07-15 update. That is the
//     shape of a bad baseline entry: an accepted finding no app could honestly
//     act on is a defect in the table, not debt in the app.
//   - One (adrboard, stat-ing the ADR corpus) left because the app did — its
//     board is now a query against keelson.adr in play's Kanban pane
//     (ADR-0122). Debt can leave by the code leaving; that is not a fix, and
//     nothing here was made safer by it.
//   - None so far has left the way the list wants: an app declaring the subject
//     it needs, or reading through the broker.
var baseline = map[string][]string{
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets": {
		"CAPABILITY_FILES",
	},
}

// baselineReasons documents why each accepted entry is accepted, keyed
// "<appId> :: <capability>". Each names the call site that incurs the
// capability, so an entry can be checked rather than taken on trust.
var baselineReasons = map[string]string{
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets :: CAPABILITY_FILES": "" +
		"widgets.RenderLoopHandlerTestDriver -> os.MkdirAll: the screenshot TestDriver " +
		"(ADR-0057) creates its capture output directory. Harness code compiled into the " +
		"demo app rather than a capability the demo itself exercises.",
}

// CompareToBaseline splits findings into drift (findings not accepted in the
// baseline) and stale (baseline entries that no longer reproduce). Findings
// with StatusOK are ignored: they are not findings.
func CompareToBaseline(findings []Finding) (drift []Finding, stale []string) {
	accepted := make(map[string]struct{}, len(baseline))
	for appId, caps := range baseline {
		for _, c := range caps {
			accepted[baselineKey(appId, c)] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		if f.Status == StatusOK {
			continue
		}
		k := baselineKey(f.AppId, f.Cap)
		seen[k] = struct{}{}
		if _, ok := accepted[k]; !ok {
			drift = append(drift, f)
		}
	}
	for k := range accepted {
		if _, ok := seen[k]; !ok {
			stale = append(stale, k)
		}
	}
	sort.Slice(drift, func(i, j int) bool {
		if drift[i].AppId != drift[j].AppId {
			return drift[i].AppId < drift[j].AppId
		}
		return drift[i].Cap < drift[j].Cap
	})
	sort.Strings(stale)
	return
}

func baselineKey(appId string, capName string) (k string) {
	k = fmt.Sprintf("%s :: %s", appId, capName)
	return
}
