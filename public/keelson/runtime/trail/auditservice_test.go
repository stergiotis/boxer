package trail

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// The audit subject refuses what is not the sender's to write: another
// app's subject, and the recorder's own domain; a valid row on the app's
// own subject is taken (ADR-0327 §SD8).
func TestTheAuditSubjectIsTheSenders(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	rec := NewRecorder(nil, "run-1", zerolog.Nop())
	t.Cleanup(rec.Close)
	svc, err := NewAuditService(bus, zerolog.Nop(), rec)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	ctx := context.Background()
	row := AuditEvent{Domain: "dmdm", Action: "vault-resolve", Outcome: OutcomeOk, Retention: "disclosure", Subject: 42}

	chat := NewAuditClient(bus.NewClient("app.chat", AuditClientCaps("test")), "app.chat")
	require.NoError(t, chat.Event(ctx, time.Time{}, "p-1", "art-15", row))

	other := NewAuditClient(bus.NewClient("app.other", AuditClientCaps("test")), "app.chat")
	err = other.Event(ctx, time.Time{}, "", "", row)
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Contains(t, refused.Reason, "own subject")

	trailRow := row
	trailRow.Domain = TrailDomain
	err = chat.Event(ctx, time.Time{}, "", "", trailRow)
	require.ErrorAs(t, err, &refused)
	assert.Contains(t, refused.Reason, "recorder's own")

	assert.Equal(t, "runtime.audit.app.chat", AuditSubjectOf("app.chat"))
	_, err = NewAuditService(nil, zerolog.Nop(), rec)
	assert.Error(t, err)
}
