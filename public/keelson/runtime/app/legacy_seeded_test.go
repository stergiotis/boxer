//go:build llm_generated_opus47

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeededFuncApp_InstancesHaveDistinctSeeds is the regression
// guard for the multi-instance ID-collision fix: every NewSeededFuncApp
// call must hand a fresh seed, so two windows of the same app cannot
// derive identical Go-side widget IDs in the same frame.
func TestSeededFuncApp_InstancesHaveDistinctSeeds(t *testing.T) {
	m := testManifest("org.test.seeded")
	render := func(seed uint64) (err error) { return }

	a1, err := NewSeededFuncApp(m, render)
	require.NoError(t, err)
	a2, err := NewSeededFuncApp(m, render)
	require.NoError(t, err)

	assert.NotEqual(t, a1.Seed(), a2.Seed(), "every SeededFuncApp must carry a unique seed")
	assert.NotSame(t, a1, a2)
}

func TestSeededFuncApp_NilRender(t *testing.T) {
	_, err := NewSeededFuncApp(testManifest("org.test.nil"), nil)
	require.Error(t, err)
}

func TestSeededFuncApp_InvalidManifest(t *testing.T) {
	_, err := NewSeededFuncApp(Manifest{}, func(seed uint64) (err error) { return })
	require.Error(t, err)
}

func TestSeededFuncApp_FramePropagatesSeed(t *testing.T) {
	var sawSeed uint64
	a, err := NewSeededFuncApp(testManifest("org.test.frame"), func(seed uint64) (err error) {
		sawSeed = seed
		return
	})
	require.NoError(t, err)

	err = a.Frame(nil)
	require.NoError(t, err)
	assert.Equal(t, a.Seed(), sawSeed, "renderer must receive the same seed the app advertised")
}
