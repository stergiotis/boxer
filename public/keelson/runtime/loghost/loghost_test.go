package loghost

import (
	"context"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectFactsStoreDefaultsToMemory pins the default backend policy:
// with BOXER_LOG_FACTS empty or "0", Install must not reach for
// ClickHouse — it falls back to the in-memory store so the logviewer tail
// works with zero external dependencies.
func TestSelectFactsStoreDefaultsToMemory(t *testing.T) {
	for _, v := range []string{"", "0"} {
		t.Run("BOXER_LOG_FACTS="+v, func(t *testing.T) {
			t.Setenv("BOXER_LOG_FACTS", v)
			store, kind := selectFactsStore(context.Background())
			require.NotNil(t, store)
			assert.Equal(t, "memory", kind)
		})
	}
}

// TestInstallReturnsUsableCloser guards the exact contract mainC relies
// on: Install returns a non-nil closer, wiring the bridge (over the
// dependency-free in-memory backend) does not error, the retargeted
// global logger accepts an event, and the closer drains cleanly and
// restores the previous global logger.
func TestInstallReturnsUsableCloser(t *testing.T) {
	t.Setenv("BOXER_LOG_FACTS", "0") // force the in-memory path — no ClickHouse

	prev := log.Logger
	t.Cleanup(func() { log.Logger = prev }) // belt-and-braces if closer misbehaves

	closer := Install(context.Background())
	require.NotNil(t, closer)

	// The global logger is now bridge-wrapped; emitting must not panic.
	log.Info().Str("probe", "loghost").Msg("loghost install smoke test")

	require.NoError(t, closer())
	assert.Equal(t, prev, log.Logger, "closer must restore the previous global logger")
}
