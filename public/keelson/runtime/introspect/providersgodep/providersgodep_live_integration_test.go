//go:build integration

package providersgodep

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The live collection is in the integration lane because it shells out to the
// `go` toolchain over the whole closure — seconds of work and a heavy
// dependency, which is what the lane is for (ENGINEERING_PRACTICES §4).
//
// It pins the two properties the unit tests cannot: that resolveConfig finds
// this module from the test's working directory, and that the counts the
// header reports agree with the rows the other two tables serve.
func TestLiveCollectionServesThisModule(t *testing.T) {
	c := newCache(resolveConfig(Config{}))
	c.budget = 2 * time.Minute

	s := c.get()
	require.Equal(t, statusReady, s.status, "collection failed: %s", s.errText)

	assert.Equal(t, "github.com/stergiotis/boxer", s.man.Run.RootModulePath)
	// The closure is on the order of a thousand packages under the repo's
	// tags; the floors are loose so a dependency change does not fail this.
	assert.Greater(t, len(s.man.Packages), 500)
	assert.Greater(t, len(s.edges), 5000)
	assert.NotEmpty(t, s.man.Run.BuildTags, "the repo's tags file drives collection")

	// The denormalised header counts are what go_collection reports; they
	// must match the rows go_packages and go_imports actually serve.
	assert.EqualValues(t, len(s.man.Packages), s.man.Run.NumPackages)
	assert.EqualValues(t, len(s.edges), s.man.Run.NumEdges)

	rec := packagesTable(s, nil).Build(introspect.AllColumns(), len(s.man.Packages))
	defer rec.Release()
	require.EqualValues(t, len(s.man.Packages), rec.NumRows())

	// Every class the collector emits should appear in a closure this size,
	// and the sum over classes accounts for every row.
	counts := map[string]int{}
	for i := range s.man.Packages {
		counts[s.man.Packages[i].Class]++
	}
	for _, class := range []string{"stdlib", "internal", "external"} {
		assert.Positive(t, counts[class], "class %q", class)
	}
	assert.Equal(t, len(s.man.Packages), counts["stdlib"]+counts["internal"]+counts["external"])

	// Edge endpoints resolve: go/packages walks the closure, so an empty
	// dst_path would mean the manifest lost a node.
	unresolved := 0
	for _, e := range s.edges {
		if e.dstPath == "" {
			unresolved++
		}
	}
	assert.Zero(t, unresolved, "every import target should be a collected package")
}

// The volume pass over the real closure (ADR-0173 §SD3/§SD10). It is a second
// packages.Load and about two seconds of scanning, so it lives here rather
// than beside the fixture tests.
//
// What it pins is that the marker normalisation survives contact with the
// tree: the tool field is free-form prose, and a regression there does not
// fail — it silently fragments one tool into a bucket per caller, or collapses
// distinct ones together. Only real markers can catch that.
func TestLiveVolumePassNamesItsGenerators(t *testing.T) {
	cfg := resolveConfig(Config{})
	vol := newVolumeCache(cfg)

	// Reach through the same getter the column uses, so this exercises the
	// lazy population rather than a private path.
	require.Positive(t, vol.get("github.com/stergiotis/boxer/public/observability/eh").CodeLines,
		"the volume pass did not run; is the toolchain reachable from the test's working directory?")

	byTool := map[string]int{}
	generatedPkgs, multiTool := 0, 0
	for _, v := range vol.byPkg {
		if len(v.Generators) == 0 {
			continue
		}
		generatedPkgs++
		if len(v.Generators) > 1 {
			multiTool++
		}
		for _, g := range v.Generators {
			byTool[g] += v.GeneratedCode
		}
		assert.True(t, slices.IsSorted(v.Generators), "generators must be sorted: %v", v.Generators)
	}

	assert.Positive(t, generatedPkgs, "this module generates code; some package must name a tool")
	assert.Positive(t, multiTool, "several packages here carry more than one tool")

	// Tools whose markers exist verbatim in the tree. If normalisation
	// regresses, these become "Leeway DML (github.com/…)" or similar and the
	// lookup misses.
	for _, want := range []string{"ANTLR 4.13.2", "Leeway DML", "protoc-gen-go"} {
		assert.Contains(t, byTool, want, "known generator missing from the vocabulary")
	}

	// The precise inverse of the invoker normalisation. A cardinality bound
	// was tried first and is the wrong gate: the closure's vocabulary is
	// legitimately ~130 strings, because stdlib and third-party markers carry
	// full invocations ("stringer -type=Foo", "go run gen.go") that cannot be
	// collapsed without inventing a taxonomy §SD10 declines to invent. What
	// *can* be stated exactly is that no tool ends in the caller parenthetical
	// invokerRe exists to remove.
	for tool := range byTool {
		assert.NotRegexp(t, `\)$`, tool, "invoker parenthetical was not stripped")
	}
	t.Logf("%d distinct generators over the closure", len(byTool))
}
