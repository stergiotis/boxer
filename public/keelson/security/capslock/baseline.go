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
// Four of the six entries are CAPABILITY_READ_SYSTEM_STATE incurred by os.Getwd
// or os.Getpid. They are grouped here because they are one problem, not four:
// ADR-0026 §SD10 maps READ_SYSTEM_STATE onto `sysmetrics.*` subjects, a row
// written for imztop reading system metrics over the bus. capslock's
// READ_SYSTEM_STATE is wider than that — it also covers ambient process facts
// (os.Getwd, os.Getpid, os.Environ, os.Executable). Declaring a `sysmetrics.*`
// subject in order to call os.Getwd would be nonsense, so these four are not
// apps failing to declare a capability; they are the mapping row being too
// coarse to separate "read the machine's metrics" from "ask the runtime where I
// am". Splitting that row is the next §SD10 decision, and it is what should
// retire these entries — not four manifest edits.
//
// The remaining two are real filesystem access from app code.
var baseline = map[string][]string{
	"github.com/stergiotis/boxer/apps/adrboard": {
		"CAPABILITY_FILES",
		"CAPABILITY_READ_SYSTEM_STATE",
	},
	"github.com/stergiotis/boxer/apps/godepview": {
		"CAPABILITY_READ_SYSTEM_STATE",
	},
	"github.com/stergiotis/boxer/apps/play": {
		"CAPABILITY_READ_SYSTEM_STATE",
	},
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/sccmap": {
		"CAPABILITY_READ_SYSTEM_STATE",
	},
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets": {
		"CAPABILITY_FILES",
	},
}

// baselineReasons documents why each accepted entry is accepted, keyed
// "<appId> :: <capability>". Each names the call site that incurs the
// capability, so an entry can be checked rather than taken on trust.
var baselineReasons = map[string]string{
	"github.com/stergiotis/boxer/apps/adrboard :: CAPABILITY_FILES": "" +
		"adrboard.isDir -> os.Stat: resolves the ADR corpus directory by walking the " +
		"filesystem rather than going through the fs Powerbox (§SD7). adrboard declares " +
		"no Caps at all, so this is the app's gap, not the table's: it should either " +
		"declare an fs.* subject or read the corpus through fsbroker.",
	"github.com/stergiotis/boxer/apps/adrboard :: CAPABILITY_READ_SYSTEM_STATE": "" +
		"adrboard.resolveCorpus -> os.Getwd: locates the repository root relative to the " +
		"process working directory. See the READ_SYSTEM_STATE note above.",
	"github.com/stergiotis/boxer/apps/godepview :: CAPABILITY_READ_SYSTEM_STATE": "" +
		"godepview.resolveCollectorConfig -> os.Getwd: resolves the module root to analyse. " +
		"See the READ_SYSTEM_STATE note above.",
	"github.com/stergiotis/boxer/apps/play :: CAPABILITY_READ_SYSTEM_STATE": "" +
		"play.newExecOptions -> os.Getpid: tags query runs with the process id. play's FILES " +
		"and NETWORK are already justified by its fsbroker and ch.* subjects and raise no " +
		"finding. See the READ_SYSTEM_STATE note above.",
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/sccmap :: CAPABILITY_READ_SYSTEM_STATE": "" +
		"sccmap.scanScc -> os.Getwd: defaults the scan root to the working directory. " +
		"See the READ_SYSTEM_STATE note above.",
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
