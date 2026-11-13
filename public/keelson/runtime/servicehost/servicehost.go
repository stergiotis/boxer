package servicehost

import (
	"context"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// DefaultShutdownDrain bounds how long ReapAll waits for the services'
	// lifetime scopes after their Unmount, before it closes their bus
	// clients regardless. It must stay below the host's shutdown grace.
	DefaultShutdownDrain = 5 * time.Second
	// maxLaunchConfigBytes is the size cap on a seeded config, the same cap
	// windowhost applies to a launch request (ADR-0135 §SD4).
	maxLaunchConfigBytes = 64 << 10

	// StopReasonShutdown, StopReasonMountError and StopReasonUnmountError are
	// the stop reasons a service's "stopped" lifecycle row carries
	// (ADR-0352 §SD2); the first two are windowhost's words for
	// the same edges.
	StopReasonShutdown     = "shutdown"
	StopReasonMountError   = "mount-error"
	StopReasonUnmountError = "unmount-error"
)

// StateE is where a service is in its life, as keelson('services') shows
// it.
type StateE uint8

const (
	// StateNotSelected: a headless app the predicate did not select; it
	// was never mounted.
	StateNotSelected StateE = 0
	// StateStarting: selected; its Mount has not returned.
	StateStarting StateE = 1
	// StateRunning: Mount returned nil and the service has not been reaped.
	StateRunning StateE = 2
	// StateStopped: reaped, through Unmount, at shutdown.
	StateStopped StateE = 3
	// StateFailed: Mount returned an error, or Unmount did, or it was
	// reaped before its Mount returned.
	StateFailed StateE = 4
)

func (inst StateE) String() (s string) {
	switch inst {
	case StateStarting:
		s = "starting"
	case StateRunning:
		s = "running"
	case StateStopped:
		s = "stopped"
	case StateFailed:
		s = "failed"
	default:
		s = "not_selected"
	}
	return
}

// Seed is a launch config delivered to one service at Mount, the service
// form of hostboot's SeedWindow (ADR-0135 §SD4): Kind must match the app
// manifest's LaunchKind and Config must pass that kind's registered probe,
// or Start fails.
type Seed struct {
	AppId  app.AppIdT
	Kind   string
	Config []byte
}

// Config wires a host.
type Config struct {
	// Registry holds the apps; nil is app.DefaultRegistry.
	Registry *app.Registry
	// Bus mints each service's client over the host's transport; nil hands
	// every service a NoopBus.
	Bus app.BusProvider
	// Facts receives the app-lifecycle rows; nil writes none.
	Facts factsstore.FactsStoreI
	// RunId is the process run every row carries; empty when no runinfo was
	// wired.
	RunId string
	Log   zerolog.Logger
	// AllocateKey mints instance keys. A host with windows passes the
	// window host's allocator so no window and service share a key; nil
	// counts from one, which a process without a window host can afford.
	AllocateKey func() uint64
	// MountValues are handed to every service's mount context
	// (app.MountValuesI), as the window host hands them to every window.
	MountValues map[any]any
	// Subscriptions, when set, lists the bus's live subscriptions once every
	// selected service's Mount has returned, so the host can say which of a
	// service's publish caps no subscriber answers in this process
	// (ADR-0352 §SD6). nil skips the check.
	Subscriptions func() []inprocbus.SubscriptionInfo
	// ShutdownDrain bounds the drain in ReapAll; zero is DefaultShutdownDrain.
	ShutdownDrain time.Duration
}

// Row is one headless app's line in keelson('services').
type Row struct {
	Id          app.AppIdT
	Selected    bool
	State       StateE
	InstanceKey uint64
	StartedAt   time.Time
	StoppedAt   time.Time
	StopReason  string
	LastError   string
}

// service is one headless app the host knows, mounted or not.
type service struct {
	row      Row
	manifest app.Manifest
	appInst  app.AppI
	mountCtx *app.StaticMountContext
	stop     chan struct{}
	stopOnce sync.Once
	scope    *lifetime.Scope
	labels   context.Context
	// mounted is closed when Mount has returned; mountErr is its result.
	mounted  chan struct{}
	mountErr error
}

// Inst is the service host.
type Inst struct {
	cfg      Config
	mu       sync.Mutex
	services []*service // sorted by id; one per headless app registered at Start
	started  bool
	reaped   bool
	nextKey  uint64
}

// New builds a host over cfg. Nothing runs until Start.
func New(cfg Config) (inst *Inst) {
	if cfg.Registry == nil {
		cfg.Registry = app.DefaultRegistry
	}
	if cfg.ShutdownDrain <= 0 {
		cfg.ShutdownDrain = DefaultShutdownDrain
	}
	inst = &Inst{cfg: cfg}
	return
}

// Start records every headless app the registry holds and mounts the
// selected ones, each on a goroutine of its own, in sorted-id order. An id
// that is not a registered headless app is logged and skipped; a seed
// whose app is not selected is skipped, and one that does not fit its app
// is an error, because the caller asked for that exact delivery. Start
// runs once; a second call is an error.
func (inst *Inst) Start(ctx context.Context, selected []app.AppIdT, seeds []Seed) (err error) {
	inst.mu.Lock()
	if inst.started {
		inst.mu.Unlock()
		err = eb.Build().Errorf("servicehost: started twice")
		return
	}
	inst.started = true
	inst.mu.Unlock()

	want := make(map[app.AppIdT]struct{}, len(selected))
	for _, id := range selected {
		want[id] = struct{}{}
	}
	var svcs []*service
	for _, r := range inst.cfg.Registry.Registrations() {
		if r.Manifest.Surface != app.SurfaceHeadless {
			continue
		}
		s := &service{row: Row{Id: r.Manifest.Id}, manifest: r.Manifest}
		if _, ok := want[r.Manifest.Id]; ok {
			s.row.Selected = true
			delete(want, r.Manifest.Id)
		}
		svcs = append(svcs, s)
	}
	for id := range want {
		inst.cfg.Log.Warn().Str("id", string(id)).Msg("servicehost: selected id is not a registered headless app; skipped")
	}
	byId := make(map[app.AppIdT]*service, len(svcs))
	for _, s := range svcs {
		byId[s.row.Id] = s
	}
	cfgs := make(map[app.AppIdT]Seed, len(seeds))
	for _, sd := range seeds {
		s, ok := byId[sd.AppId]
		if !ok || !s.row.Selected {
			inst.cfg.Log.Warn().Str("id", string(sd.AppId)).Str("kind", sd.Kind).Msg("servicehost: seed names a service that is not selected; skipped")
			continue
		}
		if err = checkSeed(s.manifest, sd); err != nil {
			return
		}
		cfgs[sd.AppId] = sd
	}
	inst.mu.Lock()
	inst.services = svcs
	inst.mu.Unlock()

	var mounting []*service
	for _, s := range svcs {
		if !s.row.Selected {
			continue
		}
		if mErr := inst.mount(s, cfgs[s.row.Id]); mErr != nil {
			// Open failed: the ctor, not the app's Mount. Recorded as a
			// failed service and left out, like a Mount error.
			inst.cfg.Log.Warn().Err(mErr).Str("id", string(s.row.Id)).Msg("servicehost: service did not open; continuing without it")
			continue
		}
		mounting = append(mounting, s)
	}
	if inst.cfg.Subscriptions != nil && len(mounting) > 0 {
		go inst.reportUnanswered(mounting)
	}
	return
}

// checkSeed applies the window host's launch-config rules to a seed.
func checkSeed(m app.Manifest, sd Seed) (err error) {
	switch {
	case sd.Kind == "":
		err = eb.Build().Str("id", string(sd.AppId)).Errorf("servicehost: seed carries a config without a kind")
	case len(sd.Config) == 0:
		err = eb.Build().Str("id", string(sd.AppId)).Str("kind", sd.Kind).Errorf("servicehost: seed kind claimed but config is empty")
	case m.LaunchKind == "":
		err = eb.Build().Str("id", string(sd.AppId)).Str("kind", sd.Kind).Errorf("servicehost: service accepts no launch config (manifest LaunchKind is empty)")
	case m.LaunchKind != sd.Kind:
		err = eb.Build().Str("id", string(sd.AppId)).Str("kind", sd.Kind).Str("launchKind", m.LaunchKind).Errorf("servicehost: seed kind does not match the service's LaunchKind")
	case len(sd.Config) > maxLaunchConfigBytes:
		err = eb.Build().Str("id", string(sd.AppId)).Int("len", len(sd.Config)).Int("max", maxLaunchConfigBytes).Errorf("servicehost: seed config exceeds the size cap")
	default:
		if cErr := kindcheck.Check(sd.Kind, sd.Config); cErr != nil {
			err = eb.Build().Str("id", string(sd.AppId)).Str("kind", sd.Kind).Errorf("servicehost: seed config bytes refused: %w", cErr)
		}
	}
	return
}

func (inst *Inst) allocateKey() (key uint64) {
	if inst.cfg.AllocateKey != nil {
		key = inst.cfg.AllocateKey()
		return
	}
	inst.mu.Lock()
	inst.nextKey++
	key = inst.nextKey
	inst.mu.Unlock()
	return
}

// mount opens the app, builds its mount context as the window host does,
// writes the "started" row and runs Mount on the service's goroutine.
func (inst *Inst) mount(s *service, sd Seed) (err error) {
	m := s.manifest
	a, err := inst.cfg.Registry.Open(m.Id)
	if err != nil {
		inst.markFailed(s, 0, err, StopReasonMountError, false)
		return
	}
	key := inst.allocateKey()
	var busC app.BusI
	var storageC app.StorageI
	if inst.cfg.Bus != nil {
		caps := append([]app.SubjectFilter(nil), m.Caps...)
		if len(m.PersistedKeys) > 0 {
			caps = append(caps, app.SubjectFilter{
				Pattern:   persist.SubjectPrefix + m.Id.SubjectAlias() + ".>",
				Direction: app.CapDirectionPub,
				Reason:    "host-injected for declared PersistedKeys",
			})
		}
		client, busErr := inst.cfg.Bus.NewBusClient(m.Id, caps)
		if busErr != nil {
			inst.cfg.Log.Warn().Err(busErr).Str("id", string(m.Id)).Msg("servicehost: bus client construction failed; using NoopBus")
		} else {
			busC = client
			if bi, ok := client.(app.BusInstanceI); ok {
				bi.SetInstanceKey(key)
			}
			sc, sErr := persist.NewClient(busC, m.Id)
			if sErr != nil {
				inst.cfg.Log.Warn().Err(sErr).Str("id", string(m.Id)).Msg("servicehost: persist client construction failed; using NoopStorage")
			} else {
				storageC = sc
			}
		}
	}
	logger := app.AppLogger(inst.cfg.Log, m.Id).With().Uint64("instance_id", key).Logger()
	stop := make(chan struct{})
	scope := lifetime.New(nil)
	mc := app.NewStaticMountContext(m.Id, logger, storageC, busC, stop)
	mc.SetLifetime(scope)
	mc.SetInstanceKey(key)
	mc.SetRunId(inst.cfg.RunId)
	if sd.Kind != "" {
		mc.SetLaunchConfig(append([]byte(nil), sd.Config...))
		mc.SetLaunchReason(app.LaunchReasonCaller)
	}
	for k, v := range inst.cfg.MountValues {
		mc.SetMountValue(k, v)
	}
	s.appInst = a
	s.mountCtx = mc
	s.stop = stop
	s.scope = scope
	s.labels = task.LabelContext(m.Id, key)
	s.mounted = make(chan struct{})
	now := time.Now()
	inst.mu.Lock()
	s.row.InstanceKey = key
	s.row.State = StateStarting
	s.row.StartedAt = now
	inst.mu.Unlock()
	inst.writeLifecycle(s, factsstore.AppLifecyclePhaseStarted, "")

	go func() {
		pprof.SetGoroutineLabels(s.labels)
		mErr := a.Mount(mc)
		inst.mu.Lock()
		s.mountErr = mErr
		if mErr == nil && s.row.State == StateStarting {
			s.row.State = StateRunning
		}
		inst.mu.Unlock()
		close(s.mounted)
		if mErr != nil {
			// Mount failed: nothing to unmount; the resources the host
			// minted go at once (ADR-0188's leave and unload).
			inst.cfg.Log.Warn().Err(mErr).Str("id", string(m.Id)).Uint64("instance_id", key).Msg("servicehost: Mount failed; service left out")
			inst.leave(s)
			inst.unload(s)
			inst.markFailed(s, key, mErr, StopReasonMountError, true)
		}
	}()
	return
}

// markFailed records a service that is not running and will not be.
func (inst *Inst) markFailed(s *service, key uint64, cause error, reason string, writeStop bool) {
	inst.mu.Lock()
	s.row.State = StateFailed
	s.row.StoppedAt = time.Now()
	s.row.StopReason = reason
	if cause != nil {
		s.row.LastError = cause.Error()
	}
	if key != 0 {
		s.row.InstanceKey = key
	}
	inst.mu.Unlock()
	if writeStop {
		inst.writeLifecycle(s, factsstore.AppLifecyclePhaseStopped, reason)
	}
}

func (inst *Inst) writeLifecycle(s *service, phase factsstore.AppLifecyclePhaseE, reason string) {
	if inst.cfg.Facts == nil {
		return
	}
	inst.mu.Lock()
	key := s.row.InstanceKey
	inst.mu.Unlock()
	_, err := inst.cfg.Facts.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: inst.cfg.RunId, AppId: s.manifest.Id, TileKey: key, Phase: phase, StopReason: reason,
	})
	if err != nil {
		inst.cfg.Log.Warn().Err(err).Str("id", string(s.manifest.Id)).Str("phase", phase.String()).Msg("servicehost: write app-lifecycle row failed")
	}
}

// leave closes the service's stop channel and retires its scope.
func (inst *Inst) leave(s *service) {
	s.stopOnce.Do(func() {
		if s.stop != nil {
			close(s.stop)
		}
	})
	if s.scope != nil {
		s.scope.Retire()
	}
}

// unload closes the service's bus client.
func (inst *Inst) unload(s *service) {
	if s.mountCtx == nil {
		return
	}
	bc, ok := s.mountCtx.Bus().(app.BusCloserI)
	if !ok {
		return
	}
	if cErr := bc.Close(); cErr != nil {
		inst.cfg.Log.Warn().Err(cErr).Str("id", string(s.manifest.Id)).Msg("servicehost: bus client close")
	}
}

// Wait blocks until every selected service's Mount has returned, or ctx
// ends. Boot does not wait; tests and the served-subjects report do.
func (inst *Inst) Wait(ctx context.Context) (err error) {
	inst.mu.Lock()
	svcs := slices.Clone(inst.services)
	inst.mu.Unlock()
	for _, s := range svcs {
		if s.mounted == nil {
			continue
		}
		select {
		case <-s.mounted:
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
	}
	return
}

// ReapAll stops every running service, in reverse start order, through the
// closing edge: leave, unmount, drain, unload, then the "stopped" row with
// reason. A service whose Mount has not returned is given until the drain
// deadline to return; one that does not is recorded as failed and its
// resources are released without Unmount. Idempotent.
func (inst *Inst) ReapAll(reason string) {
	inst.mu.Lock()
	if inst.reaped {
		inst.mu.Unlock()
		return
	}
	inst.reaped = true
	var live []*service
	for _, s := range inst.services {
		if s.mounted != nil && (s.row.State == StateStarting || s.row.State == StateRunning) {
			live = append(live, s)
		}
	}
	inst.mu.Unlock()
	slices.Reverse(live)
	ctx, cancel := context.WithTimeout(context.Background(), inst.cfg.ShutdownDrain)
	defer cancel()
	// leave: every service sees Cancel() before any Unmount runs, so work
	// that spans services winds down together.
	for _, s := range live {
		inst.leave(s)
	}
	for _, s := range live {
		select {
		case <-s.mounted:
		case <-ctx.Done():
			inst.cfg.Log.Warn().Str("id", string(s.manifest.Id)).Msg("servicehost: Mount did not return before the shutdown drain ended; released without Unmount")
			inst.unload(s)
			inst.markFailed(s, 0, ctx.Err(), reason, true)
			continue
		}
		inst.mu.Lock()
		failedMount := s.mountErr != nil
		inst.mu.Unlock()
		if failedMount {
			// Its own goroutine recorded and released it.
			continue
		}
		stopReason := reason
		pprof.SetGoroutineLabels(s.labels)
		umErr := s.appInst.Unmount(s.mountCtx)
		pprof.SetGoroutineLabels(context.Background())
		if umErr != nil {
			inst.cfg.Log.Warn().Err(umErr).Str("id", string(s.manifest.Id)).Msg("servicehost: Unmount failed")
			stopReason = StopReasonUnmountError
		}
		inst.mu.Lock()
		s.row.StopReason = stopReason
		if umErr != nil {
			s.row.LastError = umErr.Error()
		}
		inst.mu.Unlock()
	}
	// drain, in parallel, within the budget; then unload and record.
	var wg sync.WaitGroup
	for _, s := range live {
		if s.scope == nil || s.row.State == StateFailed {
			continue
		}
		wg.Add(1)
		go func(s *service) {
			defer wg.Done()
			start := time.Now()
			if dErr := s.scope.Drain(ctx); dErr != nil {
				inst.cfg.Log.Warn().Err(dErr).Str("id", string(s.manifest.Id)).Dur("waited", time.Since(start)).Msg("servicehost: the service did not drain before its bus closed")
			}
		}(s)
	}
	wg.Wait()
	for _, s := range live {
		inst.mu.Lock()
		if s.row.State == StateFailed {
			inst.mu.Unlock()
			continue
		}
		if s.row.StopReason == StopReasonUnmountError {
			s.row.State = StateFailed
		} else {
			s.row.State = StateStopped
		}
		s.row.StoppedAt = time.Now()
		stopReason := s.row.StopReason
		inst.mu.Unlock()
		inst.unload(s)
		inst.writeLifecycle(s, factsstore.AppLifecyclePhaseStopped, stopReason)
	}
}

// Rows is a snapshot of every headless app the host knows, sorted by id.
func (inst *Inst) Rows() (rows []Row) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	rows = make([]Row, 0, len(inst.services))
	for _, s := range inst.services {
		rows = append(rows, s.row)
	}
	return
}

// Running is the number of services whose Mount returned nil and that have
// not been reaped.
func (inst *Inst) Running() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, s := range inst.services {
		if s.row.State == StateRunning {
			n++
		}
	}
	return
}

// reportUnanswered waits for the services' Mounts and logs, per service,
// the publish caps no live subscription in this process answers.
func (inst *Inst) reportUnanswered(svcs []*service) {
	for _, s := range svcs {
		<-s.mounted
	}
	subs := inst.cfg.Subscriptions()
	for _, s := range svcs {
		inst.mu.Lock()
		running := s.row.State == StateRunning
		inst.mu.Unlock()
		if !running {
			continue
		}
		if unanswered := UnansweredCaps(subs, s.manifest.Caps); len(unanswered) > 0 {
			inst.cfg.Log.Warn().Str("id", string(s.manifest.Id)).Strs("caps", unanswered).
				Msg("servicehost: no subscriber in this process answers these publish caps; requests on them fail at call time (ADR-0352 SD6)")
		}
	}
}

// UnansweredCaps is the publish-direction patterns of caps that no
// subscription in subs can receive. Two patterns overlap when some concrete
// subject matches both, decided token by token: equal tokens, a `*` on
// either side, or a `>` on either side from there on. Reply inboxes are
// not subscribers of anything a cap names and are skipped.
func UnansweredCaps(subs []inprocbus.SubscriptionInfo, caps []app.SubjectFilter) (unanswered []string) {
	for _, c := range caps {
		if c.Direction != app.CapDirectionPub && c.Direction != app.CapDirectionBoth {
			continue
		}
		answered := false
		for _, sub := range subs {
			if strings.HasPrefix(sub.Pattern, inprocbus.InboxPrefix) {
				continue
			}
			if PatternsOverlap(c.Pattern, sub.Pattern) {
				answered = true
				break
			}
		}
		if !answered {
			unanswered = append(unanswered, c.Pattern)
		}
	}
	return
}

// PatternsOverlap says whether some concrete subject matches both NATS-style
// patterns.
func PatternsOverlap(a, b string) (overlap bool) {
	if a == "" || b == "" {
		return
	}
	at := strings.Split(a, ".")
	bt := strings.Split(b, ".")
	for i := 0; ; i++ {
		if i >= len(at) || i >= len(bt) {
			overlap = len(at) == len(bt)
			return
		}
		if at[i] == ">" || bt[i] == ">" {
			overlap = true
			return
		}
		if at[i] != "*" && bt[i] != "*" && at[i] != bt[i] {
			return
		}
	}
}
