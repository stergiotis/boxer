package hostboot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/fairlead"
)

// A booted host refuses a task's write at the fairlead and lets the
// person's statement through (ADR-0403). The pass stays installed after the
// Runtime closes; the other tests of this package mark no statement.
func TestBoot_InstallsTheTaskReadPass(t *testing.T) {
	rt, err := Boot(context.Background(), minimalOptions(t, newTestRegistry(t)))
	require.NoError(t, err)
	defer rt.Close(t.Context())

	marked := fairlead.WithTask(context.Background(), "task-1")
	_, err = fairlead.Lead(marked, fairlead.ExitServer, "hostboot.test", "INSERT INTO t VALUES (1)")
	require.ErrorContains(t, err, "statement refused at the fairlead")
	out, err := fairlead.Lead(marked, fairlead.ExitServer, "hostboot.test", "SELECT 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", out)
	out, err = fairlead.Lead(context.Background(), fairlead.ExitServer, "hostboot.test", "INSERT INTO t VALUES (1)")
	require.NoError(t, err)
	require.Equal(t, "INSERT INTO t VALUES (1)", out)
}
