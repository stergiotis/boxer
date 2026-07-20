package introspect

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	require.NoError(t, r.Register(NewEncryptedEntry("adhoc_x", schema, "id Int64", "/p/x.bxad", 1)))
	_, ok := r.Lookup("adhoc_x")
	assert.True(t, ok)

	assert.True(t, r.Unregister("adhoc_x"))
	_, ok = r.Lookup("adhoc_x")
	assert.False(t, ok)
	assert.False(t, r.Unregister("adhoc_x"), "second unregister is a no-op")
}

func TestEncryptedEntry(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	e := NewEncryptedEntry("adhoc_x", schema, "id Int64", "/p/x.bxad", 1)

	var _ EncryptedDatasetI = e // satisfies the marker interface
	assert.Equal(t, "adhoc_x", e.Name())
	assert.Equal(t, FreshnessLive, e.Freshness())
	assert.Equal(t, "id Int64", e.Structure())
	assert.Equal(t, "/p/x.bxad", e.Path())
	assert.Equal(t, uint64(1), e.Revision())
	assert.Equal(t, schema, e.Schema())

	_, err := e.Snapshot(AllColumns())
	require.Error(t, err, "an ad-hoc dataset never snapshots in process")

	schema2 := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.BinaryTypes.String}}, nil)
	e.Update(schema2, "v String", "/p/x2.bxad", 2)
	assert.Equal(t, "v String", e.Structure())
	assert.Equal(t, "/p/x2.bxad", e.Path())
	assert.Equal(t, uint64(2), e.Revision())
	assert.Equal(t, schema2, e.Schema())
}
