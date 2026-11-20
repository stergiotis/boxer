package wasmsurvey

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/packageprops"
)

const testModule = "example.com/m"

func tiered(path string, st packageprops.StabilityE, k packageprops.Kind) (r HarvestRow) {
	return HarvestRow{ImportPath: testModule + "/" + path, Stability: st, Kind: k, Synopsis: "Package " + path + " does a thing."}
}

func rulesOf(findings []StabilityFinding) (rules []string) {
	for _, f := range findings {
		rules = append(rules, f.Rule.String()+" "+f.Imported)
	}
	return
}

func TestCheckStability_CleanStableEdges(t *testing.T) {
	rows := []HarvestRow{
		tiered("a", packageprops.StabilityStable, packageprops.KindUnspecified),
		tiered("b", packageprops.StabilityStable, packageprops.KindUnspecified),
	}
	imports := map[string][]string{
		testModule + "/a": {"fmt", "github.com/other/x", testModule + "/b", testModule + "/a/internal/impl", propsImportPath},
	}
	assert.Empty(t, CheckStability(rows, imports, testModule))
}

// The rule the tiers exist for: a Stable package's promise cannot rest on a
// package that promises less.
func TestCheckStability_WeakerAndNonLibraryImports(t *testing.T) {
	rows := []HarvestRow{
		tiered("a", packageprops.StabilityStable, packageprops.KindUnspecified),
		tiered("exp", packageprops.StabilityExperimental, packageprops.KindUnspecified),
		tiered("int", packageprops.StabilityInternal, packageprops.KindUnspecified),
		tiered("demo", packageprops.StabilityUnspecified, packageprops.KindDemo),
		tiered("helper", packageprops.StabilityStable, packageprops.KindTestSupport),
		tiered("plain", packageprops.StabilityUnspecified, packageprops.KindUnspecified),
	}
	imports := map[string][]string{
		testModule + "/a": {testModule + "/demo", testModule + "/exp", testModule + "/helper", testModule + "/int", testModule + "/plain", testModule + "/undeclared"},
	}
	got := CheckStability(rows, imports, testModule)
	assert.Equal(t, []string{
		"imports-weaker-tier " + testModule + "/exp",
		"imports-weaker-tier " + testModule + "/int",
		"imports-non-library " + testModule + "/demo",
		"imports-non-library " + testModule + "/helper",
		"imports-untiered " + testModule + "/plain",
		"imports-untiered " + testModule + "/undeclared",
	}, rulesOf(got))
	for _, f := range got {
		assert.Equal(t, f.Rule == StabilityRuleImportsUntiered, f.Rule.Advisory())
	}
}

func TestCheckStability_DocAndKind(t *testing.T) {
	noDoc := tiered("nodoc", packageprops.StabilityStable, packageprops.KindUnspecified)
	noDoc.Synopsis = ""
	rows := []HarvestRow{
		noDoc,
		tiered("tool", packageprops.StabilityStable, packageprops.KindTool),
		// Only Stable packages are held to the rules.
		{ImportPath: testModule + "/expnodoc", Stability: packageprops.StabilityExperimental},
	}
	got := CheckStability(rows, nil, testModule)
	require.Len(t, got, 2)
	assert.Equal(t, StabilityRuleNoPackageDoc, got[0].Rule)
	assert.Equal(t, testModule+"/nodoc", got[0].ImportPath)
	assert.Equal(t, StabilityRuleNonLibraryKind, got[1].Rule)
	assert.Equal(t, testModule+"/tool", got[1].ImportPath)
}
