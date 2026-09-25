package chrows

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/require"
)

// recordOf reads the one batch of an Arrow IPC stream.
func recordOf(t *testing.T, body []byte) (rec arrow.RecordBatch) {
	rdr, err := ipc.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(rdr.Release)
	require.True(t, rdr.Next())
	rec = rdr.RecordBatch()
	return
}
