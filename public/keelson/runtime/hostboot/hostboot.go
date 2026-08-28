package hostboot

import (
	"context"
	"encoding/binary"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/coveragebus"
	"github.com/stergiotis/boxer/public/keelson/runtime/covscrape"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker/pickerbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/heartbeat"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthost"
	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmscrape"
	tasksupervisor "github.com/stergiotis/boxer/public/keelson/runtime/task/supervisor"
	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/coverage"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/runtimestatus"
	imzhost "github.com/stergiotis/boxer/public/thestack/imzero2/host"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

const (
	// DefaultShutdownGrace bounds how long a signal-driven shutdown waits for
	// the render loop to reach a frame boundary before the process is forced
	// down.
	DefaultShutdownGrace = 10 * time.Second
	// DefaultFactsPingTimeout is how long the facts store waits for ClickHouse
	// before falling back to memory.
	DefaultFactsPingTimeout = 2 * time.Second
	// persistOpenTimeout bounds the persist store backend's DDL over HTTP: a
	// server that answered the ping and then stalls must not hang boot.
	persistOpenTimeout = 15 * time.Second
	// serviceStopTimeout bounds each service's shutdown in Close.
	serviceStopTimeout = 5 * time.Second
	// sysmetricsInterval is the co-located scraper's sampling period.
	sysmetricsInterval = time.Second
)

// Services selects the optional bus-backed services Boot wires (ADR-0211
// §SD2). The zero value boots identity, facts, heartbeat, the bus with its
// audit sink, the task supervisor and the window host — the pieces an app
// needs for the ADR-0026 lifecycle — and nothing else. Each true field
// adds one service; a service that fails to start is logged and skipped,
// never fatal, as in the carousel.
type Services struct {
	// Fs is the fs.* Powerbox: file dialogs driven by the per-frame picker
	// overlay (ADR-0026 §SD7).
	Fs bool
	// Persist is runtime.persist.*, backed by boxer.persiststate when the
	// facts store reached ClickHouse and by memory otherwise.
	Persist bool
	// ChLocal is ch.local.exec.*: lazily created `clickhouse local` pools
	// (ADR-0028 §SD9).
	ChLocal bool
	// AdhocData is the encrypted ad-hoc dataset capability (ADR-0134). It
	// needs ChLocal; without it the service is skipped.
	AdhocData bool
	// Clipboard is clipboard.*: copies drained into egui copy ops each frame.
	Clipboard bool
	// Coverage samples Go coverage counters onto the bus when
	// BOXER_COVERAGE_INTERVAL asks for it (ADR-0169).
	Coverage bool
	// Sysmetrics publishes system metrics onto the host bus (ADR-0090): a
	// co-located /proc scraper, or a bridge from NATS when
	// IMZERO2_SYSMETRICS_NATS_URL is set.
	Sysmetrics bool
	// Introspect serves the keelson introspection tables over HTTP when
	// KEELSON_INTROSPECT_HTTP_LISTEN is set (ADR-0094).
	Introspect bool
}

// AllServices is every service on — the carousel's configuration.
func AllServices() Services {
	return Services{
		Fs: true, Persist: true, ChLocal: true, AdhocData: true,
		Clipboard: true, Coverage: true, Sysmetrics: true, Introspect: true,
	}
}

// SeedWindow is one window opened with a typed launch config at boot
// (ADR-0211 §SD3): the window-host form of ADR-0135's `windowhost.open`
// request. Kind must match the app manifest's LaunchKind and Config must
// pass that kind's registered probe; a failure is a boot error, unlike the
// best-effort LaunchApps seeds, because the caller asked for this exact
// window.
type SeedWindow struct {
	AppId  app.AppIdT
	Kind   string
	Config []byte
}

// Options configures Boot. The zero value is a usable minimal host over
// app.DefaultRegistry with no optional services and no seeded windows.
type Options struct {
	// Log is the base logger; the run id is stamped onto it by Boot.
	Log zerolog.Logger
	// Registry is the app registry the window host serves; nil is
	// app.DefaultRegistry.
	Registry *app.Registry
	// Services selects the optional services.
	Services Services
	// LaunchApps are pre-resolved apps opened without a config, best-effort
	// (a failed open is logged and skipped). In screenshot mode
	// (IMZERO2_SCREENSHOT_DIR set) they are the tour renderers instead and
	// at least one is required.
	LaunchApps []app.AppI
	// SeedWindows are windows opened with a launch config; see SeedWindow.
	// Ignored in screenshot mode.
	SeedWindows []SeedWindow
	// Facts, when non-nil, replaces the ClickHouse-or-memory facts store
	// chosen from the CLICKHOUSE_* environment. Tests and adopters that
	// never persist pass factsstore.NewInMemoryFactsStore().
	Facts factsstore.FactsStoreI
	// FactsPingTimeout bounds the ClickHouse reachability probe; zero is
	// DefaultFactsPingTimeout.
	FactsPingTimeout time.Duration
	// ExtraAuditSinks receive every audited bus request beside the durable
	// facts sink (the carousel adds the capability inspector's counters).
	ExtraAuditSinks []audit.AuditSinkI
	// VideoOutput and HelpHost enable the corresponding chrome menus
	// (imzhost.ChromeConfig).
	VideoOutput bool
	HelpHost    bool
	// KeepCoreDumps leaves core dumps enabled. By default they are disabled
	// before any key or decrypted buffer can exist (ADR-0134 §SD8).
	KeepCoreDumps bool
	// BeforeFirstFrame runs once before the first frame is rendered; may be
	// nil.
	BeforeFirstFrame func() error
	// AfterHost runs once the bus, services and window host exist and before
	// introspection starts — the place for registrations that need the bus
	// or the introspection registry (passes, SQL vocabularies, applet
	// stores). An error aborts Boot. May be nil.
	AfterHost func(rt *Runtime) error
	// ShutdownGrace bounds a signal-driven shutdown; zero is
	// DefaultShutdownGrace.
	ShutdownGrace time.Duration
	// ScreenshotDir, when non-empty, selects screenshot mode with that
	// destination; empty reads IMZERO2_SCREENSHOT_DIR. Env values are cached
	// process-wide on first read, so tests set this rather than the variable.
	ScreenshotDir string
}

// Runtime is the booted host: every wired piece, exported so a caller can
// label, inspect or extend it, plus the render loop and the shutdown edge.
type Runtime struct {
	RunInfo *runinfo.Inst
	// Facts is the audit trail; IsChStore tells whether it reached ClickHouse.
	Facts     factsstore.FactsStoreI
	IsChStore bool
	Bus       *inprocbus.Inst
	// Fs, Persist, ChLocal, Adhoc, Clipboard and Tasks are nil when the
	// service is off or failed to start.
	Fs        *fsbroker.Service
	Persist   *persist.Service
	ChLocal   *chlocalbroker.Service
	Adhoc     *adhocdata.Service
	Clipboard *clipboardbroker.Service
	Tasks     *tasksupervisor.Supervisor
	// PersistBackend is the persist backend label ("store" / "mem") and
	// PersistExec the executor behind a store backend, nil otherwise.
	PersistBackend string
	PersistExec    recordstore.ExecutorI
	// Introspect is the shared introspection registry (populated whether or
	// not the HTTP host serves it).
	Introspect *introspect.Registry
	Coverage   *coverage.Sampler
	Status     *runtimestatus.Snapshot
	// Host is the window host; nil in screenshot mode.
	Host *windowhost.Inst
	// Renderers are the per-frame render functions in order.
	Renderers      []func() error
	ScreenshotMode bool

	opts      Options
	heartbeat *heartbeat.Inst
	cleanups  []func()
	reapOnce  sync.Once
	closeOnce sync.Once
}

// Boot wires the runtime in the carousel's order (ADR-0211 §SD1): identity,
// facts store, runtime-start row, heartbeat, bus with audit, the selected
// services, task supervisor, status snapshot, window host or screenshot
// renderers, AfterHost, introspection. It does not create the imzero2
// application; Run does. Every failure of an optional service is logged and
// leaves that service nil; only a seeded window that cannot open or an
// AfterHost error abort.
func Boot(ctx context.Context, opts Options) (rt *Runtime, err error) {
	rt = &Runtime{opts: opts}
	logger := opts.Log
	if !opts.KeepCoreDumps {
		adhocdata.DisableCoreDumps(logger)
	}

	// Identity first so every line below carries run_id.
	runInst, riErr := runinfo.Init()
	if riErr != nil {
		logger.Warn().Err(riErr).Msg("runinfo init failed; continuing without run identity")
	} else {
		logger = runinfo.TagLogger(logger, runInst)
		logger.Info().
			Str("run_id", runInst.RunId).
			Str("hostname", runInst.Hostname).
			Int("pid", runInst.Pid).
			Str("go_version", runInst.GoVersion).
			Str("vcs_revision", runInst.VcsRevision).
			Bool("vcs_modified", runInst.VcsModified).
			Str("module_path", runInst.ModulePath).
			Str("component", topo.Self()).
			Msg("runinfo: process boot")
	}
	rt.RunInfo = runInst
	rt.opts.Log = logger

	// Facts store: ConfigFromEnv, not Defaults — a server that needs
	// credentials is reached with CLICKHOUSE_USER / CLICKHOUSE_PASSWORD and
	// CLICKHOUSE_ENDPOINT repoints it. Every row carries the run (ADR-0191
	// §SD3).
	factsCfg := chstore.ConfigFromEnv()
	if runInst != nil {
		factsCfg.RunId = runInst.RunId
	}
	if opts.Facts != nil {
		rt.Facts = opts.Facts
	} else {
		pingTimeout := opts.FactsPingTimeout
		if pingTimeout <= 0 {
			pingTimeout = DefaultFactsPingTimeout
		}
		rt.Facts, rt.IsChStore = chstore.NewWithFallback(factsCfg, logger, pingTimeout)
	}
	if runInst != nil {
		_, wErr := rt.Facts.WriteRuntimeStart(factsstore.RuntimeStartRow{
			RunId:        runInst.RunId,
			Hostname:     runInst.Hostname,
			Pid:          runInst.Pid,
			GoVersion:    runInst.GoVersion,
			VcsRevision:  runInst.VcsRevision,
			VcsModified:  runInst.VcsModified,
			VcsBuildInfo: runInst.VcsBuildInfo,
			ModulePath:   runInst.ModulePath,
			Ts:           runInst.StartedAt,
		})
		if wErr != nil {
			logger.Warn().Err(wErr).Msg("runtime-start audit write failed")
		}
		// Liveness signal: a crashed process is told from a clean shutdown by
		// the absence of a recent heartbeat. Stopped by Reap.
		hbInst, hbErr := heartbeat.Start(context.Background(), rt.Facts, runInst.RunId, heartbeat.DefaultInterval, logger)
		if hbErr != nil {
			logger.Warn().Err(hbErr).Msg("heartbeat: start failed")
		} else {
			rt.heartbeat = hbInst
		}
	}

	// In-proc subject router (ADR-0026 §SD3, §SD5), unconditional so apps
	// with declared Caps have a real BusI. The durable facts sink is wrapped
	// asynchronously so audited Requests do not pay a synchronous insert
	// inline; Close drains it last.
	rt.Bus = inprocbus.NewInst(logger)
	auditSink := audit.NewAsyncSink(factsstore.AsAuditSink(rt.Facts), 0)
	rt.cleanups = append(rt.cleanups, auditSink.Close)
	sinks := make(audit.MultiSink, 0, 1+len(opts.ExtraAuditSinks))
	sinks = append(sinks, auditSink)
	sinks = append(sinks, opts.ExtraAuditSinks...)
	rt.Bus.SetAuditSink(sinks)

	rt.bootServices(ctx, factsCfg)

	taskSupBus := rt.Bus.NewClient(tasksupervisor.AppId, tasksupervisor.Caps())
	taskSup := tasksupervisor.New(taskSupBus, rt.Facts, logger, tasksupervisor.Opts{})
	if startErr := taskSup.Start(); startErr != nil {
		logger.Warn().Err(startErr).Msg("task supervisor: start failed; task.* observable but un-audited")
	} else {
		rt.Tasks = taskSup
		rt.cleanups = append(rt.cleanups, func() { _ = taskSup.Stop() })
	}

	rt.Status = rt.statusSnapshot()

	screenshotDir := opts.ScreenshotDir
	if screenshotDir == "" {
		screenshotDir = imzero2env.ScreenshotDir.Get()
	}
	rt.ScreenshotMode = screenshotDir != ""
	if rt.ScreenshotMode {
		if len(opts.LaunchApps) == 0 {
			err = eh.Errorf("--launch must match at least one app in screenshot mode (IMZERO2_SCREENSHOT_DIR set)")
			return
		}
		for _, a := range opts.LaunchApps {
			logger.Info().Str("id", string(a.Manifest().Id)).Msg("screenshot mode: adding tour renderer")
			rt.Renderers = append(rt.Renderers, imzhost.DecorateRenderer(imzhost.AdaptToRenderer(a), rt.chrome(nil, nil)))
		}
	} else {
		err = rt.bootWindowHost()
		if err != nil {
			return
		}
	}

	if opts.AfterHost != nil {
		if err = opts.AfterHost(rt); err != nil {
			err = eh.Errorf("hostboot: after-host hook: %w", err)
			return
		}
	}

	rt.bootIntrospect()
	return
}

// bootServices starts the selected optional services in the carousel's
// order: fs, persist, chlocal, adhoc, clipboard, coverage.
func (rt *Runtime) bootServices(ctx context.Context, factsCfg chstore.Config) {
	logger := rt.opts.Log
	svc := rt.opts.Services
	if svc.Fs {
		fsSvc, fsErr := fsbroker.NewService(rt.Bus, logger)
		if fsErr != nil {
			logger.Warn().Err(fsErr).Msg("fsbroker: service start failed; fs.* will be unbound")
		} else {
			rt.Fs = fsSvc
		}
	}
	if svc.Persist {
		persistCtx, persistCancel := context.WithTimeout(ctx, persistOpenTimeout)
		backend, label, backendClose, exec := selectPersistBackend(persistCtx, factsCfg, rt.IsChStore, logger)
		persistCancel()
		rt.cleanups = append(rt.cleanups, backendClose)
		persistSvc, pErr := persist.NewService(rt.Bus, logger, backend)
		if pErr != nil {
			logger.Warn().Err(pErr).Msg("persist: service start failed; runtime.persist.* will be unbound")
		} else {
			rt.Persist = persistSvc
			rt.PersistBackend = label
			rt.PersistExec = exec
			rt.cleanups = append(rt.cleanups, persistSvc.Close)
		}
	}
	if svc.ChLocal {
		chlocalSvc, chErr := chlocalbroker.NewService(rt.Bus, chlocalpool.Config{}, logger)
		if chErr != nil {
			logger.Warn().Err(chErr).Msg("chlocalbroker: service start failed; ch.local.* will be unbound")
		} else {
			rt.ChLocal = chlocalSvc
			rt.cleanups = append(rt.cleanups, func() {
				stopCtx, cancel := context.WithTimeout(context.Background(), serviceStopTimeout)
				defer cancel()
				_ = chlocalSvc.Stop(stopCtx)
			})
		}
	}
	rt.Introspect = introspect.NewRegistry()
	if svc.AdhocData && rt.ChLocal != nil {
		adhocSvc, adhocErr := adhocdata.NewService(adhocdata.Config{
			Bus:      rt.Bus,
			Registry: rt.Introspect,
			Keys:     rt.ChLocal.KeyStore(),
			Log:      logger,
		})
		if adhocErr != nil {
			logger.Warn().Err(adhocErr).Msg("adhocdata: service start failed; adhoc.* will be unbound")
		} else {
			rt.Adhoc = adhocSvc
			rt.cleanups = append(rt.cleanups, func() {
				stopCtx, cancel := context.WithTimeout(context.Background(), serviceStopTimeout)
				defer cancel()
				_ = adhocSvc.Close(stopCtx)
			})
		}
	}
	if svc.Clipboard {
		clipSvc, clipErr := clipboardbroker.NewService(rt.Bus, logger)
		if clipErr != nil {
			logger.Warn().Err(clipErr).Msg("clipboardbroker: service start failed; clipboard.* will be unbound")
		} else {
			rt.Clipboard = clipSvc
			rt.cleanups = append(rt.cleanups, clipSvc.Close)
		}
	}
	if svc.Coverage {
		if covInterval, covEnabled := coveragebus.IntervalFromEnv(); covEnabled {
			covPub := rt.Bus.NewClient(coveragebus.ServiceAppId, []app.SubjectFilter{
				{Pattern: coveragebus.SubjectWildcard, Direction: app.CapDirectionPub},
			})
			if s, _, cerr := covscrape.StartCoverageSampler(context.Background(), covPub, covscrape.DefaultHostToken(), covInterval, logger); cerr != nil {
				logger.Info().Err(cerr).Msg("hostboot: coverage sampling unavailable; coverage tables will be empty")
			} else {
				rt.Coverage = s
			}
		}
	}
}

// bootWindowHost builds the window host over the registry, wires bus, audit,
// the metrics plane and the open service before any window opens (SetBus
// after Open has no retroactive effect), seeds the requested windows, and
// composes the decorated per-frame renderer.
func (rt *Runtime) bootWindowHost() (err error) {
	logger := rt.opts.Log
	reg := rt.opts.Registry
	if reg == nil {
		reg = app.DefaultRegistry
	}
	host := windowhost.NewInst(reg, logger)
	host.SetBus(rt.Bus)
	if rt.opts.Services.Sysmetrics {
		rt.bootSysmetrics()
	}
	if rt.RunInfo != nil {
		host.SetAudit(rt.RunInfo.RunId, rt.Facts)
	}
	// App-launch requests (ADR-0135): apps holding the cap open other apps
	// with typed launch configs.
	if _, osErr := windowhost.NewOpenService(rt.Bus, host, logger); osErr != nil {
		logger.Warn().Err(osErr).Msg("hostboot: windowhost open service unavailable; windowhost.open requests will time out")
	}
	for _, a := range rt.opts.LaunchApps {
		id := a.Manifest().Id
		if _, openErr := host.Open(id); openErr != nil {
			logger.Warn().Err(openErr).Str("id", string(id)).Msg("windowhost seed: open failed, skipping")
			continue
		}
		logger.Info().Str("id", string(id)).Msg("windowhost seed: opened window")
	}
	for _, s := range rt.opts.SeedWindows {
		if _, openErr := host.OpenWithConfig(s.AppId, s.Kind, s.Config); openErr != nil {
			err = eb.Build().Str("id", string(s.AppId)).Str("kind", s.Kind).Errorf("hostboot: seed window: %w", openErr)
			return
		}
		logger.Info().Str("id", string(s.AppId)).Str("kind", s.Kind).Msg("windowhost seed: opened configured window")
	}
	rt.Host = host

	// Distinct id stacks for the body (reset at the top of each Frame), the
	// Apps menu (rendered in the top bar before the body's reset) and the
	// picker overlay, so none of them derives stale ids from another.
	bodyIds := c.NewWidgetIdStack()
	menuIds := c.NewWidgetIdStack()
	var bridgeIds *c.WidgetIdStack
	var fsBridge *pickerbridge.Bridge
	if rt.Fs != nil {
		fsBridge = pickerbridge.NewBridge(rt.Fs, logger, pickerbridge.Config{})
		bridgeIds = c.NewWidgetIdStack()
	}
	clip := rt.Clipboard
	inner := func() (err error) {
		bodyIds.Reset()
		err = host.Frame(bodyIds)
		if fsBridge != nil {
			bridgeIds.Reset()
			fsBridge.Render(bridgeIds)
		}
		// Copies the broker accumulated off the bus this frame become one
		// CopyTextToClipboard op each; the op rides the frame-scoped egui
		// Context, so no panel or id stack is needed.
		if clip != nil {
			for _, text := range clip.DrainPending() {
				c.CopyTextToClipboard(text)
			}
		}
		return
	}
	rt.Renderers = append(rt.Renderers, imzhost.DecorateRenderer(inner, rt.chrome(func() {
		menuIds.Reset()
		host.RenderAppsMenu(menuIds)
	}, host)))
	logger.Info().Int("initialWindows", len(rt.opts.LaunchApps)+len(rt.opts.SeedWindows)).Msg("window host: started")
	return
}

// bootSysmetrics puts the system-metrics plane (ADR-0090) on the host bus:
// bridged from NATS when IMZERO2_SYSMETRICS_NATS_URL is set (an external
// sysmetricsd reads /proc in its own sandbox), otherwise a co-located
// scraper. Process-lifetime; failures leave the metric panels empty.
func (rt *Runtime) bootSysmetrics() {
	logger := rt.opts.Log
	metricPub := rt.Bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
	})
	if natsURL := sysmetricsbus.NatsURL.Get(); natsURL != "" {
		natsClient, nerr := natsbus.Connect(natsbus.Options{URL: natsURL, AppId: sysmetricsbus.ServiceAppId})
		if nerr != nil {
			logger.Warn().Err(nerr).Str("url", natsURL).Msg("hostboot: sysmetrics NATS bridge connect failed; metric panels will be empty")
			return
		}
		if _, berr := sysmetricsbus.Bridge(natsClient, metricPub, sysmetricsbus.BundleSubjectWildcard()); berr != nil {
			logger.Warn().Err(berr).Msg("hostboot: sysmetrics NATS bridge subscribe failed")
			_ = natsClient.Close()
			return
		}
		logger.Info().Str("url", natsURL).Msg("hostboot: bridging system metrics from NATS onto the host bus")
		return
	}
	if _, serr := sysmscrape.StartScraper(context.Background(), metricPub, sysmetricsbus.DefaultHostToken(), sysmetricsInterval, logger); serr != nil {
		logger.Warn().Err(serr).Msg("hostboot: sysmetrics scraper unavailable; metric panels will be empty")
	}
}

// bootIntrospect starts the introspection HTTP host when the service is on;
// the registry itself exists regardless so late registrations land.
func (rt *Runtime) bootIntrospect() {
	if !rt.opts.Services.Introspect {
		return
	}
	logger := rt.opts.Log
	deps := introspecthost.Deps{
		WindowHost:       rt.Host,
		Bus:              rt.Bus,
		ChlocalAvailable: rt.ChLocal != nil,
		Registry:         rt.Introspect,
		Facts:            rt.Facts,
		PersistExec:      rt.PersistExec,
		Log:              logger,
	}
	if rt.ChLocal != nil {
		deps.Decryptor = rt.ChLocal
	}
	if rt.Coverage != nil {
		deps.Coverage = rt.Coverage
	}
	if rt.Tasks != nil {
		deps.Tasks = rt.Tasks
	}
	stop, ierr := introspecthost.Start(deps)
	if ierr != nil {
		logger.Warn().Err(ierr).Msg("introspect: table source unavailable")
		return
	}
	rt.cleanups = append(rt.cleanups, func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), serviceStopTimeout)
		defer cancel()
		_ = stop(stopCtx)
	})
}

func (rt *Runtime) chrome(extraMenus func(), host *windowhost.Inst) imzhost.ChromeConfig {
	return imzhost.ChromeConfig{
		ExtraMenus:  extraMenus,
		Status:      rt.Status,
		Host:        host,
		VideoOutput: rt.opts.VideoOutput,
		HelpHost:    rt.opts.HelpHost,
	}
}

func (rt *Runtime) statusSnapshot() (s *runtimestatus.Snapshot) {
	s = &runtimestatus.Snapshot{
		BusActive:      rt.Bus != nil,
		FsBrokerActive: rt.Fs != nil,
	}
	if rt.RunInfo != nil && len(rt.RunInfo.RunId) >= 8 {
		s.RunIdShort = rt.RunInfo.RunId[:8]
	} else {
		s.RunIdShort = "standalone"
	}
	if rt.IsChStore {
		s.FactsBackend = "ch"
	} else {
		s.FactsBackend = "mem"
	}
	if rt.Persist != nil {
		s.PersistBackend = rt.PersistBackend
	}
	return
}

// selectPersistBackend follows the facts-store verdict: a live ClickHouse
// gets the store backend on boxer.persiststate so app state outlives the
// process; otherwise memory, since a durable path that cannot reach a server
// gains nothing. The labels are the ones the status bar and the capability
// inspector show.
func selectPersistBackend(ctx context.Context, cfg chstore.Config, isChStore bool, logger zerolog.Logger) (backend persist.StorageBackendI, label string, closeFn func(), exec recordstore.ExecutorI) {
	closeFn = func() {}
	if isChStore {
		storeExec, err := storeexec.New(chclient.New(chclient.Config{
			URL: cfg.URL, User: cfg.User, Password: cfg.Password,
		}, nil), nil)
		if err == nil {
			var sb *persist.StoreBackend
			sb, err = persist.OpenStoreBackend(ctx, storeExec, nil)
			if err == nil {
				backend = sb
				label = "store"
				closeFn = sb.Close
				exec = storeExec
				return
			}
		}
		logger.Warn().Err(err).
			Msg("persist: store backend construction failed; falling back to in-memory (app state will not survive restart)")
	}
	backend = persist.NewMemoryBackend()
	label = "mem"
	return
}

// OnClose registers fn to run at Close, after the window host is reaped and
// before the services started earlier stop (cleanups run in reverse
// registration order). For callers that start something of their own on the
// bus, typically from AfterHost.
func (rt *Runtime) OnClose(fn func()) {
	rt.cleanups = append(rt.cleanups, fn)
}

// Reap runs the closing edge once: every window is left, unmounted and
// unloaded (ADR-0188), then the heartbeat stops. Safe to call more than once
// and before Close.
func (rt *Runtime) Reap() {
	rt.reapOnce.Do(func() {
		if rt.Host != nil {
			rt.Host.ReapAll("shutdown")
		}
		rt.heartbeat.Stop()
	})
}

// Close reaps, then stops the services in reverse start order and drains the
// audit sink last. Idempotent.
func (rt *Runtime) Close() {
	rt.closeOnce.Do(func() {
		rt.Reap()
		for i := len(rt.cleanups) - 1; i >= 0; i-- {
			rt.cleanups[i]()
		}
	})
}

// Run creates the imzero2 application from cfg, installs the signal handler
// (SIGINT / SIGTERM reap the windows, shut the application down and force
// exit after the grace period), runs the render loop until the window
// closes, and Closes the runtime. It returns the application's error.
func (rt *Runtime) Run(cfg *application.Config) (err error) {
	defer rt.Close()
	logger := rt.opts.Log
	u := runtime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)
	application_, err := application.NewApplication(cfg, u)
	if err != nil {
		err = eh.Errorf("unable to create application: %w", err)
		return
	}
	grace := rt.opts.ShutdownGrace
	if grace <= 0 {
		grace = DefaultShutdownGrace
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		sig, ok := <-sigCh
		if !ok {
			return
		}
		logger.Info().Str("signal", sig.String()).Msg("hostboot: caught signal, shutting down")
		rt.Reap()
		application_.Shutdown()
		time.AfterFunc(grace, func() {
			logger.Warn().Dur("grace", grace).Msg("hostboot: shutdown did not complete within the grace period, forcing exit")
			os.Exit(1)
		})
	}()

	application_.FffiEstablishedHandler = func(fffi *runtime.Fffi2[*runtime.Unmarshaller]) error {
		typed.SetCurrentFffiVar(fffi)
		return nil
	}
	application_.BeforeFirstFrameInitHandler = func() error {
		if rt.opts.BeforeFirstFrame != nil {
			return rt.opts.BeforeFirstFrame()
		}
		return nil
	}
	renderers := rt.Renderers
	application_.RenderLoopHandler = func() (err error) {
		for _, r := range renderers {
			if err = r(); err != nil {
				return
			}
		}
		return
	}
	if err = application_.Launch(); err != nil {
		err = eh.Errorf("unable to launch application: %w", err)
		return
	}
	if err = application_.Run(); err != nil {
		err = eh.Errorf("unable to run application: %w", err)
		return
	}
	return
}
