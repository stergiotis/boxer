//go:build integration

package providersgodep

import (
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

	rec := packagesTable(s).Build(introspect.AllColumns(), len(s.man.Packages))
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
