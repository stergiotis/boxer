package chexec

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The context's identity reaches the process as its log_comment setting
// (ADR-0295 §SD3), and a bare context sets none.
func TestLocalExecutor_StampsLogComment(t *testing.T) {
	exec, err := NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	read := func(ctx context.Context) (lc string) {
		const sql = "SELECT getSetting('log_comment') AS lc SETTINGS output_format_arrow_string_as_string=1"
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			require.NoError(t, qerr)
			lc = rec.Column(0).(*array.String).Value(0)
			rec.Release()
		}
		return
	}
	assert.Equal(t, "", read(context.Background()))

	ci := callident.CallIdentity{Claims: callident.Claims{Principal: "p:local", Purpose: "test"}}
	ctx := recordstore.WithBatchId(callident.WithCallIdentity(context.Background(), ci), "B-local")
	st, ok := logcomment.Parse(read(ctx))
	require.True(t, ok)
	assert.Equal(t, logcomment.FromCallIdentity(ci, "B-local"), st)
}
