package agentrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// A verdict entry (ADR-0340 §SD4) keeps its record on the ledger row and
// its assertion and observed value in the vault: the record round-trips
// through the ledger entity, the content is committed, and the wire form
// carries both.
func TestAVerdictEntryRoundTrips(t *testing.T) {
	fixedOpenings(t)
	in := Entry{Run: 7, Ordinal: 3, Kind: KindVerdict, Key: "k", Name: "a1", Form: "check", Turn: "t1", Body: "12.4",
		Arguments: `{"expect":"12 %","id":"a1","says":"it fell 12 %","statement":"SELECT 1 FROM t"}`,
		Verdict:   &Verdict{Form: "value", Outcome: "held", RestsOn: []string{"c-1", "c-2"}, Result: 9, Digest: "d", Source: "s", Endpoint: "clickhouse:h", Reason: ""}}
	ent, err := ledgerEntity(in)
	require.NoError(t, err)
	require.True(t, ent.VerdictRecord.Has)
	back := entryOfLedger(ent)
	assert.Equal(t, in.Verdict, back.Verdict)
	assert.Empty(t, back.Body, "the ledger row holds no content")

	e, err := in.prepared(context.Background(), evidence.Head{Digest: "seed"})
	require.NoError(t, err)
	assert.Contains(t, e.Commitments, componentBody, "the observed value is committed")
	assert.Contains(t, e.Commitments, componentShredded, "the assertion is committed")
	tampered := e
	tampered.Body = "12.6"
	assert.False(t, tampered.BodiesMatch())

	w := wireOfEntry(e)
	assert.Equal(t, in.Verdict, w.Verdict)
	assert.Equal(t, in.Verdict, entryOfWire(w).Verdict)
	assert.Contains(t, in.RecordBody(), `"outcome":"held"`)
	rows := VerdictRows([]Entry{in, {Run: 7, Ordinal: 5, Kind: KindReply, Turn: "t1", Body: "It fell 12 % [a1]."}})
	require.Len(t, rows, 1)
	assert.Equal(t, "it fell 12 %", rows[0].Says)
	assert.Equal(t, "12 %", rows[0].Expect)
	assert.Equal(t, uint32(5), rows[0].Reply)
}
