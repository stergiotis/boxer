package trailviewer

import (
	"fmt"
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
	inst.cache.exceptions = []Exception{{Id: 6, Rule: RuleOutcome, Reason: "denied"}, {Id: 5, Rule: RuleTrailRow, Reason: "trail.config row"}}
	inst.pending = map[uint64]Review{5: {Id: 5, TsNs: t0.UnixNano(), Mark: "reviewed", Principal: "os-x"}}
	inst.compare, inst.hasCompare = fixtureRows()[3], true
	frame := func(msg string) {
		for range c.IdScope(inst.ids.PrepareStr("window")) {
			assert.NotPanics(t, func() { require.NoError(t, inst.render()) }, msg)
		}
		c.CurrentApplicationState.StateManager.ScriptReset()
	}
	for _, grouped := range []bool{false, true} {
		inst.setGrouped(grouped)
		for _, tab := range AllTabs {
			inst.tab = tab
			for _, side := range []sideE{sideFacets, sideExceptions, sideRules} {
				inst.side = side
				frame(fmt.Sprintf("grouped=%v tab=%s side=%d", grouped, tab, side))
			}
		}
	}
	// The entity case's as-of line and section.
	inst.tab = TabFields
	inst.applyCase(Case{Kind: CaseEntity, Value: "attention-level:k"})
	hold(inst, fixtureRows())
	inst.cache.asOf = &AsOf{Entity: "attention-level:k", Bound: t0, Events: []AuditDetail{*fixtureRows()[0].Audit, *fixtureRows()[1].Audit}}
	frame("as of")
	inst.cache.asOf = &AsOf{Entity: "attention-level:k", Bound: t0}
	frame("as of, no event")
}
