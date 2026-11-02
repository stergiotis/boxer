package hostboot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
)

// bootRunRow boots a minimal host and returns the run row it wrote.
func bootRunRow(t *testing.T) (row factsstore.RuntimeStartRow) {
	t.Helper()
	opts := minimalOptions(t, newTestRegistry(t))
	facts := factsstore.NewInMemoryFactsStore()
	opts.Facts = facts
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	t.Cleanup(rt.Close)
	runs := facts.Runs()
	require.Len(t, runs, 1)
	return runs[0]
}

func TestBoot_AHostWithoutATakeStampsTheHost(t *testing.T) {
	row := bootRunRow(t)
	assert.Equal(t, factsstore.DriverHost, row.Driver)
	assert.Empty(t, row.DriverJob)
	assert.Zero(t, row.DriverAttempt)
	assert.Empty(t, row.DriverJobKind)
}

func TestBoot_AHostStartedForATakeStampsIt(t *testing.T) {
	runinfo.RunDriverJob.SetForTest(t, "seco-dq/report/take-4")
	runinfo.RunDriverAttempt.SetForTest(t, "2")
	runinfo.RunDriverJobKind.SetForTest(t, "suite v3")
	row := bootRunRow(t)
	assert.Equal(t, factsstore.DriverBench, row.Driver)
	assert.Equal(t, "seco-dq/report/take-4", row.DriverJob)
	assert.Equal(t, uint32(2), row.DriverAttempt)
	assert.Equal(t, "suite v3", row.DriverJobKind)
}

func TestBoot_ATakeTheRowCannotCarryRefusesTheBoot(t *testing.T) {
	runinfo.RunDriverJob.SetForTest(t, "s/t/1")
	runinfo.RunDriverAttempt.SetForTest(t, "two")
	rt, err := Boot(context.Background(), minimalOptions(t, newTestRegistry(t)))
	assert.Nil(t, rt)
	assert.ErrorContains(t, err, "driver stamp")
}
