package keelsonqueryreply_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryreply"
)

func TestBuscodecAutoRegistersKeelsonQueryReply(t *testing.T) {
	got := buscodec.Lookup[keelsonqueryreply.KeelsonQueryReply]()
	require.Equal(t, "keelsonQueryReply-sparse-cbor", got.Name())
}

// A result body survives byte for byte, and a refusal carries its reason
// with no body at all.
func TestBuscodecRoundTrip(t *testing.T) {
	orig := keelsonqueryreply.KeelsonQueryReply{
		Ok: true, ContentType: "application/x-ndjson",
		Body: []byte("{\"job_id\":\"j1\"}\n{\"job_id\":\"j2\"}\n"),
	}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[keelsonqueryreply.KeelsonQueryReply](wire)
	require.NoError(t, err)
	assert.True(t, got.Ok)
	assert.Equal(t, orig.ContentType, got.ContentType)
	assert.Equal(t, orig.Body, got.Body)
	assert.Equal(t, "", got.Reason)

	refusal := keelsonqueryreply.KeelsonQueryReply{Reason: "keelson.query: the statement names env, and the subject grants watchbill_event"}
	wire, err = buscodec.Encode(refusal)
	require.NoError(t, err)
	got, err = buscodec.Decode[keelsonqueryreply.KeelsonQueryReply](wire)
	require.NoError(t, err)
	assert.False(t, got.Ok)
	assert.Equal(t, refusal.Reason, got.Reason)
	assert.Empty(t, got.Body)
}
