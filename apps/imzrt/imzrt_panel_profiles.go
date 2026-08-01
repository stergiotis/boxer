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
// label, and the raw-bytes capture step.
type profileKindSpec struct {
	key     string
	label   string
	capture func(ctx context.Context, report bgjob.Reporter) ([]byte, error)
}

// profileKinds is the UI order. CPU runs a sampling window; the others
// snapshot instantly.
var profileKinds = []profileKindSpec{
	{key: "cpu", label: "CPU (10 s)", capture: captureCPU(cpuCaptureDuration)},
	{key: "heap", label: "Heap", capture: captureLookup("heap")},
	{key: "allocs", label: "Allocs", capture: captureLookup("allocs")},
	{key: "goroutine", label: "Goroutines", capture: captureLookup("goroutine")},
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

// profileSeedSql is the buffer a fresh explore window opens with: top
// functions by self cost over the published dataset. One statement —
// grammar1 parses nothing else — and the handle is spliced as a literal
// (keelson() resolution runs before parameter substitution).
func profileSeedSql(handle string) string {
	return fmt.Sprintf(
		"SELECT leaf AS fn, pkg, sum(value) AS self\n"+
			"FROM keelson('%s')\n"+
			"GROUP BY fn, pkg\n"+
			"ORDER BY self DESC\n"+
			"LIMIT 100", handle)
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
func (h *profilesHub) explore(key string, bus app.BusI) {
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
			Sql:      profileSeedSql(handle),
			AutoRun:  true,
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
	c.Label("Captures profile this process, publishes it as an ad-hoc dataset (alias pprof_<kind>, stable handle across re-captures), and Explore opens a play window on it.").Send()
	c.AddSpace(4)

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
					profiles.explore(spec.key, inst.bus)
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
		c.AddSpace(2)
	}

	c.AddSpace(4)
	c.Label("Block and mutex profiles are omitted: they stay empty unless their runtime rates are set, and imzrt does not mutate runtime tunables.").Send()
}
