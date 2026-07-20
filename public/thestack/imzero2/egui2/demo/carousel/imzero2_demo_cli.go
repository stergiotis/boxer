package demo

import (
	gocontext "context"
	"encoding/binary"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"

	"github.com/stergiotis/boxer/apps/capinspector"
	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/heartbeat"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthost"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	tasksupervisor "github.com/stergiotis/boxer/public/keelson/runtime/task/supervisor"
	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/runtimestatus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

func NewCommand() *cli.Command {
	cfg := &application.Config{
		MainFontTTF:            "",
		ImZeroSkiaClientConfig: &application.ImZeroClientConfig{},
	}
	return &cli.Command{
		Name: "demo",
		Description: "Launch the imzero2 demo carousel.\n\n" +
			"Discovery:\n" +
			"   imzero2 demo --list                  # PrettyCompact table of every registered app\n" +
			"   imzero2 demo --list --list-format Markdown\n" +
			"   imzero2 demo --list --list-output apps.arrow   # also dump Arrow IPC stream\n" +
			"\n" +
			"Non-interactive launch — bare identifier shorthand:\n" +
			"   imzero2 demo --launch play           # shorthand for: subject_alias = 'play'\n" +
			"   imzero2 demo --launch regex_explorer\n" +
			"\n" +
			"For richer predicates, --launch is a SQL WHERE clause over the\n" +
			"registered-applications table from --list; the runtime wraps it as\n" +
			"`SELECT id FROM table WHERE <expr>` and evaluates via clickhouse-local:\n" +
			"   imzero2 demo --launch \"subject_alias IN ('play','widgets')\"\n" +
			"   imzero2 demo --launch \"legacy_code = 5\"\n" +
			"   imzero2 demo --launch \"category = 'tools'\"\n" +
			"\n" +
			"See doc/howto/launch-apps-non-interactively.md for the full recipe.",
		Flags: slices.Concat(cfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf),
			[]cli.Flag{
				&cli.StringFlag{
					Name:  "launch",
					Usage: "bare identifier `play` (shorthand for `subject_alias = 'play'`) or a SQL WHERE clause over the registered-applications table; the runtime wraps it as `SELECT id FROM table WHERE <expr>` and evaluates via clickhouse-local (run --list to see the column set)",
					Value: "",
				},
				&cli.BoolFlag{
					Name:  "list",
					Usage: "print the registered applications as a table and exit (no client launch)",
				},
				&cli.StringFlag{
					Name:  "list-output",
					Usage: "with --list: also write the raw Arrow IPC stream to this file (for downstream clickhouse-local queries)",
					Value: "",
				},
				&cli.StringFlag{
					Name:  "list-format",
					Usage: "with --list: clickhouse-local --output-format name (PrettyCompact, Pretty, Vertical, Markdown, JSONEachRow, TSV, ...)",
					Value: "PrettyCompact",
				},
			},
		),
		Action: func(context *cli.Context) error {
			if context.Bool("list") {
				return renderManifestList(
					runtimeapp.AllManifests(),
					context.String("list-output"),
					context.String("list-format"),
					os.Stdout,
				)
			}
			nMessages := cfg.FromContext(config.IdentityNameTransf, context)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to create config")
			}
			var application_ *application.Application[*runtime.Unmarshaller]
			var err error

			// ADR-0134 SD8: close the core-dump bridge before any key or
			// decrypted buffer can exist, so a crash cannot spill process
			// memory to disk. Unconditional, best-effort.
			adhocdata.DisableCoreDumps(log.Logger)

			// Runtime identity + audit setup. Best-effort throughout —
			// audit-trail failures never block boot. Identity is taken
			// first so any logging below carries run_id; facts store is
			// chosen second, runtime-start row third, dock-host wiring
			// last.
			runInst, riErr := runinfo.Init()
			if riErr != nil {
				log.Warn().Err(riErr).Msg("runinfo init failed; continuing without run identity")
			} else {
				log.Logger = runinfo.TagLogger(log.Logger, runInst)
				log.Info().
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
			facts, isChStore := chstore.NewWithFallback(chstore.Defaults(), log.Logger, 2*time.Second)
			var heartbeatInst *heartbeat.Inst
			if runInst != nil {
				_, wErr := facts.WriteRuntimeStart(factsstore.RuntimeStartRow{
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
					log.Warn().Err(wErr).Msg("runtime-start audit write failed")
				}
				// Heartbeat ticker — best-effort liveness signal so a
				// crashed process (no runtime-stop, no app-lifecycle stop
				// rows) is distinguishable from a clean shutdown by the
				// absence of a recent heartbeat. Stopped by doReap on
				// shutdown alongside ReapAll.
				hbInst, hbErr := heartbeat.Start(gocontext.Background(),
					facts, runInst.RunId, heartbeat.DefaultInterval, log.Logger)
				if hbErr != nil {
					log.Warn().Err(hbErr).Msg("heartbeat: start failed")
				} else {
					heartbeatInst = hbInst
				}
			}
			_ = isChStore

			// M2 in-proc subject router (ADR-0026 §SD3, §SD5).
			// Constructed unconditionally so apps with declared Caps
			// have a real BusI to talk to; apps with no Caps see
			// permission errors from any Publish/Subscribe — which is
			// the intended lockdown shape. The audit sink lands every
			// allowed request as a runtime.facts row when CH is
			// reachable.
			bus := inprocbus.NewInst(log.Logger)
			// MultiSink fan-out: the durable facts sink lands every
			// audit row in runtime.facts; the capinspector.Tally
			// counter sink keeps per-cap monotonic counts the
			// inspector window renders on cap nodes (Phase 2 of the
			// capability legibility direction).
			// The durable facts sink issues a synchronous insert per audit
			// row, and the bus calls Record inside Client.Request on the
			// caller's goroutine — wrap it in an AsyncSink so audited
			// Requests don't pay that round-trip inline. capinspector.Tally
			// is an in-memory counter feeding the live UI; it stays
			// synchronous. The deferred Close drains buffered audit rows at
			// shutdown (after doReap stops the producers).
			auditSink := audit.NewAsyncSink(factsstore.AsAuditSink(facts), 0)
			defer auditSink.Close()
			bus.SetAuditSink(audit.MultiSink{
				auditSink,
				capinspector.Tally,
			})

			// M2 Phase B: fs.* Powerbox (ADR-0026 §SD7). The service
			// subscribes to fs.> and queues dialog requests; the
			// per-frame pickerbridge drives the egui file picker as
			// an overlay above the window host body. Apps publish
			// fs.dialog.read / .write / .bundle to request a handle;
			// the broker mints an opaque uuid and augments the
			// requesting client's caps to include fs.handle.{uuid}.>
			// — the path is never exposed to the app. Best-effort:
			// a NewService error leaves fs.* unbound (apps timeout
			// on Request) but doesn't block startup.
			fsSvc, fsErr := fsbroker.NewService(bus, log.Logger)
			if fsErr != nil {
				log.Warn().Err(fsErr).Msg("fsbroker: service start failed; fs.* will be unbound")
				fsSvc = nil
			}

			// M2 Phase C: runtime.persist.> Powerbox (ADR-0026 §SD3,
			// §SD6). In-memory backend for now; a runtime.facts-backed
			// backend lands in a follow-up so state writes appear as
			// auditable rows alongside grants + lifecycle events. The
			// host auto-injects runtime.persist.{ownAlias}.> for any
			// app declaring PersistedKeys in its manifest, so apps
			// don't have to repeat that boilerplate.
			persistSvc, pErr := persist.NewService(bus, log.Logger, persist.NewMemoryBackend())
			if pErr != nil {
				log.Warn().Err(pErr).Msg("persist: service start failed; runtime.persist.* will be unbound")
				persistSvc = nil
			}
			defer func() {
				if persistSvc != nil {
					persistSvc.Close()
				}
			}()

			// Capability inspector: tell it which backend the runtime
			// resolved for each cap so the schematic can highlight the
			// effective impl (vs the dim alternatives). isChStore is
			// the chstore.NewWithFallback verdict.
			capinspector.SetActiveBackend(capinspector.CapRun, "runinfo")
			if isChStore {
				capinspector.SetActiveBackend(capinspector.CapFacts, "chstore")
			} else {
				capinspector.SetActiveBackend(capinspector.CapFacts, "inmem")
			}
			capinspector.SetActiveBackend(capinspector.CapBus, "inprocbus")
			if fsSvc != nil {
				capinspector.SetActiveBackend(capinspector.CapFs, "fsbroker")
			}
			if persistSvc != nil {
				capinspector.SetActiveBackend(capinspector.CapPersist, "mem")
			}
			// CapTask backend reads the supervisor wiring: "supervisor"
			// when the audit hook is live; "task" (the producer surface
			// alone) when the supervisor failed to start. Set below
			// after the supervisor block runs so the choice reflects
			// actual wiring rather than intent.

			// ADR-0028 §SD9, M2: chlocalbroker subscribes to
			// ch.local.exec.> and lazy-creates a chlocalpool.Pool per
			// pool name. Best-effort: a start failure leaves
			// ch.local.* unbound (apps will see request timeouts) but
			// doesn't block boot. First consumer is regex_explorer,
			// which declares `ch.local.exec.regex_explorer` in its
			// manifest Caps.
			chlocalSvc, chErr := chlocalbroker.NewService(bus, chlocalpool.Config{}, log.Logger)
			if chErr != nil {
				log.Warn().Err(chErr).Msg("chlocalbroker: service start failed; ch.local.* will be unbound")
				chlocalSvc = nil
			}
			defer func() {
				if chlocalSvc != nil {
					ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 5*time.Second)
					defer cancel()
					_ = chlocalSvc.Stop(ctx)
				}
			}()

			// ADR-0134: the ad-hoc dataset capability. Owns the encrypted
			// store, custodies keys with the broker (its KeyStore), and
			// registers ephemeral handles as queryable providers on
			// introspect.Default. Best-effort: a start failure leaves
			// adhoc.* unbound. The applet query path that binds an engine
			// over this registry lands with the sqlapplet surface (SD4).
			var adhocSvc *adhocdata.Service
			if chlocalSvc != nil {
				var adhocErr error
				adhocSvc, adhocErr = adhocdata.NewService(adhocdata.Config{
					Bus:      bus,
					Registry: introspect.Default,
					Keys:     chlocalSvc.KeyStore(),
					Log:      log.Logger,
				})
				if adhocErr != nil {
					log.Warn().Err(adhocErr).Msg("adhocdata: service start failed; adhoc.* will be unbound")
					adhocSvc = nil
				}
			}
			defer func() {
				if adhocSvc != nil {
					ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 5*time.Second)
					defer cancel()
					_ = adhocSvc.Close(ctx)
				}
			}()

			// Clipboard Powerbox (ADR-0026 Update 2026-05-30): subscribes to
			// clipboard.write and accumulates copy requests off-frame; the
			// windowed renderer drains them each frame and emits the egui
			// copy_text op. Best-effort: a start failure leaves clipboard.*
			// unbound (copies time out on Request) but doesn't block boot.
			// First consumer is the markdown copy button via capdemo.
			clipSvc, clipErr := clipboardbroker.NewService(bus, log.Logger)
			if clipErr != nil {
				log.Warn().Err(clipErr).Msg("clipboardbroker: service start failed; clipboard.* will be unbound")
				clipSvc = nil
			}
			defer func() {
				if clipSvc != nil {
					clipSvc.Close()
				}
			}()

			// ADR-0038 §M3: task supervisor subscribes to task.>,
			// persists every terminal-grade verb (created / done /
			// error / cancel / abandoned) into runtime.facts via
			// factsstore.WriteLog with structured run_id / instance_id
			// / task_id fields, and serves task.list.inflight
			// snapshots. Best-effort: a start failure leaves task.*
			// observable (the M1 producer surface works without a
			// supervisor) but un-audited; the capinspector reports
			// CapTask backend as "task" rather than "supervisor".
			taskSupBus := bus.NewClient(tasksupervisor.AppId, tasksupervisor.Caps())
			taskSup := tasksupervisor.New(taskSupBus, facts, log.Logger, tasksupervisor.Opts{})
			if startErr := taskSup.Start(); startErr != nil {
				log.Warn().Err(startErr).Msg("task supervisor: start failed; task.* observable but un-audited")
				taskSup = nil
			}
			defer func() {
				if taskSup != nil {
					_ = taskSup.Stop()
				}
			}()
			if taskSup != nil {
				capinspector.SetActiveBackend(capinspector.CapTask, "supervisor")
			} else {
				capinspector.SetActiveBackend(capinspector.CapTask, "task")
			}

			// Runtime status snapshot for the bottom-panel readout.
			// Constructed once: backend identities are process-static
			// (a service that fails NewService stays nil for the
			// whole run). Threaded into both screenshot- and windowed-
			// mode renderers so the indicator is visible in either
			// path.
			status := buildStatusSnapshot(runInst, isChStore, bus, fsSvc, persistSvc)

			u := runtime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)

			// ADR-0132 §SD2: mint one Manifest per registered SQL-applet doc
			// before launch resolution, so `--launch <appletId>` and the Apps
			// menu see the minted set. Best-effort per doc — the corpus test
			// is the hard gate; a partially minted set never blocks boot.
			appletCount, appletErrs := sqlapplet.MintManifests(log.Logger)
			for _, mintErr := range appletErrs {
				log.Warn().Err(mintErr).Msg("sqlapplet: mint")
			}
			if appletCount > 0 {
				log.Info().Int("applets", appletCount).Msg("sqlapplet: manifests minted")
			}
			// ADR-0132 Update "O4": the runtime applet store — loads
			// persisted applets (minted after the committed books, which
			// win slug collisions) and serves `applet.store.save` so play
			// can author applets at runtime. Best-effort, never blocks
			// boot.
			if bus != nil {
				appletStore, storeErr := sqlapplet.StartStore(bus, log.Logger)
				if storeErr != nil {
					log.Warn().Err(storeErr).Msg("sqlapplet: applet store unavailable")
				} else {
					defer appletStore.Stop()
				}
			}

			launchApps, resolveErr := resolveLaunchSql(context.String("launch"))
			if resolveErr != nil {
				return eb.Build().Str("launch", context.String("launch")).
					Errorf("--launch: %w", resolveErr)
			}

			// IMZERO2_SCREENSHOT_DIR mode uses the renderer-slice path so
			// the widgets TestDriver runs at top scope without WindowHost
			// wrapping (ADR-0057): launching `widgets` captures every
			// registered Demo in one run. Interactive mode (the common
			// case) builds a WindowHost over app.Registry and seeds it with
			// the resolved --launch apps.
			screenshotMode := imzero2env.ScreenshotDir.Get() != ""
			renderers := make([]func() error, 0, 4)
			var windowHostRef *windowhost.Inst
			if screenshotMode {
				if len(launchApps) == 0 {
					return eh.Errorf("--launch must match at least one app in screenshot mode (IMZERO2_SCREENSHOT_DIR set)")
				}
				for _, a := range launchApps {
					// Screenshot mode has no windowhost — status segments
					// stay non-clickable, the capinspector is
					// unreachable. The fields still render.
					r := decorateRenderer(adaptToRenderer(a), nil, status, nil)
					log.Info().Str("id", string(a.Manifest().Id)).
						Msg("screenshot mode: adding tour renderer")
					renderers = append(renderers, r)
				}
			} else {
				runId := ""
				if runInst != nil {
					runId = runInst.RunId
				}
				r, host := buildWindowedRenderer(launchApps, runId, facts, bus, fsSvc, clipSvc, status)
				windowHostRef = host
				renderers = append(renderers, r)
				log.Info().Int("initialWindows", len(launchApps)).Msg("window host: started")
			}

			// ADR-0108 §SD4: register the standard SQL pass set (e.g. LW_ID_*
			// macro expansion) plus play's host additions (statement
			// canonicalisation) into the process-wide pass registry — explicit
			// aggregation at the wiring site, so what this process rewrites is
			// reviewable here. Consumed by play's execute path and the
			// introspection /query endpoint; best-effort, never blocks boot.
			if passErr := passregdefaults.RegisterDefaults(); passErr != nil {
				log.Warn().Err(passErr).Msg("passreg: standard pass registration failed")
			}
			if passErr := play.RegisterPasses(passreg.Default); passErr != nil {
				log.Warn().Err(passErr).Msg("passreg: play host pass registration failed")
			}

			// ADR-0094 §SD3/§SD4: expose keelson runtime state as ClickHouse-
			// queryable tables over a loopback HTTP endpoint, and (when chlocal
			// is up) back POST /query so a co-resident app (apps/play) can query
			// keelson('env') in-process. This MUST run here, in the GUI host's
			// own process — the providers read live state (windowHostRef is nil
			// in screenshot mode, dropping only keelson.windows). Gated by
			// KEELSON_INTROSPECT_ENABLE; best-effort, never blocks boot.
			introspectStop, introspectErr := introspecthost.Start(introspecthost.Deps{
				WindowHost:       windowHostRef,
				Bus:              bus,
				ChlocalAvailable: chlocalSvc != nil,
				Log:              log.Logger,
			})
			if introspectErr != nil {
				log.Warn().Err(introspectErr).Msg("introspect: table source unavailable")
			}
			defer func() {
				ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 5*time.Second)
				defer cancel()
				_ = introspectStop(ctx)
			}()

			application_, err = application.NewApplication(cfg, u)
			if err != nil {
				return eh.Errorf("unable to create application: %w", err)
			}

			// Reap any windows still open at shutdown so the audit
			// trail is complete (no orphan "started" rows without
			// matching "stopped"). ReapAll is a no-op when
			// windowHostRef is nil (screenshot mode) or no windows
			// remain. Triggered by two paths to cover both clean exit
			// (Action returns) and signal-driven exit (boxer's
			// flightRecorder handler calls os.Exit, skipping defers in
			// this goroutine): a defer and a parallel signal listener,
			// guarded by sync.Once so neither path emits stop rows
			// twice.
			var reapOnce sync.Once
			doReap := func() {
				reapOnce.Do(func() {
					if windowHostRef != nil {
						windowHostRef.ReapAll("shutdown")
					}
					// Stop the heartbeat after the last app-lifecycle
					// stopped row lands so the gap between the final
					// heartbeat and the runtime-stop signal is small —
					// a crashed-vs-clean distinction relies on that.
					heartbeatInst.Stop()
				})
			}
			defer doReap()
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				doReap()
			}()

			return mainE(application_, renderers)
		},
	}
}

// buildStatusSnapshot summarises the active runtime services for the
// bottom-panel status line. Called once after all subsystems boot;
// the snapshot's fields are process-static so a single read suffices
// for the lifetime of the run. runInst may be nil (runinfo failed
// to init) — the short id falls back to "standalone".
func buildStatusSnapshot(runInst *runinfo.Inst, isChStore bool, bus *inprocbus.Inst, fsSvc *fsbroker.Service, persistSvc *persist.Service) (s *runtimestatus.Snapshot) {
	s = &runtimestatus.Snapshot{
		BusActive:      bus != nil,
		FsBrokerActive: fsSvc != nil,
	}
	if runInst != nil && len(runInst.RunId) >= 8 {
		s.RunIdShort = runInst.RunId[:8]
	} else {
		s.RunIdShort = "standalone"
	}
	if isChStore {
		s.FactsBackend = "ch"
	} else {
		s.FactsBackend = "mem"
	}
	if persistSvc != nil {
		// The carousel wires NewMemoryBackend today; a future commit
		// passes through a label so this stays in sync with whatever
		// backend type the service actually owns.
		s.PersistBackend = "mem"
	}
	return
}

func mainE(app *application.Application[*runtime.Unmarshaller], renderers []func() error) (err error) {
	app.FffiEstablishedHandler = func(fffi *runtime.Fffi2[*runtime.Unmarshaller]) error {
		typed.SetCurrentFffiVar(fffi)
		return nil
	}
	app.BeforeFirstFrameInitHandler = func() error {
		return widgets.BeforeFirstFrameInitHandler()
	}
	app.RenderLoopHandler = func() error {
		for _, r := range renderers {
			err = r()
			if err != nil {
				return err
			}
		}
		return nil
	}
	err = app.Launch()
	if err != nil {
		err = eh.Errorf("unable to launch application: %w", err)
		return
	}
	err = app.Run()
	if err != nil {
		err = eh.Errorf("unable to run application: %w", err)
		return
	}

	return
}
