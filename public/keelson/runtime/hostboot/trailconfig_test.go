package hostboot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// The config event is the trail's own row, one attribute per audit-relevant
// setting, the anchor store's URL never among them (ADR-0296, the Update
// of 2026-10-31).
func TestTrailConfigRow(t *testing.T) {
	trail.RequiredEnv.SetForTest(t, "true")
	llm.Retain.SetForTest(t, "ring")
	chstore.LogFactsEnabled.SetForTest(t, "0")
	headstore.OrgStoreURL.SetForTest(t, "http://anchors.example:8123")
	s := trailSettingsOf(anchor.NewMemStore(), &runinfo.Inst{VcsRevision: "abc123", VcsModified: true})
	row := trailConfigRow(s)
	assert.Equal(t, trail.TrailDomain, row.Domain)
	assert.Equal(t, trail.ActionAuditConfig, row.Action)
	assert.Equal(t, trail.OutcomeOk, row.Outcome)
	assert.Equal(t, []string{"trail-required", "llm-retain", "log-facts", "anchor-url", "anchor-store", "flush-interval", "vcs-revision", "vcs-modified"}, row.AttrKeys)
	assert.Equal(t, []string{"true", "ring", "false", "set", "memory", trailFlushInterval.String(), "abc123", "true"}, row.AttrValues)
	for _, v := range row.AttrValues {
		assert.NotContains(t, v, "anchors.example")
	}
	row.PrincipalBy, row.OriginBy = trail.PrincipalByNone, evidence.OriginByHost.String()
	assert.NoError(t, row.Validate())

	// No build info: no revision attributes.
	row = trailConfigRow(trailSettings{Retain: "off", AnchorStore: "org", FlushInterval: time.Second})
	assert.NotContains(t, row.AttrKeys, "vcs-revision")
	assert.NotContains(t, row.AttrKeys, "vcs-modified")
}
