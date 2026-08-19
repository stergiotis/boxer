// Profiles tab: capture a pprof profile of this process, publish it as an
// ad-hoc dataset (ADR-0134), and open a play window on it (ADR-0135) — the
// M2 producer of doc/adr-background-work/pprof-profiles-as-data.md, placed
// here per ADR-0061's 2026-08-01 update (the one deliberate act the
// otherwise observe-only dashboard performs, always user-initiated).
//
// Capture state is process-global like the Sampler (ADR-0061 SD3): one Go
// runtime means one current dataset per profile kind, shared by every imzrt
// window. A re-capture republishes onto the SAME handle (revision bump), so
// seeded play buffers and saved SQL keep working across captures. Handles
// are deliberately not retracted on Unmount — a profile stays explorable
// after the dashboard closes; the store bounds the cost (one dataset per
// kind, service quotas).
//
// Block and mutex profiles are absent on purpose: they are empty unless
// runtime.SetBlockProfileRate / SetMutexProfileFraction are set, and imzrt
// does not mutate runtime tunables (ADR-0061 SD6).
//
// goroutineleak (go1.27, ADR-0199) is present because it needs no tunable —
// but it is the one capture here that is not free of effect on what the
// dashboard is showing. It is computed by a GC-assisted reachability pass, so
// a capture forces a garbage collection: measured against a 205 MB live heap,
// 4.5–7.3 ms against 200–300 µs for the goroutine profile, one extra GC cycle,
// ~0.1 ms of it stop-the-world. The latency lands off the render thread like
// every other capture here; the forced cycle does not — it puts a step in the
// heap and GC series the other tabs are plotting. That is a caveat on
// ADR-0061's observe-only framing, recorded there.

package imzrt

import (
	"bytes"
	"context"
	"fmt"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/profiling/pprofarrow"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
)

// profileAliasPrefix + kind key is the stable dataset alias (`pprof_cpu`,
// `pprof_heap`, …) the catalog table keelson('adhoc') lists.
const profileAliasPrefix = "pprof_"

// cpuCaptureDuration is the fixed CPU sampling window. A duration knob is
// deliberate scope-out for the first cut.
const cpuCaptureDuration = 10 * time.Second

// profileCapture is one finished capture→convert→publish run (the bgjob
// result type).
type profileCapture struct {
	handle   string
	revision uint64
	rows     uint64
}

// profileKindSpec declares one capturable kind: its alias/hint key, button
// label, the raw-bytes capture step, and how the kind's value reads.
type profileKindSpec struct {
	key     string
	label   string
	capture func(ctx context.Context, report bgjob.Reporter) ([]byte, error)

	// unit labels the explore window's value axis and divisor converts the
	// profile's native quantity into it. Nanoseconds and bytes both read
	// badly raw — a 10 s capture is ~1e10 ns — and the reader scales by SI
	// prefix only, so the choice here is what lands totals in a range that
	// needs no prefix at all. A zero divisor leaves the native value alone.
	// ClickHouse's `/` is floating division whatever the operand types, so
	// an integer divisor does not truncate.
	unit    string
	divisor int64
}

// profileKinds is the UI order. CPU runs a sampling window; the others
// snapshot instantly. The units follow the converter's default sample type
// per kind: cpu/nanoseconds, inuse_space and alloc_space in bytes,
// goroutine/count, goroutineleak/count.
//
// goroutineleak needs no kind hint even though it renders like the goroutine
// profile: it carries its own sample type, so inferKind returns it and the
// alias lands on pprof_goroutineleak. Block and mutex, which do collide, are
// the reason WithKindHint exists — this kind is not that case, and
// TestInstantCapturesConvert pins it.
var profileKinds = []profileKindSpec{
	{key: "cpu", label: "CPU (10 s)", capture: captureCPU(cpuCaptureDuration),
		unit: "ms", divisor: 1e6},
	{key: "heap", label: "Heap", capture: captureLookup("heap"),
		unit: "MiB", divisor: 1 << 20},
	{key: "allocs", label: "Allocs", capture: captureLookup("allocs"),
		unit: "MiB", divisor: 1 << 20},
	{key: "goroutine", label: "Goroutines", capture: captureLookup("goroutine"),
		unit: "goroutines"},
	{key: "goroutineleak", label: "Leaked goroutines", capture: captureLookup("goroutineleak"),
		unit: "goroutines"},
}

// profileEntry is the shared per-kind state. The embedded Runner has its
// own lock; the hub mutex guards the rest.
type profileEntry struct {
	job      bgjob.Runner[profileCapture]
	handle   string
	revision uint64
	rows     uint64
	opening  bool
	lastErr  string
}

// profilesHub is the process-global capture state (see the package
// comment for why it is not per-window).
type profilesHub struct {
	mu      sync.Mutex
	entries map[string]*profileEntry
}

var profiles = profilesHub{entries: make(map[string]*profileEntry)}

func (h *profilesHub) entry(key string) (e *profileEntry) {
	h.mu.Lock()
	e = h.entries[key]
	if e == nil {
		e = &profileEntry{}
		h.entries[key] = e
	}
	h.mu.Unlock()
	return
}

// captureCPU returns a capture step sampling the CPU for d. It fails when
// another CPU profile is already running (a second window's capture, or a
// process started with --pprofCpuOutputFile). Cancellation aborts the run;
// the partial profile is discarded with it.
func captureCPU(d time.Duration) func(ctx context.Context, report bgjob.Reporter) ([]byte, error) {
	return func(ctx context.Context, report bgjob.Reporter) (raw []byte, err error) {
		var buf bytes.Buffer
		err = pprof.StartCPUProfile(&buf)
		if err != nil {
			err = eh.Errorf("start cpu profile: %w", err)
			return
		}
		const steps = 20
		tick := d / steps
		for i := range uint64(steps) {
			report(i, steps, "sampling")
			select {
			case <-ctx.Done():
				pprof.StopCPUProfile()
				err = ctx.Err()
				return
			case <-time.After(tick):
			}
		}
		pprof.StopCPUProfile()
		raw = buf.Bytes()
		return
	}
}

// captureLookup returns a capture step snapshotting a named instantaneous
// profile (heap, allocs, goroutine) in proto form.
func captureLookup(name string) func(ctx context.Context, report bgjob.Reporter) ([]byte, error) {
	return func(_ context.Context, report bgjob.Reporter) (raw []byte, err error) {
		report(0, 0, "snapshotting")
		p := pprof.Lookup(name)
		if p == nil {
			err = eh.Errorf("unknown profile %q", name)
			return
		}
		var buf bytes.Buffer
		err = p.WriteTo(&buf, 0)
		if err != nil {
			err = eh.Errorf("write %s profile: %w", name, err)
			return
		}
		raw = buf.Bytes()
		return
	}
}

// profileExploreTab is the body tab an explore window opens focused on:
// play's icicle panel (ADR-0160), which the seed below feeds directly. An
// id play does not register is a warning there, not a mount error, so a
// drifted slug costs the focus and nothing else — which is why this stays a
// literal rather than an import of the play package (whose init registers
// the SQL playground as a side effect, ADR-0017 §SD4).
const profileExploreTab = "icicle"

// profileSeedSql is the buffer a fresh explore window opens with: the
// capture projected into the icicle panel's folded contract — a root-first
// `stack` array and each stack's OWN value, which is exactly what the
// converter emits, so the projection only rescales the quantity and names
// its unit. The window carries every other tab too, so Table reads the same
// rows unfolded.
//
// One statement — grammar1 parses nothing else — and the handle is spliced
// as a literal (keelson() resolution runs before parameter substitution).
// The inner alias is load-bearing: `value / d AS value` reads to ClickHouse
// as a cyclic alias.
func profileSeedSql(handle string, spec profileKindSpec) string {
	value := "v"
	if spec.divisor != 0 {
		value = fmt.Sprintf("v / %d", spec.divisor)
	}
	return fmt.Sprintf(
		"WITH s AS (SELECT stack, value AS v FROM keelson('%s'))\n"+
			"SELECT stack, %s AS value, '%s' AS unit\n"+
			"FROM s", handle, value, spec.unit)
}

// startCapture launches the capture→convert→publish job for one kind. The
// compute callback runs off the render thread, so the blocking
// PublishRequest is legal there; results land via TakeResult on the next
// frames.
func (h *profilesHub) startCapture(spec profileKindSpec, bus app.BusI, tasks task.TaskApiI) {
	e := h.entry(spec.key)
	h.mu.Lock()
	prevHandle := e.handle
	h.mu.Unlock()

	started := e.job.StartReporting(tasks, bgjob.Spec{
		Kind:  "imzrt-pprof",
		Title: "pprof capture: " + spec.key,
		Tag:   spec.key,
	}, func(ctx context.Context, report bgjob.Reporter) (out *profileCapture, err error) {
		raw, err := spec.capture(ctx, report)
		if err != nil {
			return
		}
		report(0, 0, "converting")
		conv, err := pprofarrow.Convert(bytes.NewReader(raw), pprofarrow.WithKindHint(spec.key))
		if err != nil {
			return
		}
		report(0, 0, "publishing")
		if bus == nil {
			err = eh.Errorf("no bus wired (unmounted window?)")
			return
		}
		res, err := adhocdata.PublishRequest(bus, adhocdata.PublishInput{
			Alias:          profileAliasPrefix + spec.key,
			Handle:         prevHandle,
			ArrowIPCStream: conv.IPCStream,
		})
		if err != nil {
			err = eh.Errorf("publish: %w", err)
			return
		}
		out = &profileCapture{handle: res.Handle, revision: res.Revision, rows: res.Rows}
		return
	})
	if !started {
		log.Debug().Str("kind", spec.key).Msg("imzrt: pprof capture already running")
	}
}

// explore opens a play window seeded on the kind's current dataset, bound
// to the introspection endpoint where keelson('<handle>') resolves.
// RequestOpen blocks on the bus round-trip, so it runs off the frame loop.
func (h *profilesHub) explore(spec profileKindSpec, bus app.BusI) {
	key := spec.key
	e := h.entry(key)
	h.mu.Lock()
	handle := e.handle
	if handle == "" || e.opening || bus == nil {
		h.mu.Unlock()
		return
	}
	e.opening = true
	h.mu.Unlock()

	go func() {
		cfgBytes, err := buscodec.Encode(launchcfg.PlayLaunch{
			At:       time.Now().UTC(),
			Sql:      profileSeedSql(handle, spec),
			AutoRun:  true,
			Tab:      profileExploreTab,
			Endpoint: launchcfg.EndpointIntrospection,
		})
		if err == nil {
			_, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfgBytes)
		}
		h.mu.Lock()
		e.opening = false
		if err != nil {
			e.lastErr = "explore: " + err.Error()
			log.Warn().Err(err).Str("kind", key).Msg("imzrt: open play on profile failed")
		} else {
			e.lastErr = ""
		}
		h.mu.Unlock()
	}()
}

// syncEntry consumes a finished job result into the shared entry (render
// thread; TakeResult returns each result exactly once).
func (h *profilesHub) syncEntry(e *profileEntry) {
	if res, _, ok := e.job.TakeResult(); ok {
		h.mu.Lock()
		e.handle = res.handle
		e.revision = res.revision
		e.rows = res.rows
		e.lastErr = ""
		h.mu.Unlock()
	}
}

// renderProfilesPanel draws the Profiles tab body: one row per kind with
// capture/explore affordances and the current dataset's coordinates.
func (inst *App) renderProfilesPanel() {
	inst.sectionHeader("Profile capture")
	c.Label("Captures profile this process, publishes it as an ad-hoc dataset (alias pprof_<kind>, stable handle across re-captures), and Explore opens a play window on it — focused on the flamegraph, with the other lenses a tab away.").Send()
	c.AddSpace(styletokens.PaddingInner(styletokens.DensityFromEnv()))

	for _, spec := range profileKinds {
		e := profiles.entry(spec.key)
		profiles.syncEntry(e)
		snap := e.job.Snapshot()

		profiles.mu.Lock()
		handle, revision, rows, lastErr := e.handle, e.revision, e.rows, e.lastErr
		profiles.mu.Unlock()

		for range c.Horizontal().KeepIter() {
			if snap.State == bgjob.StateRunning {
				c.Label(spec.label + ": capturing…").Send()
			} else if c.Button(inst.ids.PrepareStr("pprof-cap-"+spec.key), c.Atoms().Text("Capture "+spec.label).Keep()).SendResp().HasPrimaryClicked() {
				profiles.startCapture(spec, inst.bus, inst.tasks)
			}
			if handle != "" {
				if c.Button(inst.ids.PrepareStr("pprof-open-"+spec.key), c.Atoms().Text("Explore").Keep()).SendResp().HasPrimaryClicked() {
					profiles.explore(spec, inst.bus)
				}
				c.Label(fmt.Sprintf("keelson('%s') · rev %d · %d rows", handle, revision, rows)).Send()
			}
		}
		if snap.State == bgjob.StateRunning {
			if jobprogress.Render(jobprogress.Input{
				Fraction: snap.Fraction,
				EtaMs:    snap.EtaMs,
				Note:     snap.Note,
				CancelId: inst.ids.PrepareStr("pprof-cancel-" + spec.key),
			}) {
				e.job.Cancel()
			}
		}
		if snap.State == bgjob.StateFailed && snap.Err != nil {
			c.Label("capture failed: " + snap.Err.Error()).Send()
		}
		if lastErr != "" {
			c.Label(lastErr).Send()
		}
		c.AddSpace(styletokens.PaddingHair(styletokens.DensityFromEnv()))
	}

	c.AddSpace(styletokens.PaddingInner(styletokens.DensityFromEnv()))
	c.Label("Block and mutex profiles are omitted: they stay empty unless their runtime rates are set, and imzrt does not mutate runtime tunables.").Send()
}
