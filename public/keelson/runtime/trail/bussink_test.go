package trail

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	busaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// One audited bus request becomes one audit event: the requester's app and
// window under the recorder's run, originBy host whatever the recorder's
// default, no principal, and two requests of one subject are two rows.
func TestBusRequestsAreAuditEvents(t *testing.T) {
	exec := &gateExec{open: true}
	fwd := &collectForwarder{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithForwarder(fwd))
	sink := AsAuditSink(rec)
	at := time.Unix(1_700_000_000, 0).UTC()
	r := busaudit.AuditRecord{AppId: "apps/play", InstanceKey: 3, Subject: "runtime.persist.play.x.get", Result: busaudit.AuditResultDenied, Ts: at}
	sink.Record(r)
	sink.Record(r)
	require.NoError(t, rec.Flush(context.Background()))
	rec.Close()
	fwd.mu.Lock()
	defer fwd.mu.Unlock()
	require.Len(t, fwd.got, 2, "one row per request")
	assert.NotEqual(t, fwd.got[0].Id, fwd.got[1].Id, "repeated requests do not collide")
	e := fwd.got[0]
	assert.Equal(t, audit.Origin{Id: e.Id, Run: "run-1", App: "apps/play", Instance: 3}, e.Origin)
	assert.True(t, e.At.Equal(at))
	assert.Equal(t, busaudit.BusDomain, e.Row.Domain)
	assert.Equal(t, audit.OutcomeDenied, e.Row.Outcome)
	assert.Equal(t, "host", e.Row.OriginBy, "the bus is the host's, whatever the recorder's default")
	assert.Equal(t, audit.PrincipalByNone, e.Row.PrincipalBy)
	assert.Equal(t, []string{"runtime.persist.play.x.get"}, e.Row.RefValues)
	assert.EqualValues(t, 0, rec.Counts().Invalid)
}
