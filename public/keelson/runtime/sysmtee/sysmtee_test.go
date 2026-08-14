package sysmtee_test

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmtee"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingExec is a recordstore.ExecutorI that accepts inserts and remembers
// them. It keeps this test in the default lane: the store's SQL and Arrow
// encoding are exercised, the server is not.
type recordingExec struct {
	mu      sync.Mutex
	tables  []string
	batches int
	rows    int64
}

var _ recordstore.ExecutorI = (*recordingExec)(nil)

func (e *recordingExec) Exec(context.Context, string) error { return nil }

func (e *recordingExec) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (e *recordingExec) InsertArrow(_ context.Context, table string, records []arrow.RecordBatch) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tables = append(e.tables, table)
	e.batches += len(records)
	for _, r := range records {
		e.rows += r.NumRows()
	}
	return nil
}

func (e *recordingExec) snapshot() (tables []string, batches int, rows int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.tables...), e.batches, e.rows
}

func sampleBundle(ms int64) *sysmsnap.BundleSnapshot {
	return &sysmsnap.BundleSnapshot{
		SampledAtUnixMs: ms,
		CPU: &sysmsnap.CPUSnapshot{
			TotalPercent:   40,
			PerCorePercent: []uint8{10, 20},
			PerCoreFreqMHz: []uint32{3000, 3100},
			LoadAvg1:       0.5,
			ModelName:      "Test CPU",
			LogicalCores:   2,
		},
		Mem: &sysmsnap.MemSnapshot{TotalBytes: 16 << 30, AvailableBytes: 8 << 30},
	}
}

// startTee wires a tee over an in-process bus and returns it with the executor
// behind its store and a publish helper.
func startTee(t *testing.T, opts sysmtee.Options) (*sysmtee.Tee, *recordingExec, func(*sysmsnap.BundleSnapshot)) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	subCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}
	pubCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}}

	exec := &recordingExec{}
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})

	opts.Bus = bus.NewClient("tee", subCap)
	opts.Store = store
	if opts.Host == "" {
		opts.Host = "box1"
	}
	tee, err := sysmtee.Start(opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tee.Stop(); store.Close() })

	codec := sysmetricsbus.NewCBORCodec()
	pub := bus.NewClient("producer", pubCap)
	publish := func(snap *sysmsnap.BundleSnapshot) {
		payload, err := codec.Encode(snap)
		require.NoError(t, err)
		require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject(opts.Host), payload))
	}
	return tee, exec, publish
}

// TestTee_WritesWhatThePlaneCarries is the end-to-end shape: bundles in on the
// bus, rows out through the store's insert path.
func TestTee_WritesWhatThePlaneCarries(t *testing.T) {
	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: 20 * time.Millisecond})

	for i := range 3 {
		publish(sampleBundle(time.Now().UnixMilli() + int64(i)))
	}

	require.Eventually(t, func() bool {
		_, _, rows := exec.snapshot()
		return rows > 0
	}, 5*time.Second, 10*time.Millisecond, "no rows reached the executor")

	require.NoError(t, tee.Stop())

	tables, batches, rows := exec.snapshot()
	assert.Positive(t, batches)
	// Three ticks: three cpu samples, three mem samples, and one cpu descriptor
	// written on first sight of the host.
	assert.EqualValues(t, 7, rows, "expected 3 cpu + 3 mem + 1 descriptor")
	for _, tbl := range tables {
		assert.Equal(t, "boxer.facts", tbl, "the tee writes the facts table, qualified")
	}

	st := tee.Stats()
	assert.EqualValues(t, 3, st.Bundles)
	assert.EqualValues(t, 7, st.Rows)
	assert.EqualValues(t, 7, st.Flushed)
	assert.Zero(t, st.FlushErrors)
	assert.Zero(t, st.Dropped)
}

// The descriptor split: static CPU facts are written once per host, not once
// per tick, or a 1 Hz plane would store the model name 86400 times a day.
func TestTee_WritesTheCpuDescriptorOncePerHost(t *testing.T) {
	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: 20 * time.Millisecond})

	for i := range 5 {
		publish(sampleBundle(time.Now().UnixMilli() + int64(i)))
	}
	require.Eventually(t, func() bool {
		return tee.Stats().Rows >= 11
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tee.Stop())

	_, _, rows := exec.snapshot()
	// 5 cpu + 5 mem + exactly 1 descriptor.
	assert.EqualValues(t, 11, rows)
}

// A bundle whose collector was not wired contributes nothing. Absence must stay
// absence rather than becoming a row of zeros, which would be indistinguishable
// from a genuinely idle machine.
func TestTee_SkipsUnwiredDomains(t *testing.T) {
	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: 20 * time.Millisecond})

	publish(&sysmsnap.BundleSnapshot{
		SampledAtUnixMs: time.Now().UnixMilli(),
		Mem:             &sysmsnap.MemSnapshot{TotalBytes: 1 << 30},
	})
	require.Eventually(t, func() bool {
		_, _, rows := exec.snapshot()
		return rows > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tee.Stop())

	_, _, rows := exec.snapshot()
	assert.EqualValues(t, 1, rows, "a mem-only bundle is one row, with no cpu or descriptor")
}

// Stop must make queued work durable rather than abandoning it — otherwise the
// last flush interval of every run is lost on a clean shutdown.
func TestTee_StopFlushesWhatIsBuffered(t *testing.T) {
	// An interval long enough that nothing flushes on the ticker.
	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: time.Hour})

	publish(sampleBundle(time.Now().UnixMilli()))
	require.Eventually(t, func() bool {
		return tee.Stats().Rows > 0
	}, 5*time.Second, 10*time.Millisecond, "the bundle never reached the writer")

	_, _, before := exec.snapshot()
	require.Zero(t, before, "nothing should have flushed on this interval")

	require.NoError(t, tee.Stop())
	_, _, after := exec.snapshot()
	assert.EqualValues(t, 3, after, "Stop must drain and flush")
}

// TestTee_WritesEveryWiredDomain covers the M4 widening: a bundle with every
// collector wired must produce one row per kind, and the disk snapshot must
// produce two. Counting them is what catches a domain silently omitted from
// the ingest switch — which would look exactly like an unwired collector.
func TestTee_WritesEveryWiredDomain(t *testing.T) {
	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: 20 * time.Millisecond})

	publish(&sysmsnap.BundleSnapshot{
		SampledAtUnixMs: time.Now().UnixMilli(),
		CPU:             &sysmsnap.CPUSnapshot{TotalPercent: 10, LogicalCores: 2},
		Mem:             &sysmsnap.MemSnapshot{TotalBytes: 1 << 30},
		PSI:             &sysmsnap.PSISnapshot{Available: true},
		Net:             &sysmsnap.NetSnapshot{Interfaces: []sysmsnap.NetInterface{{Name: "eth0"}}},
		Disk: &sysmsnap.DiskSnapshot{
			Mounts:       []sysmsnap.DiskMount{{Device: "/dev/sda1", MountPoint: "/"}},
			BlockDevices: []sysmsnap.BlockDevice{{Name: "sda1"}},
		},
		Battery: &sysmsnap.BatterySnapshot{Batteries: []sysmsnap.BatteryStatus{{Name: "BAT0"}}},
		GPU:     &sysmsnap.GPUSnapshot{Devices: []sysmsnap.GPUDevice{{Vendor: "amd"}}},
	})

	// cpu + cpuInfo + mem + psi + net + diskMount + diskIo + battery + gpu.
	const wantRows = 9
	require.Eventually(t, func() bool {
		return tee.Stats().Flushed >= wantRows
	}, 5*time.Second, 10*time.Millisecond, "stats: %+v", tee.Stats())
	require.NoError(t, tee.Stop())

	_, _, rows := exec.snapshot()
	assert.EqualValues(t, wantRows, rows)
	assert.Zero(t, tee.Stats().FlushErrors)
}

// Command lines must not reach the table unless a deployment asked for them.
// This is the assertion behind ADR-0090 §SD8's exposure boundary for the
// durable form — the default writes the process table without them.
func TestTee_ProcCmdIsOffByDefault(t *testing.T) {
	bundle := func() *sysmsnap.BundleSnapshot {
		return &sysmsnap.BundleSnapshot{
			SampledAtUnixMs: time.Now().UnixMilli(),
			Procs:           []sysmsnap.ProcInfo{{PID: 1, Name: "systemd", Cmd: "/sbin/init"}},
		}
	}

	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: 20 * time.Millisecond})
	publish(bundle())
	require.Eventually(t, func() bool {
		_, _, rows := exec.snapshot()
		return rows > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tee.Stop())
	_, _, rows := exec.snapshot()
	assert.EqualValues(t, 1, rows, "the process table alone, without the command-line kind")

	optedIn, execIn, publishIn := startTee(t, sysmtee.Options{
		FlushInterval:  20 * time.Millisecond,
		PersistProcCmd: true,
	})
	publishIn(bundle())
	require.Eventually(t, func() bool {
		_, _, r := execIn.snapshot()
		return r >= 2
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, optedIn.Stop())
	_, _, rowsIn := execIn.snapshot()
	assert.EqualValues(t, 2, rowsIn, "opting in adds the command-line kind beside it")
}

// The sockets collector re-sends one snapshot on every tick. Writing a row per
// bundle would store the same observation repeatedly and put a rising Order on
// rows that never changed.
func TestTee_SocketsAreWrittenOncePerObservation(t *testing.T) {
	tee, exec, publish := startTee(t, sysmtee.Options{FlushInterval: 20 * time.Millisecond})

	sockets := &sysmsnap.SocketsSnapshot{
		CollectedAtUnixMs: time.Now().UnixMilli(),
		Sockets:           []sysmsnap.SocketInfo{{Proto: sysmsnap.SocketProtoTCP, Port: 8123}},
	}
	// Three bundles carrying the same snapshot, as the plane actually behaves.
	for range 3 {
		publish(&sysmsnap.BundleSnapshot{SampledAtUnixMs: time.Now().UnixMilli(), Sockets: sockets})
	}
	require.Eventually(t, func() bool { return tee.Stats().Rows >= 1 }, 5*time.Second, 10*time.Millisecond)

	// A new collection pass must produce a second row.
	publish(&sysmsnap.BundleSnapshot{
		SampledAtUnixMs: time.Now().UnixMilli(),
		Sockets: &sysmsnap.SocketsSnapshot{
			CollectedAtUnixMs: sockets.CollectedAtUnixMs + 5000,
			Sockets:           []sysmsnap.SocketInfo{{Proto: sysmsnap.SocketProtoTCP, Port: 9000}},
		},
	})
	require.Eventually(t, func() bool { return tee.Stats().Rows >= 2 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tee.Stop())

	_, _, rows := exec.snapshot()
	assert.EqualValues(t, 2, rows, "four bundles, two observations, two rows")
}

func TestTee_StopIsIdempotent(t *testing.T) {
	tee, _, _ := startTee(t, sysmtee.Options{FlushInterval: time.Hour})
	require.NoError(t, tee.Stop())
	require.NoError(t, tee.Stop())
}

func TestStart_RequiresBusStoreAndHost(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	client := bus.NewClient("tee", []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}})
	store := sysmfacts.NewSysmetricsStore(&recordingExec{}, nil, sysmfacts.SysmetricsStoreConfig{})
	t.Cleanup(store.Close)

	_, err := sysmtee.Start(sysmtee.Options{Store: store, Host: "box1"})
	require.Error(t, err)
	_, err = sysmtee.Start(sysmtee.Options{Bus: client, Host: "box1"})
	require.Error(t, err)
	// The host cannot be recovered from the message, so an unset one is a
	// configuration error rather than a silent empty token on every row.
	_, err = sysmtee.Start(sysmtee.Options{Bus: client, Store: store})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Host")
}
