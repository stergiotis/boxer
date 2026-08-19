//go:build integration

package imztop

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmtee"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This is the whole ADR-0197 read path against the table the runtime uses:
// bundles go onto a bus, the tee writes them, a StoreSource reads them back,
// and the ReplaySampler folds them into the same PublishedSnapshot the panels
// draw. Everything below the fold is covered by unit tests; what only a live
// server can show is that the two halves meet.

func replayITHost(t *testing.T) string {
	t.Helper()
	return "imztop-replay-it-" + time.Now().UTC().Format("20060102150405.000000")
}

// teeBundles publishes bundles through a tee onto the live table.
func teeBundles(t *testing.T, host string, bundles []*sysmsnap.BundleSnapshot) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	defer store.Close()
	require.NoError(t, store.VerifySchema(context.Background()))

	bus := inprocbus.NewInst(zerolog.Nop())
	tee, err := sysmtee.Start(sysmtee.Options{
		Bus:           bus.NewClient("tee", []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}),
		Store:         store,
		Host:          host,
		FlushInterval: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	codec := sysmetricsbus.NewCBORCodec()
	pub := bus.NewClient("producer", []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}})
	for _, b := range bundles {
		payload, encErr := codec.Encode(b)
		require.NoError(t, encErr)
		require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject(host), payload))
	}
	require.Eventually(t, func() bool { return tee.Stats().Flushed > 0 },
		10*time.Second, 50*time.Millisecond, "rows never became durable: %+v", tee.Stats())
	require.NoError(t, tee.Stop())
}

// TestReplayEndToEnd_StoredHistoryReachesTheFold is M3's proof: what the tee
// wrote is what the panels would draw.
func TestReplayEndToEnd_StoredHistoryReachesTheFold(t *testing.T) {
	host := replayITHost(t)
	base := time.Now().UTC().Truncate(time.Millisecond)

	const ticks = 5
	sent := make([]*sysmsnap.BundleSnapshot, 0, ticks)
	for i := range ticks {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU: &sysmsnap.CPUSnapshot{
				SampledAtUnixMs: ts,
				TotalPercent:    uint8(20 + i*10),
				PerCorePercent:  []uint8{uint8(i), uint8(i + 1)},
				ModelName:       "End-to-end CPU",
				LogicalCores:    2,
			},
			Mem: &sysmsnap.MemSnapshot{
				SampledAtUnixMs: ts,
				TotalBytes:      32 << 30, AvailableBytes: uint64(16-i) << 30,
			},
		})
	}
	teeBundles(t, host, sent)

	ctx := context.Background()
	src, err := NewStoreSource(ctx, StoreSourceOptions{Host: host})
	require.NoError(t, err)
	defer src.Close()
	assert.Equal(t, host, src.Host())
	assert.NotEmpty(t, src.Endpoint())

	session, err := NewReplaySampler(ReplayOptions{
		Source: src,
		Window: sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Hour)},
		Speed:  10000,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, session.Close()) }()
	session.Start(ctx)

	require.Eventually(t, session.Exhausted, 20*time.Second, 10*time.Millisecond,
		"the stored window never played through")

	snap := session.Latest()
	require.NotNil(t, snap, "the fold produced no frame from stored history")
	require.NotNil(t, snap.LatestCPU)
	assert.Equal(t, uint8(20+(ticks-1)*10), snap.LatestCPU.TotalPercent,
		"the last stored sample is the visible one")
	assert.Equal(t, "End-to-end CPU", snap.LatestCPU.ModelName,
		"the descriptor was carried forward from its once-written row")
	assert.Len(t, snap.HistoryCPUTotal, ticks, "every stored tick folded into the history")

	// Recorded time, not replay time (ADR-0197 §SD3).
	require.Len(t, snap.HistoryTimeUnixSec, ticks)
	assert.InDelta(t, float64(base.UnixMilli())/1000.0, snap.HistoryTimeUnixSec[0], 0.01)
	assert.Equal(t, time.Second, session.Interval(),
		"cadence is the recorded 1 Hz, not the 10000x playback")

	at, ok := session.Position()
	require.True(t, ok)
	assert.Equal(t, base.Add(4*time.Second).UnixMilli(), at.UnixMilli())
}

// TestReplayEndToEnd_SessionSwapsTheRenderPath exercises the process-wide
// session against a real store, which is what a window actually goes through.
func TestReplayEndToEnd_SessionSwapsTheRenderPath(t *testing.T) {
	require.NoError(t, LeaveReplay())
	t.Cleanup(func() { require.NoError(t, LeaveReplay()) })

	host := replayITHost(t)
	base := time.Now().UTC().Truncate(time.Millisecond)
	ts := base.UnixMilli()
	teeBundles(t, host, []*sysmsnap.BundleSnapshot{{
		SampledAtUnixMs: ts,
		CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: 77},
	}})

	ctx := context.Background()
	require.NoError(t, EnterReplay(ctx,
		sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Hour)},
		StoreSourceOptions{Host: host}))

	st := CurrentReplayStatus()
	require.Equal(t, ReplayOn, st.State, "err: %v", st.Err)
	assert.Equal(t, host, st.Host)

	s, err := activeSampler()
	require.NoError(t, err)
	require.Same(t, ActiveReplay(), s, "the render path must draw the session")

	require.Eventually(t, func() bool {
		snap := s.Latest()
		return snap != nil && snap.LatestCPU != nil && snap.LatestCPU.TotalPercent == 77
	}, 20*time.Second, 10*time.Millisecond, "stored history never reached the render path")

	require.NoError(t, LeaveReplay())
	assert.Equal(t, ReplayOff, CurrentReplayStatus().State)
}

// TestReplayEndToEnd_HostWithNoHistoryIsEmptyNotBroken is the "no tee, nothing
// to replay" path against a real, healthy server: the session opens, and says
// there is nothing rather than failing or hanging.
func TestReplayEndToEnd_HostWithNoHistoryIsEmptyNotBroken(t *testing.T) {
	require.NoError(t, LeaveReplay())
	t.Cleanup(func() { require.NoError(t, LeaveReplay()) })

	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}

	// A host token no tee has ever written under.
	host := replayITHost(t) + "-never-scraped"
	require.NoError(t, EnterReplay(context.Background(),
		sysmreplay.Window{From: time.Now().Add(-time.Hour), To: time.Now()},
		StoreSourceOptions{Host: host}))

	require.Eventually(t, func() bool { return CurrentReplayStatus().Empty },
		20*time.Second, 10*time.Millisecond,
		"a host with no stored history should report empty, not spin")

	st := CurrentReplayStatus()
	assert.Equal(t, ReplayOn, st.State, "an empty window is a healthy session, not a failure")
	assert.NoError(t, st.Err)
	assert.Nil(t, ActiveReplay().Latest(), "and nothing was folded")
}
