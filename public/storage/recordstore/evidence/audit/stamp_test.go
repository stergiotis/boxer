package audit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// Stamp takes the principal and the purpose from the claims, never from
// the row, and OriginBy from the attestation, else the default.
func TestStamp(t *testing.T) {
	row := validEvent()
	out := audit.Stamp(context.Background(), row, evidence.OriginBySelf)
	assert.False(t, out.Principal.Has, "no identity, no principal")
	assert.False(t, out.Purpose.Has)
	assert.Equal(t, audit.PrincipalByNone, out.PrincipalBy)
	assert.False(t, out.PrincipalVia.Has, "no principal, no mechanism")
	assert.Equal(t, "self", out.OriginBy)
	require.NoError(t, out.Validate())

	ctx := callident.WithClaims(context.Background(), callident.Claims{Principal: "p-2", Purpose: "art-30"})
	ctx = evidence.WithAttestation(ctx, evidence.Attestation{OriginBy: evidence.OriginByEnvelope})
	out = audit.Stamp(ctx, row, evidence.OriginBySelf)
	assert.Equal(t, option.Some("p-2"), out.Principal)
	assert.Equal(t, option.Some("art-30"), out.Purpose)
	assert.Equal(t, row.PrincipalBy, out.PrincipalBy, "the trust class stays the emitter's")
	assert.Equal(t, row.PrincipalVia, out.PrincipalVia, "the mechanism stays the emitter's")
	assert.Equal(t, "envelope", out.OriginBy)
}

// The run is the recorder's whatever the call identity says; the app and
// the instance are the call identity's when it names them.
func TestOriginOf(t *testing.T) {
	def := audit.Origin{Run: "ignored", App: "apps/def", Instance: 1}
	assert.Equal(t, audit.Origin{Run: "run-1", App: "apps/def", Instance: 1}, audit.OriginOf(context.Background(), "run-1", def))
	ctx := callident.WithCallIdentity(context.Background(), callident.CallIdentity{Origin: callident.Origin{Run: "run-9", App: "apps/x", Instance: 4}})
	assert.Equal(t, audit.Origin{Run: "run-1", App: "apps/x", Instance: 4}, audit.OriginOf(ctx, "run-1", def))
	ctx = callident.WithCallIdentity(context.Background(), callident.CallIdentity{Origin: callident.Origin{Run: "run-9"}})
	assert.Equal(t, audit.Origin{Run: "run-1", App: "apps/def", Instance: 1}, audit.OriginOf(ctx, "run-1", def), "a run alone moves nothing")
}

// The rows a recorder writes about itself pass the bounds and keep what
// they promise.
func TestInvalidAndGapRows(t *testing.T) {
	row := validEvent()
	row.Action = "Not A Name"
	verr := row.Validate()
	require.Error(t, verr)
	inv := audit.Invalid(row, verr, evidence.OriginBySelf)
	require.NoError(t, inv.Validate())
	assert.Equal(t, audit.ActionAuditInvalid, inv.Action)
	assert.Equal(t, []string{"dmdm", "Not A Name", verr.Error()}, inv.AttrValues)
	assert.Equal(t, row.Subject, inv.Subject)
	assert.Equal(t, row.Node, inv.Node)
	assert.Equal(t, row.Principal, inv.Principal)
	assert.Equal(t, row.PrincipalVia, inv.PrincipalVia)
	assert.Equal(t, "envelope", inv.OriginBy)

	row.OriginBy = "friend"
	assert.Equal(t, "self", audit.Invalid(row, errors.New("x"), evidence.OriginBySelf).OriginBy)

	first := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	gap := audit.Gap(12, first, first.Add(time.Minute), evidence.OriginByHost, option.Some("cell-a"))
	require.NoError(t, gap.Validate())
	assert.Equal(t, audit.ActionAuditGap, gap.Action)
	assert.Equal(t, []string{"12", "2026-10-13T01:00:00Z", "2026-10-13T01:01:00Z"}, gap.AttrValues)
	assert.Equal(t, "host", gap.OriginBy)
	assert.Equal(t, option.Some("cell-a"), gap.Node)
}
