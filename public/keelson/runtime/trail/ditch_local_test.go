package trail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The ditch row is within the audit event's bounds, ok or failed, with the
// app filter when given and the error, bounded, when it failed.
func TestDitchEvent(t *testing.T) {
	ok := trail.DitchEvent("", nil)
	assert.Equal(t, trail.TrailDomain, ok.Domain)
	assert.Equal(t, trail.ActionAuditDitch, ok.Action)
	assert.Equal(t, trail.OutcomeOk, ok.Outcome)
	assert.Empty(t, ok.AttrKeys)
	assert.Empty(t, ok.RefTypes, "the ditch names no entity")

	failed := trail.DitchEvent("apps/x", errors.New(strings.Repeat("e", 2*trail.MaxValueRunes)))
	assert.Equal(t, trail.OutcomeFailed, failed.Outcome)
	assert.Equal(t, []string{"app", "error"}, failed.AttrKeys)
	assert.Equal(t, "apps/x", failed.AttrValues[0])
	assert.Len(t, []rune(failed.AttrValues[1]), trail.MaxValueRunes)
	for _, row := range []trail.AuditEvent{ok, failed} {
		row.PrincipalBy, row.OriginBy = trail.PrincipalByNone, evidence.OriginByHost.String()
		assert.NoError(t, row.Validate())
	}
}

// A recorder with no store does not ditch: the path never mutates without
// its row.
func TestDitchNeedsADurableTrail(t *testing.T) {
	var none *trail.Recorder
	require.Error(t, none.DitchBodies(context.Background(), ""))
	rec := trail.NewRecorder(nil, "run-1", zerolog.Nop())
	defer rec.Close()
	require.Error(t, rec.DitchBodies(context.Background(), ""))
}

// refuseAlter is a server that refuses mutations.
type refuseAlter struct {
	recordstore.ExecutorI
}

func (inst refuseAlter) Exec(ctx context.Context, sql string) error {
	if strings.HasPrefix(sql, "ALTER TABLE") {
		return errors.New("mutations are not allowed")
	}
	return inst.ExecutorI.Exec(ctx, sql)
}

// Over clickhouse-local: a ditch through the recorder lands its row, ok
// when the statement ran and failed with the error when it did not.
func TestDitchBodiesRecordsTheDitch(t *testing.T) {
	local, ctx := localFacts(t)
	rec := trail.NewRecorder(local, "run-d", zerolog.Nop())
	defer rec.Close()
	require.NoError(t, rec.DitchBodies(ctx, "apps/x"))
	refusing := trail.NewRecorder(refuseAlter{ExecutorI: local}, "run-e", zerolog.Nop())
	defer refusing.Close()
	err := refusing.DitchBodies(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutations are not allowed")

	byOutcome := map[string]trail.AuditEvent{}
	for _, e := range scanEvents(t, rec, ctx, recordstore.ScanOpts{}) {
		if e.AuditEvent.Val.Action == trail.ActionAuditDitch {
			byOutcome[e.AuditEvent.Val.Outcome] = e.AuditEvent.Val
			assert.Equal(t, "", e.Origin.Val.App, "the recorder's own row")
		}
	}
	require.Len(t, byOutcome, 2)
	assert.Equal(t, []string{"app"}, byOutcome[trail.OutcomeOk].AttrKeys)
	assert.Equal(t, []string{"apps/x"}, byOutcome[trail.OutcomeOk].AttrValues)
	assert.Equal(t, []string{"error"}, byOutcome[trail.OutcomeFailed].AttrKeys)
	assert.Contains(t, byOutcome[trail.OutcomeFailed].AttrValues[0], "mutations are not allowed")
	assert.Equal(t, trail.TrailDomain, byOutcome[trail.OutcomeFailed].Domain)
}
