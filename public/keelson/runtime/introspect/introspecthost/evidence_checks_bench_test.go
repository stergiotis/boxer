package introspecthost

import (
	"context"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// BenchmarkEvidenceChecksJournal is one snapshot of the relation over a
// journal of one run and a hundred entries with an anchored head: the
// journal's laws L31 and L32 and the compaction laws, as a SQL reader of
// keelson('evidence_checks') pays them per run.
func BenchmarkEvidenceChecksJournal(b *testing.B) {
	Enabled.SetForTest(b, "true")
	introspect.SetLocalQueryEndpoint("")
	ctx := context.Background()
	journal := agentrun.NewMemStore()
	anchors := anchor.NewMemStore()
	w, err := agentrun.Open(ctx, journal, agentrun.Run{Id: 11, Cell: "c", OwnerApp: "app.chat"})
	require.NoError(b, err)
	for i := range 100 {
		_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "entry " + strconv.Itoa(i), Turn: "t1"})
		require.NoError(b, err)
	}
	_, err = headstore.Ship(ctx, headstore.Over(anchors), w, headstore.DefaultCadence())
	require.NoError(b, err)
	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Log: zerolog.Nop(), AgentRuns: journal, Anchors: anchors})
	require.NoError(b, err)
	b.Cleanup(func() { _ = stop(context.Background()) })
	p, ok := reg.Lookup(TableEvidenceChecks)
	require.True(b, ok)
	b.ReportAllocs()
	for b.Loop() {
		rec, err := p.Snapshot(introspect.AllColumns())
		if err != nil {
			b.Fatal(err)
		}
		rec.Release()
	}
}
