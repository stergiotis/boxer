// Package auditrectest holds the conformance test every binding of
// package auditrec runs against its store on a server: what the recorder
// writes through the binding reads back unchanged, a run's rows and seals
// are selected and verify, and the reads narrow on the right columns.
package auditrectest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
)

// Scheme is the scheme the conformance test seals under.
var Scheme = evidence.NewScheme("auditrectest/v1")

// Collector is a forwarder that keeps what it is handed.
type Collector struct {
	mu  sync.Mutex
	got []audit.Event
}

// Forward keeps events.
func (inst *Collector) Forward(_ context.Context, events []audit.Event) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.got = append(inst.got, events...)
	return nil
}

// Events is what the collector was handed so far.
func (inst *Collector) Events() []audit.Event {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]audit.Event(nil), inst.got...)
}

// FullEvent is an audit event with every field the emitter sets; the
// principal and the purpose come from the context (FullContext).
func FullEvent(subject uint64) audit.AuditEvent {
	return audit.AuditEvent{
		Domain: "conformance", Action: "read-value", Outcome: audit.OutcomeOk,
		PrincipalBy: audit.PrincipalBySystem, PrincipalVia: option.Some(audit.PrincipalViaMtls),
		Node: option.Some("node-a"), Subject: subject, Retention: "personal-data-access",
		RefTypes: []string{"vaultref", "slot"}, RefValues: []string{"v-1", "s-2"},
		AttrKeys: []string{"plane", "verb"}, AttrValues: []string{"steward", "get"},
	}
}

// FullContext carries a call identity naming an app, an instance, a
// principal and a purpose.
func FullContext() context.Context {
	return callident.WithCallIdentity(context.Background(), callident.CallIdentity{
		Origin: callident.Origin{App: "apps/conformance", Instance: 4},
		Claims: callident.Claims{Principal: "p-7", Purpose: "art-15"},
	})
}

// Conformance runs the checks every binding passes. open returns the
// binding over a fresh, provisioned table on a server; the test closes it
// through the recorder.
func Conformance[B auditrec.BatchI](t *testing.T, open func(t *testing.T) auditrec.StoreI[B]) {
	t.Helper()
	ctx := context.Background()
	fwd := &Collector{}
	rec, err := auditrec.New(open(t), "conformance-run", Scheme, zerolog.Nop(),
		auditrec.WithForwarder(fwd), auditrec.WithOrigin("apps/default", 1), auditrec.WithFlushInterval(0))
	require.NoError(t, err)
	defer rec.Close()

	at := time.Date(2026, 10, 13, 9, 30, 0, 123456789, time.UTC)
	require.NoError(t, rec.Event(FullContext(), at, FullEvent(42)))
	bad := FullEvent(43)
	bad.Action = "Not A Name"
	require.NoError(t, rec.Event(FullContext(), at.Add(time.Second), bad))
	require.NoError(t, rec.Flush(ctx))
	require.NoError(t, rec.Event(context.Background(), at.Add(2*time.Second), FullEvent(44)))
	require.NoError(t, rec.Flush(ctx))
	require.Eventually(t, func() bool { return len(fwd.Events()) == 3 }, 5*time.Second, 10*time.Millisecond, "the prompt path forwards the three events")

	// What was forwarded reads back unchanged: every field reaches the
	// store and returns, the origin included.
	forwarded := map[uint64]audit.Event{}
	for _, e := range fwd.Events() {
		forwarded[e.Id] = e
	}
	got, err := rec.EventsBySubject(ctx, 42, auditrec.Window{})
	require.NoError(t, err)
	require.Len(t, got, 1, "the event of subject 42")
	want, ok := forwarded[got[0].Id]
	require.True(t, ok, "the read event is one the prompt path forwarded")
	assert.True(t, want.At.Equal(got[0].At), "the time reads back: %s, %s", want.At, got[0].At)
	got[0].At = want.At
	assert.Equal(t, want, got[0], "the event reads back as it was forwarded")
	assert.Equal(t, audit.Origin{Id: want.Id, Run: "conformance-run", App: "apps/conformance", Instance: 4}, got[0].Origin)
	assert.Equal(t, option.Some("p-7"), got[0].Row.Principal)
	assert.Equal(t, option.Some(audit.PrincipalViaMtls), got[0].Row.PrincipalVia)
	assert.Equal(t, option.Some("art-15"), got[0].Row.Purpose)

	inv, err := rec.EventsBySubject(ctx, 43, auditrec.Window{})
	require.NoError(t, err)
	require.Len(t, inv, 1)
	assert.Equal(t, audit.ActionAuditInvalid, inv[0].Row.Action, "a row outside the bounds reads back as the invalid row")
	assert.Equal(t, "Not A Name", inv[0].Row.AttrValues[1])

	byP, err := rec.EventsByPrincipal(ctx, "p-7", auditrec.Window{Newest: true})
	require.NoError(t, err)
	require.Len(t, byP, 2, "the principal's two events; the third had none")
	assert.True(t, byP[0].At.After(byP[1].At), "newest first")
	none, err := rec.EventsByPrincipal(ctx, "p-7", auditrec.Window{From: at.Add(time.Hour)})
	require.NoError(t, err)
	assert.Empty(t, none, "the window bounds the read")
	noOrigin, err := rec.EventsBySubject(ctx, 44, auditrec.Window{})
	require.NoError(t, err)
	require.Len(t, noOrigin, 1)
	assert.Equal(t, audit.Origin{Id: noOrigin[0].Id, Run: "conformance-run", App: "apps/default", Instance: 1}, noOrigin[0].Origin, "without a call identity the origin is WithOrigin's")

	// The run's rows and seals are selected, and the seal law holds over
	// the rows as they read back.
	broken, unnamed, _, err := rec.VerifySeals(ctx, "conformance-run")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreakNone, broken.Why, "the run's seals hold: %+v", broken)
	assert.Empty(t, unnamed)
	assert.EqualValues(t, 2, rec.Counts().Seals)
	broken, unnamed, _, err = rec.VerifySeals(ctx, "another-run")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreakNone, broken.Why)
	assert.Empty(t, unnamed, "another run selects none of these rows")
}
