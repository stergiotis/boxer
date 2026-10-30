package audit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	evaudit "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// One audited request becomes one valid audit event: the bus domain and
// the request action, the result as the outcome, the subject as a
// reference, the measurements as attributes, no principal, no data subject, the trail's
// retention class, and the host's origin.
func TestEventOfAnAuditedRequest(t *testing.T) {
	rec := AuditRecord{AppId: "apps/play", InstanceKey: 3, Subject: "runtime.persist.play.x.get", Result: AuditResultTimeout,
		LatencyMs: 12, RequestSizeB: 40, ResponseSizeB: 0, Ts: time.Unix(1_700_000_000, 0)}
	e := EventOf(rec)
	require.NoError(t, e.Validate())
	assert.Equal(t, BusDomain, e.Domain)
	assert.Equal(t, BusActionRequest, e.Action)
	assert.Equal(t, evaudit.OutcomeTimeout, e.Outcome, "a timeout keeps its own outcome")
	assert.False(t, e.Principal.Has)
	assert.Equal(t, evaudit.PrincipalByNone, e.PrincipalBy)
	assert.Equal(t, "host", e.OriginBy)
	assert.Zero(t, e.Subject, "a bus request names no data subject")
	assert.Equal(t, evaudit.RetentionTrail, e.Retention)
	assert.Equal(t, []string{BusRefSubject}, e.RefTypes)
	assert.Equal(t, []string{"runtime.persist.play.x.get"}, e.RefValues)
	assert.Equal(t, []string{BusAttrLatencyMs, BusAttrRequestBytes}, e.AttrKeys, "a zero measurement is absent")
	assert.Equal(t, []string{"12", "40"}, e.AttrValues)
}

// Every result is a valid outcome: ok, denied, timeout and error by
// name, unspecified as failed.
func TestEventOfEveryResult(t *testing.T) {
	for r, outcome := range map[AuditResultE]string{
		AuditResultOk: evaudit.OutcomeOk, AuditResultDenied: evaudit.OutcomeDenied, AuditResultTimeout: evaudit.OutcomeTimeout,
		AuditResultError: evaudit.OutcomeError, AuditResultUnspecified: evaudit.OutcomeFailed,
	} {
		e := EventOf(AuditRecord{Subject: "a.b", Result: r})
		require.NoError(t, e.Validate(), r.String())
		assert.Equal(t, outcome, e.Outcome, r.String())
	}
	e := EventOf(AuditRecord{Result: AuditResultOk})
	require.NoError(t, e.Validate(), "a request without a subject is still a valid row")
	assert.Empty(t, e.RefTypes)
}
