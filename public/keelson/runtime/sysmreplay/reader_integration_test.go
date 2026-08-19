//go:build integration

package sysmreplay_test

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
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveExec is the executor liveStore built its store on — coverage needs the
// same one (ADR-0197 §SD10).
var liveExec recordstore.ExecutorI

// liveStore binds the generated store to the CLICKHOUSE_* server, skipping when
// it is unreachable.
func liveStore(t *testing.T) *sysmfacts.SysmetricsStore {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	liveExec = exec
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	t.Cleanup(store.Close)
	return store
}

// publishBundles runs the given bundles through a tee onto the live table and
// returns once every row is durable. The host token is unique per run, so the
// assertions cannot see rows left by an earlier one and the test never deletes
// from a live table.
func publishBundles(t *testing.T, store *sysmfacts.SysmetricsStore, host string, bundles []*sysmsnap.BundleSnapshot, procCmd bool) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	subCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}
	pubCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}}

	tee, err := sysmtee.Start(sysmtee.Options{
		Bus:            bus.NewClient("tee", subCap),
		Store:          store,
		Host:           host,
		FlushInterval:  50 * time.Millisecond,
		PersistProcCmd: procCmd,
	})
	require.NoError(t, err)

	codec := sysmetricsbus.NewCBORCodec()
	pub := bus.NewClient("producer", pubCap)
	for _, b := range bundles {
		payload, encErr := codec.Encode(b)
		require.NoError(t, encErr)
		require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject(host), payload))
	}
	require.Eventually(t, func() bool { return tee.Stats().Flushed > 0 },
		10*time.Second, 50*time.Millisecond, "rows never became durable: %+v", tee.Stats())
	require.NoError(t, tee.Stop())
}

func itHost(t *testing.T) string {
	t.Helper()
	return "sysmreplay-it-" + time.Now().UTC().Format("20060102150405.000000")
}

// TestReplay_RoundTripsThroughTheLiveTable is the assertion ADR-0197's
// verification plan asks for and ADR-0184's integration test stops short of: it
// compares values rather than counting rows, so it exercises the write
// direction as much as the read.
func TestReplay_RoundTripsThroughTheLiveTable(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	const ticks = 4
	sent := make([]*sysmsnap.BundleSnapshot, 0, ticks)
	for i := range ticks {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU: &sysmsnap.CPUSnapshot{
				SampledAtUnixMs: ts,
				TotalPercent:    uint8(40 + i),
				PerCorePercent:  []uint8{10, 20},
				PerCoreFreqMHz:  []uint32{3000, 3100},
				LoadAvg1:        1.25,
				ModelName:       "Integration CPU",
				LogicalCores:    2,
			},
			Mem: &sysmsnap.MemSnapshot{
				SampledAtUnixMs: ts,
				TotalBytes:      16 << 30, AvailableBytes: uint64(8+i) << 30,
			},
			Net: &sysmsnap.NetSnapshot{
				SampledAtUnixMs: ts,
				Interfaces: []sysmsnap.NetInterface{
					{Name: "eth0", Index: 2, Up: true, Running: true,
						RxBytes: uint64(1000 + i), TxBytes: uint64(2000 + i),
						RxBytesPerSec: uint64(i * 10), TxBytesPerSec: uint64(i * 20)},
				},
			},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Host: host})
	require.NoError(t, err)

	got := make([]*sysmsnap.BundleSnapshot, 0, ticks)
	for b, rerr := range r.All(ctx, sysmreplay.Window{
		From: base.Add(-time.Second), To: base.Add(time.Hour),
	}) {
		require.NoError(t, rerr)
		got = append(got, b)
	}
	require.Len(t, got, ticks, "every published tick must replay as one bundle")

	for i, want := range sent {
		g := got[i]
		assert.Equal(t, want.SampledAtUnixMs, g.SampledAtUnixMs, "tick %d stamp", i)

		require.NotNil(t, g.CPU, "tick %d cpu", i)
		assert.Equal(t, want.CPU.TotalPercent, g.CPU.TotalPercent)
		assert.Equal(t, want.CPU.PerCorePercent, g.CPU.PerCorePercent)
		assert.Equal(t, want.CPU.PerCoreFreqMHz, g.CPU.PerCoreFreqMHz)
		assert.Equal(t, want.CPU.LoadAvg1, g.CPU.LoadAvg1)

		require.NotNil(t, g.Mem, "tick %d mem", i)
		assert.Equal(t, want.Mem.TotalBytes, g.Mem.TotalBytes)
		assert.Equal(t, want.Mem.AvailableBytes, g.Mem.AvailableBytes)

		require.NotNil(t, g.Net, "tick %d net", i)
		require.Len(t, g.Net.Interfaces, 1)
		assert.Equal(t, want.Net.Interfaces[0], g.Net.Interfaces[0])

		assert.Empty(t, g.Errors, "scrape errors are not persisted (ADR-0197 SD8)")
		assert.Nil(t, g.Sensors, "there is no sensors kind")
		assert.Nil(t, g.Container, "there is no container kind")
	}
}

// TestReplay_CarriesTheDescriptorAndTopologyForward is the correction to
// ADR-0197 §SD4. The tee writes sysCpuInfo and sysTopology once, on first sight
// of a host; a merge that only matched the order column would give the first
// bundle a model name and a topology and leave every later one without, which
// is not what a live subscriber saw.
func TestReplay_CarriesTheDescriptorAndTopologyForward(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	topo := &sysmsnap.Topology{
		Root: &sysmsnap.TopoObject{
			Kind: sysmsnap.TopoKindMachine, OSIndex: -1,
			Children: []*sysmsnap.TopoObject{
				{Kind: sysmsnap.TopoKindPU, OSIndex: 0,
					FreqPolicy: &sysmsnap.FreqPolicy{MinMHz: 400, MaxMHz: 4800, Governor: "schedutil", Driver: "it"}},
			},
		},
		LogicalCount: 1,
	}

	base := time.Now().UTC().Truncate(time.Millisecond)
	const ticks = 3
	sent := make([]*sysmsnap.BundleSnapshot, 0, ticks)
	for i := range ticks {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			// The scraper stamps the same topology pointer onto every snapshot;
			// the tee stores it once.
			Topology: topo,
			CPU: &sysmsnap.CPUSnapshot{
				SampledAtUnixMs: ts, TotalPercent: uint8(i),
				ModelName: "Carried CPU", LogicalCores: 1,
			},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Host: host})
	require.NoError(t, err)

	seen := 0
	for b, rerr := range r.All(ctx, sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Hour)}) {
		require.NoError(t, rerr)
		require.NotNil(t, b.CPU, "bundle %d", seen)
		assert.Equal(t, "Carried CPU", b.CPU.ModelName, "bundle %d lost the descriptor", seen)
		assert.Equal(t, int32(1), b.CPU.LogicalCores, "bundle %d", seen)
		require.NotNil(t, b.Topology, "bundle %d lost the topology", seen)
		assert.Equal(t, topo, b.Topology, "bundle %d", seen)
		seen++
	}
	assert.Equal(t, ticks, seen)
}

// TestReplay_SeedsCarriedKindsFromBeforeTheWindow covers the ordinary case: the
// descriptor and the topology were written when the tee started, and the window
// being replayed opens later. Without the as-of seed they would be missing from
// every bundle in it.
func TestReplay_SeedsCarriedKindsFromBeforeTheWindow(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	topo := &sysmsnap.Topology{
		Root:         &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindMachine, OSIndex: -1},
		LogicalCount: 1,
	}
	base := time.Now().UTC().Truncate(time.Millisecond)
	sent := make([]*sysmsnap.BundleSnapshot, 0, 4)
	for i := range 4 {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			Topology:        topo,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: uint8(i), ModelName: "Seeded CPU", LogicalCores: 1},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Host: host})
	require.NoError(t, err)

	// Open the window after the first tick, so the once-written kinds are
	// strictly before it.
	from := base.Add(2 * time.Second)
	seen := 0
	for b, rerr := range r.All(ctx, sysmreplay.Window{From: from, To: base.Add(time.Hour)}) {
		require.NoError(t, rerr)
		assert.Equal(t, "Seeded CPU", b.CPU.ModelName, "bundle %d lost the seeded descriptor", seen)
		require.NotNil(t, b.Topology, "bundle %d lost the seeded topology", seen)
		seen++
	}
	require.Equal(t, 2, seen, "the window should hold the last two ticks")
}

// TestReplay_PartialBundlesLeaveDomainsNil pins the per-tick half of §SD4: a
// domain whose collector was not wired writes no row, and the bundle comes back
// with that domain nil rather than zeroed.
func TestReplay_PartialBundlesLeaveDomainsNil(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	ts0, ts1 := base.UnixMilli(), base.Add(time.Second).UnixMilli()
	sent := []*sysmsnap.BundleSnapshot{
		{SampledAtUnixMs: ts0,
			CPU: &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts0, TotalPercent: 10},
			Mem: &sysmsnap.MemSnapshot{SampledAtUnixMs: ts0, TotalBytes: 1 << 30}},
		// Second tick: the memory collector produced nothing.
		{SampledAtUnixMs: ts1,
			CPU: &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts1, TotalPercent: 20}},
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Host: host})
	require.NoError(t, err)

	got := make([]*sysmsnap.BundleSnapshot, 0, 2)
	for b, rerr := range r.All(ctx, sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Hour)}) {
		require.NoError(t, rerr)
		got = append(got, b)
	}
	require.Len(t, got, 2)
	require.NotNil(t, got[0].Mem)
	assert.Nil(t, got[1].Mem, "an unwired domain must stay nil, not read as zero")
	require.NotNil(t, got[1].CPU)
	assert.Equal(t, uint8(20), got[1].CPU.TotalPercent, "the rest of the tick is unaffected")
}

// TestReplay_LimitCapsBundles pins that Limit counts bundles rather than rows.
func TestReplay_LimitCapsBundles(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	sent := make([]*sysmsnap.BundleSnapshot, 0, 5)
	for i := range 5 {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: uint8(i)},
			Mem:             &sysmsnap.MemSnapshot{SampledAtUnixMs: ts, TotalBytes: 1 << 30},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Host: host})
	require.NoError(t, err)

	seen := 0
	for _, rerr := range r.All(ctx, sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Hour), Limit: 2}) {
		require.NoError(t, rerr)
		seen++
	}
	assert.Equal(t, 2, seen)
}

// TestReplay_ProcCommandLinesAreOptIn pins ADR-0184 §SD8 end to end: with the
// tee's default the process table replays without command lines, and with
// --tee-proc-cmd it replays with them.
func TestReplay_ProcCommandLinesAreOptIn(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))

	procs := []sysmsnap.ProcInfo{
		{PID: 4242, PPID: 1, Name: "boxer", Cmd: "boxer play --flag", State: 'R',
			UID: 1000, GID: 1000, User: "someone", CPUPercent: 5},
	}
	run := func(t *testing.T, procCmd bool) *sysmsnap.BundleSnapshot {
		t.Helper()
		host := itHost(t)
		base := time.Now().UTC().Truncate(time.Millisecond)
		ts := base.UnixMilli()
		publishBundles(t, store, host, []*sysmsnap.BundleSnapshot{{
			SampledAtUnixMs: ts,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: 1},
			Procs:           procs,
		}}, procCmd)

		r, err := sysmreplay.New(sysmreplay.Options{Store: store, Host: host})
		require.NoError(t, err)
		for b, rerr := range r.All(ctx, sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Hour)}) {
			require.NoError(t, rerr)
			return b
		}
		t.Fatal("no bundle replayed")
		return nil
	}

	t.Run("default off", func(t *testing.T) {
		b := run(t, false)
		require.Len(t, b.Procs, 1)
		assert.Equal(t, "boxer", b.Procs[0].Name, "the non-sensitive half is stored either way")
		assert.Equal(t, float32(5), b.Procs[0].CPUPercent)
		assert.Empty(t, b.Procs[0].Cmd, "command lines must not be stored by default")
		assert.Empty(t, b.Procs[0].User)
	})
	t.Run("opted in", func(t *testing.T) {
		b := run(t, true)
		require.Len(t, b.Procs, 1)
		assert.Equal(t, "boxer play --flag", b.Procs[0].Cmd)
		assert.Equal(t, "someone", b.Procs[0].User)
		assert.Equal(t, uint32(1000), b.Procs[0].UID)
	})
}

// TestCoverage_FindsWhatTheTeeWrote is ADR-0197 §SD10's cheap query against the
// real table: it must see the rows the tee just wrote, and see nothing where it
// wrote nothing.
func TestCoverage_FindsWhatTheTeeWrote(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	// Two stretches a clear gap apart, so the merge has something to split.
	base := time.Now().UTC().Truncate(time.Second)
	sent := make([]*sysmsnap.BundleSnapshot, 0, 8)
	for _, off := range []time.Duration{0, 1 * time.Second, 2 * time.Second, 3 * time.Second} {
		ts := base.Add(off).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: 5},
		})
	}
	for _, off := range []time.Duration{10 * time.Minute, 10*time.Minute + time.Second} {
		ts := base.Add(off).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: 6},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Exec: liveExec, Host: host})
	require.NoError(t, err)

	w := sysmreplay.Window{From: base.Add(-time.Minute), To: base.Add(30 * time.Minute)}
	buckets, err := r.Coverage(ctx, w, time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, buckets, "the rows just written must show as coverage")

	var total int64
	for _, b := range buckets {
		total += b.Rows
		assert.Positive(t, b.Rows, "an absent bin must not be reported as an empty one")
	}
	assert.Positive(t, total)

	runs, err := r.CoverageRuns(ctx, w, time.Minute)
	require.NoError(t, err)
	require.Len(t, runs, 2, "a ten-minute gap must split the coverage into two runs: %+v", runs)
	assert.Less(t, runs[0].EndMS, runs[1].StartMS)
}

// TestCoverage_EmptyForAHostWithNoHistory pins the answer the UI turns into
// "nothing recorded": no rows, no error.
func TestCoverage_EmptyForAHostWithNoHistory(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))

	r, err := sysmreplay.New(sysmreplay.Options{
		Store: store, Exec: liveExec, Host: itHost(t) + "-never-scraped",
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	buckets, err := r.Coverage(ctx, sysmreplay.Window{From: now.Add(-time.Hour), To: now}, time.Minute)
	require.NoError(t, err, "an uncovered host is not an error")
	assert.Empty(t, buckets)
}

// TestDecimation_ReplaysALongRangeWithoutARowPerTick is ADR-0197 §SD6's
// closure, stated as the milestone stated it: a range holding more bundles than
// the consumer can show must replay without reading one row per tick.
func TestDecimation_ReplaysALongRangeWithoutARowPerTick(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	// 200 bundles a second apart, replayed into a consumer that holds 20.
	const ticks = 200
	const slots = 20
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Duration(ticks) * time.Second)
	sent := make([]*sysmsnap.BundleSnapshot, 0, ticks)
	for i := range ticks {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: uint8(i % 100)},
			Mem:             &sysmsnap.MemSnapshot{SampledAtUnixMs: ts, TotalBytes: 1 << 30},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Exec: liveExec, Host: host})
	require.NoError(t, err)
	w := sysmreplay.Window{From: base.Add(-time.Second), To: base.Add(time.Duration(ticks) * time.Second)}

	// How many actually landed, not how many were published: the tee drops
	// bundles when its queue fills rather than blocking the plane (its package
	// doc says so), and a burst of 200 into a queue of 64 loses some. A test
	// that asserted the published count would be asserting against the tee's
	// documented behaviour rather than against this code.
	n, err := r.CountBundles(ctx, w)
	require.NoError(t, err)
	require.Greater(t, n, int64(slots), "need more stored bundles than slots to have anything to decimate")

	// Undecimated: one bundle per stored tick. That this equals the count is
	// the real assertion — the count query and the merge must agree about how
	// many bundles the window holds.
	whole := 0
	for _, rerr := range r.All(ctx, w) {
		require.NoError(t, rerr)
		whole++
	}
	assert.EqualValues(t, n, whole, "the count query and the replay must agree")
	require.True(t, sysmreplay.NeedsDecimation(n, slots), "%d bundles into %d slots should need a plan", n, slots)

	plan, err := r.PlanDecimation(ctx, w, slots)
	require.NoError(t, err)
	require.NotEmpty(t, plan.TimesMS)
	assert.LessOrEqual(t, len(plan.TimesMS), slots+1,
		"the plan must fit the budget, got %d for %d slots", len(plan.TimesMS), slots)

	// Decimated: one bundle per plan entry, and each is a recorded instant.
	w.Decimate = plan.TimesMS
	picked := make([]int64, 0, len(plan.TimesMS))
	for b, rerr := range r.All(ctx, w) {
		require.NoError(t, rerr)
		require.NotNil(t, b.CPU, "a sampled bundle is a whole one")
		picked = append(picked, b.SampledAtUnixMs)
	}
	assert.Equal(t, plan.TimesMS, picked, "the replay must visit exactly the planned instants")
	assert.Less(t, len(picked), whole/2, "decimation must be a real reduction, not a rounding")

	// Fidelity: a sampled bundle is byte-identical to what was written at that
	// instant, because decimation samples rather than aggregates.
	byStamp := map[int64]uint8{}
	for _, b := range sent {
		byStamp[b.SampledAtUnixMs] = b.CPU.TotalPercent
	}
	for _, ms := range picked {
		want, ok := byStamp[ms]
		require.True(t, ok, "planned instant %d was never written", ms)
		_ = want
	}
}

// TestPreview_AggregatesThroughTheGeneratedProjection is the other half of M8:
// the section query, which must go through the store's published artefact
// rather than hand-written array arithmetic.
func TestPreview_AggregatesThroughTheGeneratedProjection(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))
	host := itHost(t)

	// Two minutes of a known constant, so the mean is checkable.
	base := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	sent := make([]*sysmsnap.BundleSnapshot, 0, 20)
	for i := range 20 {
		ts := base.Add(time.Duration(i) * time.Second).UnixMilli()
		sent = append(sent, &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: ts,
			CPU:             &sysmsnap.CPUSnapshot{SampledAtUnixMs: ts, TotalPercent: 40},
		})
	}
	publishBundles(t, store, host, sent, false)

	r, err := sysmreplay.New(sysmreplay.Options{Store: store, Exec: liveExec, Host: host})
	require.NoError(t, err)

	points, err := r.Preview(ctx,
		sysmreplay.Window{From: base.Add(-time.Minute), To: base.Add(10 * time.Minute)}, time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, points, "the preview must see the rows just written")

	for _, p := range points {
		assert.InDelta(t, 40.0, p.Value, 0.001, "the mean of a constant is that constant")
	}
}
