package loghost

import (
	"context"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithoutLogFactsNothingIsPersisted pins the default policy: with
// BOXER_LOG_FACTS empty or "0", Install must not reach for ClickHouse and
// keeps no store at all, and says why — the log viewer shows that reason.
func TestWithoutLogFactsNothingIsPersisted(t *testing.T) {
	for _, v := range []string{"", "0"} {
		t.Run("BOXER_LOG_FACTS="+v, func(t *testing.T) {
			t.Setenv("BOXER_LOG_FACTS", v)
			store, _, _, why := openFactsStore(context.Background(), "")
			assert.Nil(t, store)
			assert.Contains(t, why, "BOXER_LOG_FACTS is not set")
		})
	}
}

// TestInstallReturnsUsableCloser guards the exact contract mainC relies
// on: Install returns a non-nil closer, the retargeted global logger
// accepts an event, and the closer restores the previous global logger.
func TestInstallReturnsUsableCloser(t *testing.T) {
	t.Setenv("BOXER_LOG_FACTS", "0") // no ClickHouse, no sink

	prev := log.Logger
	t.Cleanup(func() { log.Logger = prev }) // belt-and-braces if closer misbehaves

	closer := Install(context.Background())
	require.NotNil(t, closer)

	log.Info().Str("probe", "loghost").Msg("loghost install smoke test")

	require.NoError(t, closer())
	assert.Equal(t, prev, log.Logger, "closer must restore the previous global logger")
}

// AdoptRun is what the viewer's This run scope reads, at query time.
func TestAdoptRunIsReadAtQueryTime(t *testing.T) {
	prev := currentRun()
	t.Cleanup(func() { AdoptRun(prev) })
	AdoptRun("")
	assert.Equal(t, "", currentRun())
	AdoptRun("run-x")
	assert.Equal(t, "run-x", currentRun())
}
