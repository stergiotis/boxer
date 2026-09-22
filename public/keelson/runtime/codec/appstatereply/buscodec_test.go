package appstatereply_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstatereply"
)

func TestBuscodecAutoRegistersAppStateReply(t *testing.T) {
	got := buscodec.Lookup[appstatereply.AppStateReply]()
	require.Equal(t, "appStateReply-sparse-cbor", got.Name())
}

// The per-kind outcome columns survive zipped, and a refusal carries its
// reason with no outcomes at all.
func TestBuscodecRoundTrip(t *testing.T) {
	orig := appstatereply.AppStateReply{
		Ok: false, Reason: "1 of 4 entries could not be cleared",
		Kind:    []string{"column_width", "persist", "workingset"},
		Cleared: []uint64{2, 0, 1},
		Failed:  []uint64{0, 1, 0},
	}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[appstatereply.AppStateReply](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Ok, got.Ok)
	assert.Equal(t, orig.Reason, got.Reason)
	assert.Equal(t, orig.Kind, got.Kind)
	assert.Equal(t, orig.Cleared, got.Cleared)
	assert.Equal(t, orig.Failed, got.Failed)

	refusal := appstatereply.AppStateReply{Reason: "appstate: no durable state store in this process"}
	wire, err = buscodec.Encode(refusal)
	require.NoError(t, err)
	got, err = buscodec.Decode[appstatereply.AppStateReply](wire)
	require.NoError(t, err)
	assert.False(t, got.Ok)
	assert.Equal(t, refusal.Reason, got.Reason)
	assert.Empty(t, got.Kind)
}
