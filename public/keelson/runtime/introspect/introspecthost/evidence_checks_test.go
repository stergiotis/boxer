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
	notDurable string
}

func (inst sealVerdict) VerifySeals(context.Context, string) (evidence.SealBreak, []uint64, []uint64, error) {
	return inst.broken, inst.unnamed, inst.unanchored, nil
}

func (inst sealVerdict) NotDurable() string { return inst.notDurable }

// readEvidenceChecks serves the relation over deps and returns its rows
// keyed store/scope/law.
func readEvidenceChecks(t *testing.T, deps Deps) (rows map[string]checkRow) {
	reg := introspect.NewRegistry()
	deps.Registry, deps.Log = reg, zerolog.Nop()
	stop, err := Start(deps)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
	p, ok := reg.Lookup(TableEvidenceChecks)
	require.True(t, ok, "tables = %v", reg.Names())
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	rows = make(map[string]checkRow)
	for i := 0; i < int(rec.NumRows()); i++ {
		r := checkRow{
			store: rec.Column(0).(*array.String).Value(i), scope: rec.Column(1).(*array.String).Value(i),
			law: rec.Column(2).(*array.String).Value(i), holds: rec.Column(3).(*array.Boolean).Value(i),
			broken: rec.Column(4).(*array.Uint64).Value(i), why: rec.Column(5).(*array.String).Value(i),
		}
		rows[r.store+"/"+r.scope+"/"+r.law] = r
	}
	require.Len(t, rows, int(rec.NumRows()), "one row per store, scope and law")
	return
}

type checkRow struct {
	store, scope, law, why string
	holds                  bool
	broken                 uint64
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

	rows := readEvidenceChecks(t, Deps{AgentRuns: journal, Anchors: anchors,
		Trail: sealVerdict{unanchored: []uint64{3}}, Run: "run-x"})
	require.Len(t, rows, 15, "six laws per journal run, three per trail run")
	assert.True(t, rows["journal/11/L31"].holds)
	assert.True(t, rows["journal/11/L32"].holds)
	assert.True(t, rows["journal/12/L31"].holds, "the run's own chain holds")
	l32 := rows["journal/12/L32"]
	assert.False(t, l32.holds, "the anchored head is not what the chain reproduces")
	assert.EqualValues(t, 1, l32.broken)
	assert.Contains(t, l32.why, "heads break at 1")
	assert.True(t, rows["trail/run-x/"+LawTrailDurable].holds)
	assert.Empty(t, rows["trail/run-x/"+LawTrailDurable].why)
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

// A trail whose rows do not land is one row saying why — a host without
// ClickHouse, or one without a run id — and no seal laws, which have no
// stream to read.
func TestEvidenceChecksSayTheTrailIsNotDurable(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")
	rows := readEvidenceChecks(t, Deps{Trail: sealVerdict{notDurable: "the host has no run id, so the trail has no seal stream"}})
	require.Len(t, rows, 1)
	r := rows["trail//"+LawTrailDurable]
	assert.False(t, r.holds)
	assert.Equal(t, "the host has no run id, so the trail has no seal stream", r.why)

	rows = readEvidenceChecks(t, Deps{Trail: sealVerdict{notDurable: "this host has no durable backend for boxer.facts"}, Run: "run-y"})
	require.Len(t, rows, 1)
	assert.False(t, rows["trail/run-y/"+LawTrailDurable].holds)
}
