package trailviewer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// Every region renders under the discard channel — controls, verdicts,
// strip, facets, table (flat and grouped) and each detail tab — over the
// fixture rows, without an id-stack mismatch.
func TestTheWindowRendersEveryRegion(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst, _ := opsFixture(t)
	inst.cache.buckets = []Bucket{{Bucket: t0.Unix(), Key: "auditEvent", N: 2}, {Bucket: t0.Unix() + 60, Key: "llmCall", N: 1}}
	inst.cache.step = time.Minute
	inst.cache.facets = [][]FacetValue{{{Value: "auditEvent", N: 2}}, {{Value: "ok", N: 1}}, {{Value: "apps/a", N: 2}}, {{Value: "trail.config", N: 1}}, {{Value: "p-1", N: 1}}}
	inst.applyFilter(Filter{}.with(FacetKind, "auditEvent", false))
	hold(inst, fixtureRows())
	require.True(t, inst.selectById(6))
	prev := judgeAudit(inst.selected.Audit, nil, nil, time.Hour)
	inst.detail.fill, inst.detail.id, inst.detail.has = &detailFill{id: 6, before: fixtureRows()[1:3], previous: &prev}, 6, true
	for _, grouped := range []bool{false, true} {
		inst.setGrouped(grouped)
		for _, tab := range []TabE{TabFields, TabPrevious, TabContext} {
			inst.tab = tab
			ids := inst.ids
			for range c.IdScope(ids.PrepareStr("window")) {
				assert.NotPanics(t, func() { require.NoError(t, inst.render()) }, "grouped=%v tab=%s", grouped, tab)
			}
			c.CurrentApplicationState.StateManager.ScriptReset()
		}
	}
}
