package appstaterequest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstaterequest"
)

func TestBuscodecAutoRegistersAppStateRequest(t *testing.T) {
	got := buscodec.Lookup[appstaterequest.AppStateRequest]()
	require.Equal(t, "appStateRequest-sparse-cbor", got.Name())
}

// Every field survives the wire, for both verbs' shapes.
func TestBuscodecRoundTrip(t *testing.T) {
	for _, orig := range []appstaterequest.AppStateRequest{
		{At: time.Unix(0, 1_700_000_000_000_000_000).UTC(), Op: appstaterequest.OpDelete,
			AppId: "github.com/example/play", Kind: "column_width", Key: "instance/master/table/ab12"},
		{Op: appstaterequest.OpDelete, AppId: "github.com/example/play", Kind: "unknown", EntityId: "zz/github.com%2Fexample%2Fplay/x"},
		{Op: appstaterequest.OpForget, AppId: "github.com/example/tally"},
	} {
		wire, err := buscodec.Encode(orig)
		require.NoError(t, err)
		got, err := buscodec.Decode[appstaterequest.AppStateRequest](wire)
		require.NoError(t, err)
		assert.Equal(t, orig.Op, got.Op)
		assert.Equal(t, orig.AppId, got.AppId)
		assert.Equal(t, orig.Kind, got.Kind)
		assert.Equal(t, orig.Key, got.Key)
		assert.Equal(t, orig.EntityId, got.EntityId)
	}
}
