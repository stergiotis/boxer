package capslock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cpb "github.com/google/capslock/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// --- mapping table -------------------------------------------------------

func TestCapRequirements_FilesNeedsFsCap(t *testing.T) {
	prefixes, hardFail, alwaysOK := capRequirements("CAPABILITY_FILES")
	assert.Equal(t, []string{"fs."}, prefixes)
	assert.False(t, hardFail)
	assert.False(t, alwaysOK)
}

func TestCapRequirements_NetworkAcceptsMultiplePrefixes(t *testing.T) {
	prefixes, _, _ := capRequirements("CAPABILITY_NETWORK")
	assert.ElementsMatch(t, []string{"nats.", "ch.", "kafka.", "net."}, prefixes)
}

func TestCapRequirements_RuntimeAlwaysAllowed(t *testing.T) {
	_, hardFail, alwaysOK := capRequirements("CAPABILITY_RUNTIME")
	assert.False(t, hardFail)
	assert.True(t, alwaysOK)
}

func TestCapRequirements_OsExecHardFail(t *testing.T) {
	for _, capName := range []string{
		"CAPABILITY_OPERATING_SYSTEM",
		"CAPABILITY_EXEC",
		"CAPABILITY_SYSTEM_CALLS",
		"CAPABILITY_ARBITRARY_EXECUTION",
		"CAPABILITY_CGO",
		"CAPABILITY_UNSAFE_POINTER",
		"CAPABILITY_REFLECT",
		"CAPABILITY_MODIFY_SYSTEM_STATE",
	} {
		_, hardFail, _ := capRequirements(capName)
		assert.True(t, hardFail, "cap=%s should be hard fail", capName)
	}
}

func TestCapRequirements_Unknown_HardFail(t *testing.T) {
	_, hardFail, _ := capRequirements("CAPABILITY_NEW_THING")
	assert.True(t, hardFail)
}

// The classifier is configured never to emit UNANALYZED, so it can only reach
// the table as an unknown. Pin that it lands on the defensive default rather
// than being silently permitted.
func TestCapRequirements_Unanalyzed_FallsToDefault(t *testing.T) {
	_, hardFail, alwaysOK := capRequirements("CAPABILITY_UNANALYZED")
	assert.True(t, hardFail)
	assert.False(t, alwaysOK)
}

// --- capability-name normalisation ---------------------------------------

// The library reports the classifier's bare category string; the JSON output
// reports the proto enum. The mapping table is written in the enum vocabulary,
// so an unnormalised name would fall to capRequirements' hard-fail default and
// damn every app.
func TestNormaliseCapability(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"FILES", "CAPABILITY_FILES"},
		{"NETWORK", "CAPABILITY_NETWORK"},
		{"UNANALYZED", "CAPABILITY_UNANALYZED"},
		{"CAPABILITY_FILES", "CAPABILITY_FILES"},
		{"NETWORK/dial", "CAPABILITY_NETWORK"},
		{"", ""},
	} {
		assert.Equal(t, tc.want, normaliseCapability(tc.in), "in=%q", tc.in)
	}
}

func TestNormaliseCapability_FeedsTheTable(t *testing.T) {
	// The regression this pins: the raw library name must not reach the table.
	_, hardFail, _ := capRequirements("FILES")
	assert.True(t, hardFail, "raw library name is unknown to the table (hence normalisation)")
	prefixes, hardFail, _ := capRequirements(normaliseCapability("FILES"))
	assert.False(t, hardFail)
	assert.Equal(t, []string{"fs."}, prefixes)
}

// --- own-capability aggregation ------------------------------------------

func fnPath(pkgs ...string) (out []*cpb.Function) {
	for _, p := range pkgs {
		pkg := p
		out = append(out, &cpb.Function{Package: &pkg})
	}
	return
}

func rec(capName string, ctype cpb.CapabilityType, path ...string) (ci *cpb.CapabilityInfo) {
	n := capName
	t := ctype
	ci = &cpb.CapabilityInfo{CapabilityName: &n, CapabilityType: &t, Path: fnPath(path...)}
	return
}

func cil(cis ...*cpb.CapabilityInfo) (out *cpb.CapabilityInfoList) {
	out = &cpb.CapabilityInfoList{CapabilityInfo: cis}
	return
}

func TestOwnCapabilities_KeepsDirectDropsTransitive(t *testing.T) {
	out := ownCapabilities(cil(
		rec("FILES", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p", "os"),
		rec("NETWORK", cpb.CapabilityType_CAPABILITY_TYPE_TRANSITIVE, "p", "other", "net"),
	))
	assert.Equal(t, map[string]map[string]struct{}{
		"p": {"CAPABILITY_FILES": {}},
	}, out)
}

// The F4 property: capslock emits one record per originating function, so a
// (package, capability) pair is the package's as soon as ANY of its records
// qualifies. A transitive record must not mask a direct one.
func TestOwnCapabilities_AnyQualifyingFunctionWins(t *testing.T) {
	out := ownCapabilities(cil(
		rec("NETWORK", cpb.CapabilityType_CAPABILITY_TYPE_TRANSITIVE, "p", "other", "net"),
		rec("NETWORK", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p", "net"),
		rec("NETWORK", cpb.CapabilityType_CAPABILITY_TYPE_TRANSITIVE, "p", "another", "net"),
	))
	_, ok := out["p"]["CAPABILITY_NETWORK"]
	assert.True(t, ok, "one qualifying record among transitives must make the pair the package's")
}

func TestOwnCapabilities_AttributesToPathOrigin(t *testing.T) {
	out := ownCapabilities(cil(
		rec("FILES", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "origin", "os"),
	))
	assert.Contains(t, out, "origin")
	assert.NotContains(t, out, "os")
}

// A path that reaches the classified function from a deeper stdlib frame is
// not the app's operation, even though capslock calls it DIRECT (a stdlib hop
// never demotes). This is the strconv.FormatFloat -> internal/strconv.float32bits
// shape: formatting a float would otherwise hard-fail as UNSAFE_POINTER.
func TestOwnCapabilities_DropsSinkReachedInsideStdlib(t *testing.T) {
	out := ownCapabilities(cil(
		rec("UNSAFE_POINTER", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT,
			"p", "strconv", "internal/strconv", "internal/strconv"),
	))
	assert.Empty(t, out, "the app called strconv, not the unsafe operation")
}

// The context.WithCancel -> afterFuncCtx.cancel$1 -> (*net.netFD).connect$1
// shape: VTA links every func() ever handed to context.AfterFunc, so merely
// cancelling a context reads as NETWORK.
func TestOwnCapabilities_DropsGuessedClosureEdge(t *testing.T) {
	out := ownCapabilities(cil(
		rec("NETWORK", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT,
			"p", "context", "context", "net"),
	))
	assert.Empty(t, out, "the app called context, not the network")
}

// The app's own function is itself classified: a one-element path is its own
// caller and must be kept.
func TestOwnCapabilities_KeepsSelfClassifiedFunction(t *testing.T) {
	out := ownCapabilities(cil(
		rec("UNSAFE_POINTER", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p"),
	))
	_, ok := out["p"]["CAPABILITY_UNSAFE_POINTER"]
	assert.True(t, ok)
}

// Indirection inside the app's own package still counts: capslock emits a
// record per originating function, so the inner function's own record qualifies.
func TestOwnCapabilities_KeepsIntraPackageIndirection(t *testing.T) {
	out := ownCapabilities(cil(
		rec("FILES", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p", "p", "os"),
	))
	_, ok := out["p"]["CAPABILITY_FILES"]
	assert.True(t, ok, "p's own code calls os.Stat, via one hop inside p")
}

func TestOwnCapabilities_SkipsEmptyPath(t *testing.T) {
	out := ownCapabilities(cil(rec("FILES", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT)))
	assert.Empty(t, out)
}

func TestOwnCapabilities_DedupesCapabilities(t *testing.T) {
	out := ownCapabilities(cil(
		rec("FILES", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p", "os"),
		rec("FILES", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p", "os"),
		rec("NETWORK", cpb.CapabilityType_CAPABILITY_TYPE_DIRECT, "p", "net"),
	))
	assert.Len(t, out["p"], 2)
}

// --- evaluation ----------------------------------------------------------

func TestEvaluate_Ok_FilesWithFsCap(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_FILES", []app.SubjectFilter{
		{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, StatusOK, f.Status)
}

func TestEvaluate_MissingCap_FilesWithoutFsCap(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_FILES", []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, StatusMissingCap, f.Status)
	assert.Contains(t, f.Reason, "fs.")
}

func TestEvaluate_NetworkSatisfiedByCh(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_NETWORK", []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, StatusOK, f.Status)
}

func TestEvaluate_HardFailNoMatterWhat(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_EXEC", []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, StatusHardFail, f.Status)
}

func TestEvaluate_RuntimeAlwaysOk(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_RUNTIME", nil)
	assert.Equal(t, StatusOK, f.Status)
}

func TestPackageForManifest_DemoStripsToWidgets(t *testing.T) {
	got := packageForManifest(app.AppIdT(widgetsPkgPath + "/table"))
	assert.Equal(t, widgetsPkgPath, got)
}

func TestPackageForManifest_TopLevelPassThrough(t *testing.T) {
	id := app.AppIdT("github.com/stergiotis/boxer/apps/imztop")
	got := packageForManifest(id)
	assert.Equal(t, string(id), got)
}

// --- baseline comparison -------------------------------------------------

func TestCompareToBaseline_IgnoresOk(t *testing.T) {
	drift, _ := CompareToBaseline([]Finding{{AppId: "a", Cap: "CAPABILITY_RUNTIME", Status: StatusOK}})
	assert.Empty(t, drift, "a capability the manifest justifies is not a finding")
}

func TestCompareToBaseline_UnacceptedIsDrift(t *testing.T) {
	drift, _ := CompareToBaseline([]Finding{{AppId: "nope", Cap: "CAPABILITY_EXEC", Status: StatusHardFail}})
	require.Len(t, drift, 1)
	assert.Equal(t, "nope", drift[0].AppId)
}

func TestCompareToBaseline_AcceptedIsNotDrift(t *testing.T) {
	for appId, caps := range baseline {
		require.NotEmpty(t, caps)
		drift, _ := CompareToBaseline([]Finding{{AppId: appId, Cap: caps[0], Status: StatusHardFail}})
		assert.Empty(t, drift, "a baselined finding must not be drift")
		return
	}
}

func TestCompareToBaseline_MissingAcceptedIsStale(t *testing.T) {
	_, stale := CompareToBaseline(nil)
	assert.Len(t, stale, baselineSize(), "every accepted entry is stale when nothing is reported")
}

// Every baseline entry carries a stated reason: the list is a record of debt,
// and an entry without a reason is indistinguishable from an oversight.
func TestBaseline_EveryEntryHasAReason(t *testing.T) {
	for appId, caps := range baseline {
		for _, c := range caps {
			k := baselineKey(appId, c)
			assert.NotEmpty(t, baselineReasons[k], "baseline entry %q has no reason", k)
		}
	}
	for k := range baselineReasons {
		found := false
		for appId, caps := range baseline {
			for _, c := range caps {
				if baselineKey(appId, c) == k {
					found = true
				}
			}
		}
		assert.True(t, found, "reason %q documents no baseline entry", k)
	}
}

func baselineSize() (n int) {
	for _, caps := range baseline {
		n += len(caps)
	}
	return
}

// --- the app set ---------------------------------------------------------

// moduleRoot resolves the repository root from the test's working directory.
// godepcollect.ModuleRoot needs an absolute start: it walks up with
// filepath.Dir, and filepath.Dir(".") is ".", so a relative "." reports the
// filesystem root as reached and returns ok=false immediately.
func moduleRoot(t *testing.T) (root string) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	root, ok := godepcollect.ModuleRoot(wd)
	require.True(t, ok, "module root from %q", wd)
	return
}

// The gate evaluates only apps registered in this binary, and registration
// happens through the side-effect imports in check.go. A package that registers
// an app but is not imported there is silently unchecked — the failure mode is
// invisible, so assert the list against the tree instead of trusting it.
func TestAppSetIsComplete(t *testing.T) {
	root := moduleRoot(t)
	registered := make(map[string]struct{})
	for _, m := range app.AllManifests() {
		registered[packageForManifest(m.Id)] = struct{}{}
	}
	var missing []string
	for _, dir := range []string{"apps", filepath.Join("public", "thestack", "imzero2", "egui2", "demo", "apps")} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		require.NoError(t, err)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, dir, e.Name(), "app_register.go"))
			if err != nil {
				continue // not an app
			}
			if !strings.Contains(string(b), "app.Manifest{") {
				continue
			}
			pkgPath := "github.com/stergiotis/boxer/" + filepath.ToSlash(filepath.Join(dir, e.Name()))
			if _, ok := registered[pkgPath]; !ok {
				missing = append(missing, pkgPath)
			}
		}
	}
	assert.Empty(t, missing,
		"these packages register an app but are not side-effect imported by check.go, so the gate never sees them")
}

// --- the real analysis ---------------------------------------------------

// TestAnalyse_MatchesBaseline runs the real capslock analysis and compares it
// against the accepted baseline: this is ADR-0026 §SD10's M3 `compare` mode.
//
// It costs ~15s and several GB of RSS (SSA over the apps' dependency cones), so
// it is skipped under -short, which is how CI's default test gate runs. The
// capability gate itself runs it without -short from scripts/ci/lint.sh.
// Everything above this line is pure and runs in every `go test` invocation.
func TestAnalyse_MatchesBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("capslock analysis is expensive; run without -short (scripts/ci/lint.sh does)")
	}
	findings, err := Analyse(context.Background(), Options{Root: moduleRoot(t)})
	require.NoError(t, err)
	require.NotEmpty(t, findings, "an empty result means the load matched nothing, not that the tree is clean")
	drift, stale := CompareToBaseline(findings)
	for _, f := range drift {
		t.Errorf("drift: %s :: %s — %s\n\tan app gained a capability its manifest does not justify.\n"+
			"\tEither declare the cap in its manifest, remove the call, or accept it in baseline.go with a reason.",
			f.AppId, f.Cap, f.Reason)
	}
	for _, s := range stale {
		t.Errorf("stale baseline entry: %s — no longer reported; remove it from baseline.go", s)
	}
}
