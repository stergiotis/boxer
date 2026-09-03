package play

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIdentity_JobFillsEveryRow pins the Table job: one drive over the whole
// record, cells that read "…" until it lands and the row's hex after, the
// same values the strip computes for a one-row slice, one job per ResultID.
func TestIdentity_JobFillsEveryRow(t *testing.T) {
	rec := factsRecord(t)
	cards := factsCards(t)
	app := &PlayApp{cards: cards}
	app.tableOpts.showHashes = true
	require.Equal(t, []int{rec.Schema().NumFields(), rec.Schema().NumFields() + 1}, app.identitySynthCols(rec.Schema()))
	app.tableOpts.showHashes = false
	require.Nil(t, app.identitySynthCols(rec.Schema()))

	job := app.ensureIdentityJob(rec, 3)
	require.Same(t, job, app.ensureIdentityJob(rec, 3), "one job per result")
	deadline := time.Now().Add(5 * time.Second)
	for {
		text, _ := job.cell(identityColCanonform, 0)
		if text != "…" {
			break
		}
		require.True(t, time.Now().Before(deadline), "job did not land")
		time.Sleep(2 * time.Millisecond)
	}
	comp, err := newIdentityComputer(cards.TableDesc(), cards.IR(), cards.Driver())
	require.NoError(t, err)
	for row := range int64(2) {
		v, err := comp.row(rec, row)
		require.NoError(t, err)
		text, hover := job.cell(identityColCanonform, row)
		require.Equal(t, hex.EncodeToString(v.canon[:]), hover)
		require.Equal(t, hover[:identityCellRunes], text)
		text, hover = job.cell(identityColCanonwire, row)
		require.Equal(t, hex.EncodeToString(v.wire[:]), hover)
		require.Equal(t, hover[:identityCellRunes], text)
	}
	text, _ := job.cell(identityColCanonform, 99)
	require.Equal(t, "", text, "a row past the result is blank")

	next := app.ensureIdentityJob(rec, 4)
	require.NotSame(t, job, next, "a new result starts a new job")
	app.identityJob.stop()
}
