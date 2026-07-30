package persist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

func newFactsBackend(t *testing.T) (b *FactsBackend, facts *factsstore.InMemoryFactsStore) {
	t.Helper()
	facts = factsstore.NewInMemoryFactsStore()
	b, err := NewFactsBackend(facts)
	require.NoError(t, err)
	return
}

func TestNewFactsBackend_NilStore(t *testing.T) {
	b, err := NewFactsBackend(nil)
	require.Error(t, err)
	assert.Nil(t, b)
}

func TestFactsBackend_GetSetDelete_RoundTrip(t *testing.T) {
	b, _ := newFactsBackend(t)
	ref := aliasRef("play")

	value, found, err := b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, value)

	require.NoError(t, b.Set(ref, "tabs", []byte("hello")))

	got, found, err := b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("hello"), got)

	require.NoError(t, b.Delete(ref, "tabs"))

	_, found, err = b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.False(t, found)
}

// A write after a delete must resurrect the key. The store is append-only,
// so "latest row wins" has to beat "a tombstone exists" — the same trap
// ListWorkingsets hit with WHERE-vs-HAVING on the CH side.
func TestFactsBackend_SetAfterDelete_Resurrects(t *testing.T) {
	b, _ := newFactsBackend(t)
	ref := aliasRef("play")

	require.NoError(t, b.Set(ref, "tabs", []byte("v1")))
	require.NoError(t, b.Delete(ref, "tabs"))
	require.NoError(t, b.Set(ref, "tabs", []byte("v2")))

	got, found, err := b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(got))
}

// Deleting a key that was never written is not an error — MemoryBackend
// tolerates it and the persist contract documents it, so the durable
// backend must not start failing the call.
func TestFactsBackend_DeleteAbsentKey(t *testing.T) {
	b, _ := newFactsBackend(t)
	require.NoError(t, b.Delete(aliasRef("play"), "never-written"))
}

// An empty value is a value: Get must report found=true with a zero-length
// slice, distinct from an absent key. play relies on this to tell "the user
// cleared the buffer" from "nothing was ever saved".
func TestFactsBackend_EmptyValue_RoundTrips(t *testing.T) {
	b, _ := newFactsBackend(t)
	ref := aliasRef("play")

	require.NoError(t, b.Set(ref, "tabs", []byte{}))

	got, found, err := b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Empty(t, got)
}

func TestFactsBackend_AliasSeparation(t *testing.T) {
	b, _ := newFactsBackend(t)
	require.NoError(t, b.Set(aliasRef("play"), "tabs", []byte("p")))
	require.NoError(t, b.Set(aliasRef("imztop"), "tabs", []byte("i")))

	got, _, _ := b.Get(aliasRef("play"), "tabs")
	assert.Equal(t, "p", string(got))
	got, _, _ = b.Get(aliasRef("imztop"), "tabs")
	assert.Equal(t, "i", string(got))
}

// The point of threading StorageRef.AppId: the recorded row carries the
// app's durable id, so a state fact joins that app's grant / audit / launch
// facts on one column. An adopter whose id is "elle/factsplay" addresses
// the alias "factsplay"; recording the alias would silently break that join
// while looking correct for every app whose id happens to equal its alias.
func TestFactsBackend_RecordsAttributedAppId(t *testing.T) {
	b, facts := newFactsBackend(t)
	const id app.AppIdT = "elle/factsplay"
	ref := StorageRef{Alias: id.SubjectAlias(), AppId: id}
	require.Equal(t, "factsplay", ref.Alias)

	require.NoError(t, b.Set(ref, "lastSql", []byte("SELECT 1")))

	got, found, err := facts.LatestState(id, "lastSql")
	require.NoError(t, err)
	assert.True(t, found, "row must be recorded under the durable app id, not the alias")
	assert.Equal(t, "SELECT 1", string(got))
}

// Reads and writes must agree under the fallback too. An unattributed ref
// records under the alias promoted to an id; the same ref must read it
// back. This is the path a sender/subject-alias mismatch takes.
func TestFactsBackend_UnattributedRef_ReadsBackItsOwnWrite(t *testing.T) {
	b, facts := newFactsBackend(t)
	ref := aliasRef("factsplay")

	require.NoError(t, b.Set(ref, "lastSql", []byte("SELECT 2")))

	got, found, err := b.Get(ref, "lastSql")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "SELECT 2", string(got))

	// And it is not visible under some other app's durable id.
	_, found, err = facts.LatestState("elle/factsplay", "lastSql")
	require.NoError(t, err)
	assert.False(t, found)
}

// The synthetic service ids that have no registry entry must work. This is
// the case that rules out resolving alias→AppId through the app registry:
// the applet store is a runtime service, never a registered app, and it is
// the consumer whose non-durability prompted this backend (ADR-0132 O4).
func TestFactsBackend_SyntheticServiceId(t *testing.T) {
	b, _ := newFactsBackend(t)
	const id app.AppIdT = "runtime.appletstore"
	ref := StorageRef{Alias: id.SubjectAlias(), AppId: id}
	require.Equal(t, "runtime_appletstore", ref.Alias)

	require.NoError(t, b.Set(ref, "index", []byte("slug-a\nslug-b")))

	got, found, err := b.Get(ref, "index")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "slug-a\nslug-b", string(got))
}

func TestStorageRef_StateAppId(t *testing.T) {
	assert.Equal(t, app.AppIdT("elle/factsplay"),
		StorageRef{Alias: "factsplay", AppId: "elle/factsplay"}.StateAppId())
	assert.Equal(t, app.AppIdT("factsplay"),
		StorageRef{Alias: "factsplay"}.StateAppId())
}
