package imztop

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmscrape"
	"github.com/stretchr/testify/require"
)

// newColocatedSampler wires a consumer Sampler to a co-located StartScraper
// over a private inprocbus — the test analogue of the carousel host's wiring
// (ADR-0090): imztop subscribes; a separate scraper reads /proc and publishes.
// It Starts the Sampler and registers teardown via t.Cleanup. Skips when the
// scraper can't build (no /proc).
func newColocatedSampler(t *testing.T, opts SamplerOptions) (s *Sampler) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	pub := bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
	})
	sub := bus.NewClient(manifest.Id, []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub},
	})
	built, err := NewSampler(opts, sub)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	built.Start(ctx) // subscribe before the scraper publishes
	stopScraper, sErr := sysmscrape.StartScraper(ctx, pub, "test", opts.UpdateInterval, zerolog.Nop())
	if sErr != nil {
		t.Skipf("scraper unavailable (no /proc?): %v", sErr)
	}
	t.Cleanup(func() {
		_ = stopScraper()
		_ = built.Close()
	})
	s = built
	return
}

// TestSampler_ConsumerEndToEnd proves the ADR-0090 consumer model: a Sampler
// subscribed to the bus is driven by a separate StartScraper producer, holding
// no /proc access of its own. Latest() fills in from what crossed the bus, and
// Pause freezes the published frame (the scraper keeps publishing; onBundle
// drops frames while paused).
func TestSampler_ConsumerEndToEnd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scraper needs /proc (Linux)")
	}
	s := newColocatedSampler(t, SamplerOptions{UpdateInterval: 20 * time.Millisecond, HistoryWindow: 2 * time.Second})

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

	s.Pause(true)
	require.True(t, s.IsPaused())
	time.Sleep(80 * time.Millisecond) // let any in-flight frame drain
	frozen := s.Latest().SampledAtUnixMs
	time.Sleep(120 * time.Millisecond)
	require.Equal(t, frozen, s.Latest().SampledAtUnixMs, "paused sampler kept advancing")
}
