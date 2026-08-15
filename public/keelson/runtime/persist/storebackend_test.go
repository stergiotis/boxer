package persist

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// newStoreBackend opens a backend over a fresh clickhouse-local location.
// The suite was written to mirror the facts-bound predecessor's case for
// case (that backend is gone since 2026-08-15): the migration ADR-0105 D3a
// describes only holds if the replacement answers the same contract, so
// the cheapest way to say that is to ask it the same questions.
func newStoreBackend(t *testing.T) (b *StoreBackend) {
	t.Helper()
	return openStoreBackendAt(t, t.TempDir())
}

func openStoreBackendAt(t *testing.T, location string) (b *StoreBackend) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(location, nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	b, err = OpenStoreBackend(context.Background(), exec, nil)
	require.NoError(t, err)
	t.Cleanup(b.Close)
	return
}

func TestOpenStoreBackend_NilExecutor(t *testing.T) {
	b, err := OpenStoreBackend(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Nil(t, b)
}

func TestStoreBackend_GetSetDelete_RoundTrip(t *testing.T) {
	b := newStoreBackend(t)
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

// A write after a delete must resurrect the key. Where the facts backend
// had to beat "a tombstone exists" in hand-written SQL, here the state
// view answers it — but the behaviour under test is the same one.
func TestStoreBackend_SetAfterDelete_Resurrects(t *testing.T) {
	b := newStoreBackend(t)
	ref := aliasRef("play")

	require.NoError(t, b.Set(ref, "tabs", []byte("v1")))
	require.NoError(t, b.Delete(ref, "tabs"))
	require.NoError(t, b.Set(ref, "tabs", []byte("v2")))

	got, found, err := b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(got))
}

func TestStoreBackend_DeleteAbsentKey(t *testing.T) {
	b := newStoreBackend(t)
	require.NoError(t, b.Delete(aliasRef("play"), "never-written"))
}

// An empty value is a value: found=true with a zero-length slice, distinct
// from absent. play relies on this to tell "the user cleared the buffer"
// from "nothing was ever saved".
func TestStoreBackend_EmptyValue_RoundTrips(t *testing.T) {
	b := newStoreBackend(t)
	ref := aliasRef("play")

	require.NoError(t, b.Set(ref, "tabs", []byte{}))

	got, found, err := b.Get(ref, "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Empty(t, got)
}

func TestStoreBackend_AliasSeparation(t *testing.T) {
	b := newStoreBackend(t)
	require.NoError(t, b.Set(aliasRef("play"), "tabs", []byte("p")))
	require.NoError(t, b.Set(aliasRef("imztop"), "tabs", []byte("i")))

	got, _, _ := b.Get(aliasRef("play"), "tabs")
	assert.Equal(t, "p", string(got))
	got, _, _ = b.Get(aliasRef("imztop"), "tabs")
	assert.Equal(t, "i", string(got))
}

// The composite key is built from the durable AppId, not the alias, so an
// adopter whose id is "elle/factsplay" addressing the alias "factsplay"
// keys on the id. Recording the alias would look correct for every app
// whose id equals its alias and silently diverge for the ones that adopt.
func TestStoreBackend_KeysOnAttributedAppId(t *testing.T) {
	b := newStoreBackend(t)
	adopted := StorageRef{Alias: "factsplay", AppId: app.AppIdT("elle/factsplay")}

	require.NoError(t, b.Set(adopted, "tabs", []byte("adopted")))

	// Same alias, no attribution: StateAppId falls back to the alias, which
	// is a different key — the two must not collide.
	got, found, err := b.Get(StorageRef{Alias: "factsplay"}, "tabs")
	require.NoError(t, err)
	assert.False(t, found, "unattributed ref must not read the adopted app's row")
	assert.Nil(t, got)

	got, found, err = b.Get(adopted, "tabs")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "adopted", string(got))
}

// An unattributed ref must still read back its own write — the fallback
// loses the join, not the state.
func TestStoreBackend_UnattributedRef_ReadsBackItsOwnWrite(t *testing.T) {
	b := newStoreBackend(t)
	ref := StorageRef{Alias: "solo"}

	require.NoError(t, b.Set(ref, "k", []byte("v")))

	got, found, err := b.Get(ref, "k")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "v", string(got))
}

// The whole point of D3a over the memory backend: state outlives the
// process. Reopening the same location must find the rows — MergeTree
// makes them durable across clickhouse-local's one-shot processes.
func TestStoreBackend_SurvivesReopen(t *testing.T) {
	location := t.TempDir()
	ref := aliasRef("play")

	b := openStoreBackendAt(t, location)
	require.NoError(t, b.Set(ref, "tabs", []byte("durable")))
	b.Close()

	reopened := openStoreBackendAt(t, location)
	got, found, err := reopened.Get(ref, "tabs")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "durable", string(got))
}

// A delete must survive a reopen too: the tombstone is a row like any
// other, and the state view has to keep reading it as absent once the
// cache that served it is gone.
func TestStoreBackend_DeleteSurvivesReopen(t *testing.T) {
	location := t.TempDir()
	ref := aliasRef("play")

	b := openStoreBackendAt(t, location)
	require.NoError(t, b.Set(ref, "tabs", []byte("v1")))
	require.NoError(t, b.Delete(ref, "tabs"))
	b.Close()

	reopened := openStoreBackendAt(t, location)
	_, found, err := reopened.Get(ref, "tabs")
	require.NoError(t, err)
	assert.False(t, found)
}
