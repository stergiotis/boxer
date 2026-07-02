package naturalkey

import (
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// buildAcctKey returns an encoder carrying a fixed composite natural key.
func buildAcctKey() *Encoder {
	enc := NewEncoder()
	enc.Begin()
	enc.AddStr("acct")
	enc.AddUint64(42)
	return enc
}

// TestEncoderEndAndGenerate_WithMemInternalizer exercises the EndAndGenerate /
// EndAndGenerate2 paths (idGen.GetId over the serialized key) with a concrete
// in-memory generator, which previously had no implementation to test against.
func TestEncoderEndAndGenerate_WithMemInternalizer(t *testing.T) {
	const tagVal = identifier.TagValue(5)
	gen, err := mem.NewIdInternalizer(tagVal, 8)
	require.NoError(t, err)

	// First generation mints a fresh, valid, correctly-tagged id.
	id1, fresh1, err := buildAcctKey().EndAndGenerate(gen, SerializationFormatCbor)
	require.NoError(t, err)
	require.True(t, fresh1)
	require.True(t, id1.IsValid())
	require.EqualValues(t, tagVal, id1.GetTag().GetValue())

	// The identical key resolves to the same id (not fresh). EndAndGenerate2 also
	// returns the serialized key it fed to the generator.
	id2, key2, fresh2, err := buildAcctKey().EndAndGenerate2(gen, SerializationFormatCbor)
	require.NoError(t, err)
	require.False(t, fresh2)
	require.Equal(t, id1, id2)

	// Feeding that serialized key straight to the generator agrees.
	idDirect, freshDirect, err := gen.GetId(key2)
	require.NoError(t, err)
	require.False(t, freshDirect)
	require.Equal(t, id1, idDirect)

	// A different composite key gets a different id.
	enc := NewEncoder()
	enc.Begin()
	enc.AddStr("other")
	id3, fresh3, err := enc.EndAndGenerate(gen, SerializationFormatCbor)
	require.NoError(t, err)
	require.True(t, fresh3)
	require.NotEqual(t, id1, id3)

	require.Equal(t, 2, gen.Len())
}
