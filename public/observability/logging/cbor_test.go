package logging

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_embeddAsCbor(t *testing.T) {
	encMode, err := cbor.CanonicalEncOptions().EncMode()
	require.NoError(t, err)
	var decMode cbor.DecMode
	decMode, err = cbor.DecOptions{}.DecMode()
	require.NoError(t, err)
	a := []any{1.0, true, "test", []byte{0, 1, 2}}
	var b string
	b, err = embeddAsCbor(encMode, &a)
	require.NoError(t, err)
	require.True(t, containsEmbeddedCborJson(b))
	var aa any
	err = unmarshallEmbeddedCborJson(b, decMode, &aa)
	require.NoError(t, err)
	require.Equalf(t, a, aa, "not equal")
}
