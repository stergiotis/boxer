package introspecthost

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
)

type callsOf []llm.CallRecord

func (inst callsOf) Calls() []llm.CallRecord { return inst }

type copiesOf []clipboardbroker.CopyRecord

func (inst copiesOf) Records() []clipboardbroker.CopyRecord { return inst }

// The exits' laws judge the calls by a declared remote route — an amber
// call with no approval breaks both, a green one and a call to a route the
// declaration does not list are not judged — the copy law judges the
// broker's records, and a compaction whose from is not zero breaks L22.
func TestLawChecksOverLiveRows(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")
	ctx := context.Background()
	journal := agentrun.NewMemStore()
	w, err := agentrun.Open(ctx, journal, agentrun.Run{Id: 21, Cell: "c", OwnerApp: "app.chat", Conversation: "conv"})
	require.NoError(t, err)
	_, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindUser, Body: "hello", Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindUser, Body: "again", Turn: "t2"},
		agentrun.Entry{Kind: agentrun.KindCompact, From: 1, Target: 2, Body: "summary", Turn: "t2"})
	require.NoError(t, err)

	decl := cell.Defaults()
	decl.ModelHost = "api.example"
	now := time.Now()
	calls := callsOf{
		{Id: 1, CallId: "c1", At: now, EndpointHost: "api.example", Conversation: "conv", Tier: uint8(agentrun.TierGreen)},
		{Id: 2, CallId: "c2", At: now, EndpointHost: "api.example", Conversation: "conv", Tier: uint8(agentrun.TierAmber)},
		{Id: 3, CallId: "c3", At: now, EndpointHost: "localhost", Conversation: "conv", Tier: uint8(agentrun.TierRed)},
	}
	copies := copiesOf{{Digest: "d", Tier: uint8(agentrun.TierAmber), Plaintext: true}}

	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Log: zerolog.Nop(), AgentRuns: journal, LLMCalls: calls, Clipboard: copies,
		Cell: func() cell.Declaration { return decl }})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
	p, ok := reg.Lookup(TableEvidenceChecks)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	type row struct {
		holds  bool
		broken uint64
		why    string
	}
	rows := map[string]row{}
	for i := 0; i < int(rec.NumRows()); i++ {
		key := rec.Column(0).(*array.String).Value(i) + "/" + rec.Column(1).(*array.String).Value(i) + "/" + rec.Column(2).(*array.String).Value(i)
		rows[key] = row{holds: rec.Column(3).(*array.Boolean).Value(i), broken: rec.Column(4).(*array.Uint64).Value(i), why: rec.Column(5).(*array.String).Value(i)}
	}
	for _, law := range []string{"L14", "L33"} {
		r, ok := rows["exits/"+cell.SinkKindModelRoute.String()+"/"+law]
		require.True(t, ok, law)
		assert.False(t, r.holds, law)
		assert.EqualValues(t, 2, r.broken, "%s: the amber call", law)
		assert.Contains(t, r.why, "1 of 2 exits", "%s: the local call is no exit", law)
	}
	l20 := rows["clipboard/clipboard/L20"]
	assert.False(t, l20.holds, "amber plaintext with no recorded exit")
	l22 := rows["journal/21/L22"]
	assert.False(t, l22.holds)
	assert.EqualValues(t, 3, l22.broken, "the compact entry's ordinal")
	for _, law := range []string{"L2", "L23", "L25"} {
		_, present := rows["journal/21/"+law]
		assert.True(t, present, "%s has a row per run", law)
	}
}
