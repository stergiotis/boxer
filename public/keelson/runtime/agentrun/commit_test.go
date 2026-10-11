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
// content under two openings commits to two values: hiding and binding,
// as far as a test can say so (ADR-0320 §SD2).
func TestACommitmentOpensUnderItsOpeningOnly(t *testing.T) {
	a, b := NewOpening(), NewOpening()
	require.Len(t, a, OpeningSize)
	assert.NotEqual(t, a, b, "openings are random")
	c := Commit(a, FieldBody, "a name")
	assert.Len(t, c, 64)
	assert.True(t, Opens(a, FieldBody, "a name", c))
	assert.False(t, Opens(b, FieldBody, "a name", c), "another opening does not open it")
	assert.False(t, Opens(a, FieldCalls, "a name", c), "another field does not open it")
	assert.False(t, Opens(a, FieldBody, "a name ", c), "other content does not open it")
	assert.False(t, Opens(nil, FieldBody, "a name", c), "no opening does not open it")
	assert.NotEqual(t, c, Commit(b, FieldBody, "a name"), "the same content commits differently per opening")
	assert.Empty(t, Commit(a, FieldBody, ""), "empty content has no commitment")
	assert.True(t, Opens(nil, FieldBody, "", ""), "and opens as none")
	assert.False(t, Opens(nil, FieldBody, "", c), "an empty field with a commitment is erased, not open")
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

// Digested draws one opening per entry with content, commits each field
// under it, names the scheme, and leaves an entry without content bare;
// an erased entry keeps its commitments and nothing else.
func TestDigestedCommitsUnderOneOpening(t *testing.T) {
	fixedOpenings(t)
	e := Entry{Kind: KindReply, Body: "hi", Calls: "[]"}.Digested()
	require.Len(t, e.Opening, OpeningSize)
	assert.Equal(t, CommitScheme, e.KeyId)
	assert.Equal(t, Commit(e.Opening, FieldBody, "hi"), e.BodyDigest)
	assert.Equal(t, Commit(e.Opening, FieldCalls, "[]"), e.CallsDigest)
	assert.True(t, e.BodiesMatch())
	assert.False(t, e.Tombstoned())
	again := e.Digested()
	assert.Equal(t, e, again, "an entry with an opening keeps it")

	bare := Entry{Kind: KindTurn}.Digested()
	assert.Nil(t, bare.Opening)
	assert.Empty(t, bare.BodyDigest)
	assert.Empty(t, bare.KeyId)
	assert.True(t, bare.BodiesMatch())

	erased := e.Erased()
	assert.Empty(t, erased.Body)
	assert.Nil(t, erased.Opening)
	assert.Equal(t, e.BodyDigest, erased.BodyDigest, "the commitment stays")
	assert.True(t, erased.Tombstoned())
	assert.True(t, erased.BodiesMatch(), "an erased field matches: there is nothing to open")
	form1, err := e.CanonicalForm()
	require.NoError(t, err)
	form2, err := erased.CanonicalForm()
	require.NoError(t, err)
	assert.Equal(t, form1, form2, "the canonical form is the same with and without the content: the opening is not in it")
}
