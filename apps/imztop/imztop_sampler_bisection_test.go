package imztop

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSampler_BisectionEndToEnd proves the ADR-0090 P2 deliverable: the
// producer→inprocbus→consumer path actually moves data. NewSampler wires a
// co-located producer/consumer over a private in-proc bus; Start kicks the
// producer; within a few ticks Latest() returns a PublishedSnapshot the
// consumer built from what crossed the bus (CBOR-encoded each tick). Pause
// then freezes the published frame.
//
// Linux-only: the cpu/mem collectors read /proc, so on other GOOS (or a
// /proc-restricted sandbox) NewSampler fails at construction — acceptable,
// sysmetrics targets Linux (same stance as the heap-drift guard).
func TestSampler_BisectionEndToEnd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sysmetrics collectors need /proc (Linux)")
	}

	s, err := NewSampler(SamplerOptions{
		UpdateInterval: 20 * time.Millisecond,
		HistoryWindow:  2 * time.Second,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	// Poll until the first frame crosses the bus and is windowed.
	var snap *PublishedSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snap = s.Latest(); snap != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, snap, "no PublishedSnapshot delivered through the bus")
	require.NotZero(t, snap.SampledAtUnixMs)
	require.NotEmpty(t, snap.HistoryTimeUnixSec, "windowing produced no history")
	require.NotNil(t, snap.LatestCPU, "cpu domain did not survive the bus round trip")

	// Pause stops the producer; once any in-flight tick drains, the
	// published frame must stop advancing.
	s.Pause(true)
	require.True(t, s.IsPaused())
	time.Sleep(80 * time.Millisecond) // > a few tick intervals: drain in-flight
	frozen := s.Latest().SampledAtUnixMs
	time.Sleep(120 * time.Millisecond)
	require.Equal(t, frozen, s.Latest().SampledAtUnixMs, "paused producer kept publishing")
}
