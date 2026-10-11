package agentrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// fixedOpenings replaces the opening with a counter, so a test's
// commitments are reproducible; the cleanup restores the random one.
func fixedOpenings(t *testing.T) {
	t.Helper()
	was := evidence.NewOpening
	var n byte
	evidence.NewOpening = func() (opening []byte) {
		n++
		opening = make([]byte, evidence.OpeningSize)
		for i := range opening {
			opening[i] = n
		}
		return
	}
	t.Cleanup(func() { evidence.NewOpening = was })
}

// A prepared entry commits to its content one vault row per component,
// each under an opening of its own, names the scheme, and leaves an entry
// without content bare; an erased entry keeps its commitments and its
// ledger records and nothing else, and its canonical form is the one it
// had with content (ADR-0327 §SD4, §SD9).
func TestPreparedCommitsPerComponent(t *testing.T) {
	fixedOpenings(t)
	ctx := context.Background()
	e, err := Entry{Run: 7, Kind: KindReply, Body: "hi", Calls: `[{"Id":"c1","Name":"t","Arguments":"{}"}]`, Reply: &ReplyMeta{CallId: "x"}}.prepared(ctx, evidence.Head{Digest: "seed"})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), e.Ordinal)
	assert.Equal(t, ChainScheme.Name(), e.KeyId)
	require.Len(t, e.Openings, 2, "the prose and the shredded calls are two components")
	require.Len(t, e.Commitments, 2)
	assert.Len(t, e.Openings[componentBody], evidence.OpeningSize)
	assert.Len(t, e.Commitments[componentShredded], 64)
	assert.NotEqual(t, e.Openings[componentBody], e.Openings[componentShredded], "one opening per component")
	assert.Equal(t, `[{"Arguments":"{}","Id":"c1","Name":"t"}]`, e.Calls, "the calls are stored in canonical form")
	assert.True(t, e.BodiesMatch())
	assert.False(t, e.Tombstoned())
	require.NotNil(t, e.WrittenBy)
	assert.Equal(t, "self", e.WrittenBy.OriginBy)

	bare, err := Entry{Run: 7, Kind: KindTurn, TurnEnd: &TurnState{State: "answered"}}.prepared(ctx, evidence.Head{Ordinal: 1, Digest: e.Digest})
	require.NoError(t, err)
	assert.Empty(t, bare.Openings, "a ledger record is not content")
	assert.Empty(t, bare.Commitments)
	assert.Equal(t, ChainScheme.Name(), bare.KeyId, "the scheme is on every entry")
	assert.Equal(t, uint32(1), bare.Prev)
	assert.True(t, bare.BodiesMatch())

	erased := e.Erased()
	assert.Empty(t, erased.Body)
	assert.Empty(t, erased.Calls)
	assert.Nil(t, erased.Openings)
	assert.Equal(t, e.Commitments, erased.Commitments, "the commitments stay")
	assert.Equal(t, e.Reply, erased.Reply, "the ledger's record stays")
	assert.True(t, erased.Tombstoned())
	assert.True(t, erased.BodiesMatch(), "an erased entry matches: there is nothing to open")
	form1, err := e.CanonicalForm()
	require.NoError(t, err)
	form2, err := erased.CanonicalForm()
	require.NoError(t, err)
	assert.Equal(t, form1, form2, "the canonical form is the same with and without the content: the openings are not in it")
	assert.Equal(t, e.Digest, ChainDigest("seed", form1))
}

// A vault record is content: it is committed to, and changing it breaks
// the commitment; a tool call's arguments are committed to as their
// shredded attributes, so their spelling does not move the commitment.
func TestVaultRecordsAndArgumentsAreCommitted(t *testing.T) {
	fixedOpenings(t)
	e, err := Entry{Run: 7, Kind: KindEndorse, Endorse: &Endorsement{Digest: "d", Epoch: "t1", Classes: []string{"publish"}}}.prepared(context.Background(), evidence.Head{Digest: "seed"})
	require.NoError(t, err)
	require.Contains(t, e.Commitments, componentEndorse)
	tampered := e
	tampered.Endorse = &Endorsement{Digest: "other", Epoch: "t1", Classes: []string{"publish"}}
	assert.False(t, tampered.BodiesMatch(), "a changed record does not open the commitment")

	a, err := ArgumentsDigest(`{"b": 1, "a": [true, null]}`)
	require.NoError(t, err)
	b, err := ArgumentsDigest(`{"a":[true,null],"b":1}`)
	require.NoError(t, err)
	assert.Equal(t, a, b, "two spellings of the same arguments are one digest")
	c, err := ArgumentsDigest(`{"a":[true,null],"b":2}`)
	require.NoError(t, err)
	assert.NotEqual(t, a, c)
	text, err := ArgumentsDigest(`not json`)
	require.NoError(t, err)
	assert.NotEqual(t, a, text, "arguments that do not parse are hashed as text")
}
