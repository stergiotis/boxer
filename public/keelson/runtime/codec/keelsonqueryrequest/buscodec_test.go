package keelsonqueryrequest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryrequest"
)

func TestBuscodecAutoRegistersKeelsonQueryRequest(t *testing.T) {
	got := buscodec.Lookup[keelsonqueryrequest.KeelsonQueryRequest]()
	require.Equal(t, "keelsonQueryRequest-sparse-cbor", got.Name())
}

// The statement survives with its quoting and newlines, and an empty
// format comes back empty rather than as a default the wire invented.
func TestBuscodecRoundTrip(t *testing.T) {
	orig := keelsonqueryrequest.KeelsonQueryRequest{
		Table:  "watchbill_event",
		Sql:    "SELECT job_id, at\nFROM keelson('watchbill_event') WHERE job_id = 'j\\'1'",
		Format: "JSONEachRow",
	}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[keelsonqueryrequest.KeelsonQueryRequest](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Table, got.Table)
	assert.Equal(t, orig.Sql, got.Sql)
	assert.Equal(t, orig.Format, got.Format)

	bare := keelsonqueryrequest.KeelsonQueryRequest{Table: "env", Sql: "SELECT count() FROM env"}
	wire, err = buscodec.Encode(bare)
	require.NoError(t, err)
	got, err = buscodec.Decode[keelsonqueryrequest.KeelsonQueryRequest](wire)
	require.NoError(t, err)
	assert.Equal(t, "", got.Format)
}
