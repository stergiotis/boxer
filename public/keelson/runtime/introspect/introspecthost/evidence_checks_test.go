package introspecthost

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// sealVerdict is a trail recorder's verdict, as the relation reads it.
type sealVerdict struct {
	broken     evidence.SealBreak
	unnamed    []uint64
	unanchored []uint64
}

func (inst sealVerdict) VerifySeals(context.Context, string) (evidence.SealBreak, []uint64, []uint64, error) {
	return inst.broken, inst.unnamed, inst.unanchored, nil
}

// keelson('evidence_checks') serves the laws over the host's stores: L31
// and L32 per journal run, L34 and the seal anchors per trail run, each
// row saying whether the law holds and where it breaks (ADR-0327 §SD7).
func TestEvidenceChecksServeTheLawsInProcess(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")
	ctx := context.Background()
	journal := agentrun.NewMemStore()
	anchors := anchor.NewMemStore()
	w, err := agentrun.Open(ctx, journal, agentrun.Run{Id: 11, Cell: "c", OwnerApp: "app.chat"})
	require.NoError(t, err)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "hello", Turn: "t1"})
	require.NoError(t, err)
	_, err = headstore.Ship(ctx, headstore.Over(anchors), w, headstore.DefaultCadence())
	require.NoError(t, err)
	// A second run whose anchored head the chain does not reproduce.
	w2, err := agentrun.Open(ctx, journal, agentrun.Run{Id: 12, Cell: "c", OwnerApp: "app.chat"})
	require.NoError(t, err)
	_, err = w2.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "more", Turn: "t1"})
	require.NoError(t, err)
	_, err = anchors.Append(ctx, anchor.Head{Cell: "c", Scope: 12, Ordinal: 1, Scheme: agentrun.ChainScheme.Name(), Digest: "forged"})
	require.NoError(t, err)

	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Log: zerolog.Nop(), AgentRuns: journal, Anchors: anchors,
		Trail: sealVerdict{unanchored: []uint64{3}}, Run: "run-x"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
	p, ok := reg.Lookup(TableEvidenceChecks)
	require.True(t, ok, "tables = %v", reg.Names())
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 14, rec.NumRows(), "six laws per journal run, two per trail run")

	type row struct {
		store, scope, law, why string
		holds                  bool
		broken                 uint64
	}
	rows := make(map[string]row)
	for i := 0; i < int(rec.NumRows()); i++ {
		r := row{
			store: rec.Column(0).(*array.String).Value(i), scope: rec.Column(1).(*array.String).Value(i),
			law: rec.Column(2).(*array.String).Value(i), holds: rec.Column(3).(*array.Boolean).Value(i),
			broken: rec.Column(4).(*array.Uint64).Value(i), why: rec.Column(5).(*array.String).Value(i),
		}
		rows[r.store+"/"+r.scope+"/"+r.law] = r
	}
	assert.True(t, rows["journal/11/L31"].holds)
	assert.True(t, rows["journal/11/L32"].holds)
	assert.True(t, rows["journal/12/L31"].holds, "the run's own chain holds")
	l32 := rows["journal/12/L32"]
	assert.False(t, l32.holds, "the anchored head is not what the chain reproduces")
	assert.EqualValues(t, 1, l32.broken)
	assert.Contains(t, l32.why, "heads break at 1")
	assert.True(t, rows["trail/run-x/L34"].holds)
	anchorsRow := rows["trail/run-x/L32"]
	assert.False(t, anchorsRow.holds)
	assert.EqualValues(t, 3, anchorsRow.broken)
	assert.Contains(t, anchorsRow.why, "anchored seals break at 3")
}

// Without a journal store, an anchor store or a recorder the relation is
// present and empty, so the table names do not depend on what was wired.
func TestEvidenceChecksPresentWithoutStores(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")
	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
	p, ok := reg.Lookup(TableEvidenceChecks)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 0, rec.NumRows())
}
