//go:build llm_generated_opus47

package capslock

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

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
	for _, cap := range []string{
		"CAPABILITY_OPERATING_SYSTEM",
		"CAPABILITY_EXEC",
		"CAPABILITY_SYSTEM_CALLS",
		"CAPABILITY_ARBITRARY_EXECUTION",
		"CAPABILITY_CGO",
		"CAPABILITY_UNSAFE_POINTER",
		"CAPABILITY_REFLECT",
		"CAPABILITY_MODIFY_SYSTEM_STATE",
	} {
		_, hardFail, _ := capRequirements(cap)
		assert.True(t, hardFail, "cap=%s should be hard fail", cap)
	}
}

func TestCapRequirements_Unknown_HardFail(t *testing.T) {
	_, hardFail, _ := capRequirements("CAPABILITY_NEW_THING")
	assert.True(t, hardFail)
}

func TestEvaluate_Ok_FilesWithFsCap(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_FILES", []app.SubjectFilter{
		{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, findingOK, f.Status)
}

func TestEvaluate_MissingCap_FilesWithoutFsCap(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_FILES", []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, findingMissingCap, f.Status)
	assert.Contains(t, f.Reason, "fs.")
}

func TestEvaluate_NetworkSatisfiedByCh(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_NETWORK", []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, findingOK, f.Status)
}

func TestEvaluate_HardFailNoMatterWhat(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_EXEC", []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionPub},
	})
	assert.Equal(t, findingHardFail, f.Status)
}

func TestEvaluate_RuntimeAlwaysOk(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_RUNTIME", nil)
	assert.Equal(t, findingOK, f.Status)
}

func TestEvaluate_Unanalysed(t *testing.T) {
	f := evaluate("some.app", "CAPABILITY_UNANALYZED", nil)
	assert.Equal(t, findingNeedsAnalysis, f.Status)
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

func TestAggregateByPackage_DedupesCapabilities(t *testing.T) {
	report := capslockReport{
		CapabilityInfo: []capInfo{
			{Capability: "CAPABILITY_FILES", Path: []pathStep{{Package: "p"}}},
			{Capability: "CAPABILITY_FILES", Path: []pathStep{{Package: "p"}}},
			{Capability: "CAPABILITY_NETWORK", Path: []pathStep{{Package: "p"}}},
		},
	}
	out := aggregateByPackage(report)
	assert.Len(t, out["p"], 2)
}

func TestAggregateByPackage_SkipsEmptyPath(t *testing.T) {
	report := capslockReport{
		CapabilityInfo: []capInfo{
			{Capability: "CAPABILITY_FILES"},
		},
	}
	out := aggregateByPackage(report)
	assert.Empty(t, out)
}
