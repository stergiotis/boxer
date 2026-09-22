package persiststore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
)

// TestEntityIdOfInvertsEntryKeyOf: the key keelson('app_state') shows for an
// entry addresses that same entry again — including a column width whose
// scope contains "/", which a naive split would misplace.
func TestEntityIdOfInvertsEntryKeyOf(t *testing.T) {
	const app = "github.com/example/play"
	for _, ent := range []PersistEntity{
		{ID: StateKey(app, "tabs"), State: option.Some(State{Key: "tabs"})},
		{ID: WorkingsetKey(app, "default"), Workingset: option.Some(Workingset{Name: "default"})},
		{ID: ColumnWidthKey(app, "instance", "master/table", "ab12"), ColumnWidth: option.Some(ColumnWidth{Tier: "instance", Scope: "master/table", ColumnKey: "ab12"})},
		{ID: ColumnWidthKey(app, "column", "", "ab12"), ColumnWidth: option.Some(ColumnWidth{Tier: "column", ColumnKey: "ab12"})},
	} {
		id, err := EntityIdOf(KindOf(&ent), app, EntryKeyOf(&ent))
		require.NoError(t, err)
		assert.Equal(t, ent.ID, id)
	}
}

func TestEntityIdOfRefusesWhatItCannotAddress(t *testing.T) {
	_, err := EntityIdOf(KindUnknown, "a", "zz/a/x")
	assert.ErrorIs(t, err, ErrUnaddressableKind)
	_, err = EntityIdOf("no-such-kind", "a", "x")
	assert.ErrorIs(t, err, ErrUnaddressableKind)
	_, err = EntityIdOf(KindColumnWidth, "a", "just-one-segment")
	assert.Error(t, err)
	unknown := PersistEntity{ID: "zz/a/x", Owner: option.Some(Owner{AppId: "a"})}
	assert.Equal(t, KindUnknown, KindOf(&unknown))
	assert.Equal(t, "zz/a/x", EntryKeyOf(&unknown), "an unknown row is named by its store key")
}
