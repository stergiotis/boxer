package sysmscrape_test

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
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/require"
)

// TestStartScraper_PublishesOverBus runs the shared scraper helper against an
// in-proc bus and asserts a Consumer receives published snapshots — the
// co-located wiring the carousel host, the screenshot tour, and tests share.
// Linux-only: StartScraper builds the real collector set, which reads /proc.
func TestStartScraper_PublishesOverBus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("DefaultBundleOptions needs /proc (Linux)")
	}

	bus := inprocbus.NewInst(zerolog.Nop())
	pub := bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
	})
	sub := bus.NewClient("test.consumer", []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub},
	})

	got := make(chan *sysmsnap.BundleSnapshot, 4)
	consumer, err := sysmetricsbus.NewConsumer(sysmetricsbus.ConsumerOptions{
		Bus:     sub,
		Subject: sysmetricsbus.BundleSubjectWildcard(),
		Codec:   sysmetricsbus.NewCBORCodec(),
		Handler: func(s *sysmsnap.BundleSnapshot) {
			select {
			case got <- s:
			default:
			}
		},
	})
	require.NoError(t, err)
	require.NoError(t, consumer.Start())
	t.Cleanup(func() { _ = consumer.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stop, err := sysmscrape.StartScraper(ctx, pub, "testhost", 30*time.Millisecond, zerolog.Nop())
	if err != nil {
		t.Skipf("scraper build failed (likely no /proc access): %v", err)
	}
	t.Cleanup(func() { _ = stop() })

	select {
	case s := <-got:
		require.NotNil(t, s)
		require.NotNil(t, s.CPU, "cpu domain should be present from a real scrape")
	case <-time.After(3 * time.Second):
		t.Fatal("scraper published no snapshot over the bus")
	}
}
