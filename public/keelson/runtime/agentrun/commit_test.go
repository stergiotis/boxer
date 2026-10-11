package agentrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedOpenings replaces the opening with a counter, so a test's
// commitments are reproducible; the cleanup restores the random one.
func fixedOpenings(t *testing.T) {
	t.Helper()
	was := NewOpening
	var n byte
	NewOpening = func() (opening []byte) {
		n++
		opening = make([]byte, OpeningSize)
		for i := range opening {
			opening[i] = n
		}
		return
	}
	t.Cleanup(func() { NewOpening = was })
}

// A commitment opens under its opening and under no other, and the same
// item under two openings commits to two values: hiding and binding, as
// far as a test can say so (ADR-0325 §SD3).
func TestACommitmentOpensUnderItsOpeningOnly(t *testing.T) {
	a, b := NewOpening(), NewOpening()
	require.Len(t, a, OpeningSize)
	assert.NotEqual(t, a, b, "openings are random")
	item := []byte("a vault row")
	c := CommitVault(a, item)
	assert.Len(t, c, 64)
	assert.True(t, OpensVault(a, item, c))
	assert.False(t, OpensVault(b, item, c), "another opening does not open it")
	assert.False(t, OpensVault(a, []byte("another row"), c), "another item does not open it")
	assert.False(t, OpensVault(nil, item, c), "no opening does not open it")
	assert.False(t, OpensVault(a, item, ""), "no commitment is not opened")
	assert.NotEqual(t, c, CommitVault(b, item), "the same item commits differently per opening")
}

// The opening's text form round-trips, and a malformed text is no
// opening.
func TestTheOpeningTextRoundTrips(t *testing.T) {
	o := NewOpening()
	assert.Equal(t, o, OpeningOf(OpeningText(o)))
	assert.Nil(t, OpeningOf(""))
	assert.Nil(t, OpeningOf("zz"))
	assert.Nil(t, OpeningOf("00"), "the wrong length is no opening")
}

// Digested draws one opening per entry with content, commits the vault
// row under it, names the scheme, and leaves an entry without content
// bare; an erased entry keeps its commitment and its ledger records and
// nothing else, and its canonical form is the one it had with content.
func TestDigestedCommitsUnderOneOpening(t *testing.T) {
	fixedOpenings(t)
	e, err := Entry{Run: "r", Ordinal: 1, Kind: KindReply, Body: "hi", Calls: `[{"Id":"c1","Name":"t","Arguments":"{}"}]`, Reply: &ReplyMeta{CallId: "x"}}.Digested()
	require.NoError(t, err)
	require.Len(t, e.Opening, OpeningSize)
	assert.Equal(t, CommitScheme, e.KeyId)
	assert.Len(t, e.VaultCommitment, 64)
	assert.Equal(t, `[{"Arguments":"{}","Id":"c1","Name":"t"}]`, e.Calls, "the calls are stored in canonical form")
	assert.True(t, e.BodiesMatch())
	assert.False(t, e.Tombstoned())
	again, err := e.Digested()
	require.NoError(t, err)
	assert.Equal(t, e, again, "an entry with an opening keeps it")

	bare, err := Entry{Run: "r", Ordinal: 2, Kind: KindTurn, TurnEnd: &TurnState{State: "answered"}}.Digested()
	require.NoError(t, err)
	assert.Nil(t, bare.Opening, "a ledger record is not content")
	assert.Empty(t, bare.VaultCommitment)
	assert.Empty(t, bare.KeyId)
	assert.True(t, bare.BodiesMatch())

	erased := e.Erased()
	assert.Empty(t, erased.Body)
	assert.Empty(t, erased.Calls)
	assert.Nil(t, erased.Opening)
	assert.Equal(t, e.VaultCommitment, erased.VaultCommitment, "the commitment stays")
	assert.Equal(t, e.Reply, erased.Reply, "the ledger's record stays")
	assert.True(t, erased.Tombstoned())
	assert.True(t, erased.BodiesMatch(), "an erased entry matches: there is nothing to open")
	form1, err := e.CanonicalForm()
	require.NoError(t, err)
	form2, err := erased.CanonicalForm()
	require.NoError(t, err)
	assert.Equal(t, form1, form2, "the canonical form is the same with and without the content: the opening is not in it")
}

// A vault record is content: it is committed to, and changing it breaks
// the commitment; a tool call's arguments are committed to as their
// shredded attributes, so their spelling does not move the commitment.
func TestVaultRecordsAndArgumentsAreCommitted(t *testing.T) {
	fixedOpenings(t)
	e, err := Entry{Run: "r", Ordinal: 3, Kind: KindEndorse, Endorse: &Endorsement{Digest: "d", Epoch: "t1", Classes: []string{"publish"}}}.Digested()
	require.NoError(t, err)
	require.NotEmpty(t, e.VaultCommitment)
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
