//go:build integration

package watchbill

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillpresence"
)

// A started and a stopped row round-trip through the facts-bound store on
// the server the CLICKHOUSE_* variables name, and the reader folds them
// (ADR-0237, verification plan). chstore provisions the table, as the host
// does; the store itself runs no DDL.
func TestPresenceRoundTripOnTheServer(t *testing.T) {
	ctx := context.Background()
	exec := serverExec(t)
	facts, isCh := chstore.NewWithFallback(chstore.ConfigFromEnv(), zerolog.Nop(), 2*time.Second)
	require.True(t, isCh, "the facts store must reach the server the executor reaches")
	_ = facts

	pres := NewPresence(exec)
	defer pres.Close()
	runId := "presence-it-" + time.Now().UTC().Format("150405.000")
	started := Status{RunId: runId, Kinds: []string{"it.kind"}, Queues: []string{"bulk"}, MaxWorkers: 3}
	require.NoError(t, pres.Started(ctx, started))

	rows, err := pres.ListPresence(ctx, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	var mine []watchbillpresence.WorkerPresence
	for _, r := range rows {
		if r.RunId == runId {
			mine = append(mine, r)
		}
	}
	require.Len(t, mine, 1)
	assert.Equal(t, watchbillpresence.PhaseStarted, mine[0].Phase)
	assert.Equal(t, []string{"it.kind"}, mine[0].Kinds)
	assert.Equal(t, []string{"bulk"}, mine[0].Queues)
	assert.EqualValues(t, 3, mine[0].MaxWorkers)
	assert.NotEmpty(t, mine[0].Host)

	workers, err := FoldWorkers(ctx, rows, time.Now(), 90*time.Second, nil)
	require.NoError(t, err)
	found := false
	for _, w := range workers {
		if w.RunId == runId {
			found = true
			assert.True(t, w.Alive, "unstopped and no liveness source")
		}
	}
	require.True(t, found)

	require.NoError(t, pres.Stopped(ctx, started))
	rows, err = pres.ListPresence(ctx, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	workers, err = FoldWorkers(ctx, rows, time.Now(), 90*time.Second, nil)
	require.NoError(t, err)
	for _, w := range workers {
		if w.RunId == runId {
			assert.False(t, w.Alive, "stopped")
			assert.False(t, w.StoppedAt.IsZero())
		}
	}
	for _, r := range rows {
		assert.Equal(t, "watchbillWorker", r.Kind, "the kind label is on every row")
	}
}
