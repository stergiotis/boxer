package demo

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/apps/capinspector"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker/pickerbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/helphost"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/metricsoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/runtimestatus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/videooutput"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
	// Side-effect imports — each app's init() registers itself into
	// app.DefaultRegistry. Carousel is the single import site that pulls all
	// M3-migrated apps; the dock host iterates the registry directly.
	_ "github.com/stergiotis/boxer/apps/capdemo"
	_ "github.com/stergiotis/boxer/apps/capinspector"
	_ "github.com/stergiotis/boxer/apps/godepview"
	_ "github.com/stergiotis/boxer/apps/imzrt"
	_ "github.com/stergiotis/boxer/apps/imztop"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/splashscreen"
	_ "github.com/stergiotis/boxer/apps/taskdemo"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/configview"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/logviewer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/hn_explorer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/idsshowcase"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/sccmap"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
)

// idleRepaintIntervalSecs is the steady-state repaint cadence requested by
// decorateRenderer in interactive mode. egui overrides it with sooner
// repaints for input and animation (it keeps the earliest deadline), so it
// only bounds how often a fully idle window refreshes. Matches the imztop
// sampler's 1 s tick; the Rust side mirrors it (src/imzero2/app.rs's
// IDLE_REPAINT_INTERVAL). Apps that change without interaction faster than
// this can request a sooner repaint themselves.
const idleRepaintIntervalSecs = 1.0

// decorateRenderer wraps an inner renderer in the shared host chrome:
// top PanelTop with the File / Layout menus + an optional extraMenus
// callback (used by the dock host to inject its "Apps" menu), bottom
// PanelBottom with a runtime status line + the metrics overlay.
// extraMenus may be nil — the screenshot tour path uses nil since
// there is no WindowHost in tour mode. status may be nil — the
// bottom row collapses to the metrics overlay alone. host may be nil
// — the status segments stay non-clickable when no windowhost is
// available to open the capinspector.
func decorateRenderer(r func() error, extraMenus func(), status *runtimestatus.Snapshot, host *windowhost.Inst) func() error {
	ids := c.NewWidgetIdStack()
	_ = ids
	// Captured once at setup so the per-frame closure pays no per-call
	// env-read cost. ADR-0032 §SD2 — IDS spacing tokens at the active
	// density preset.
	density := styletokens.DensityFromEnv()
	// Screenshot/tour mode (IMZERO2_SCREENSHOT_DIR set). Drives two things,
	// captured once so the per-frame closure doesn't repeat the env read:
	//   1. Skips the bottom status panel — the metrics overlay (Go ms / Rust
	//      ms / vsync / network / fps) and the run_id chip in runtimestatus
	//      change byte-for-byte every frame, producing 30+ "modified" file
	//      diffs every tour rerun without any visual content change.
	//   2. Keeps requesting immediate repaints (continuous mode) so every
	//      pass renders for capture, rather than the interactive idle
	//      heartbeat below.
	screenshotMode := imzero2env.ScreenshotDir.Get() != ""
	// Render cadence (IMZERO2_RENDER_CADENCE), captured once — the launch-time
	// default, and the fallback when no remote stream is live. Continuous
	// (default) paints at vsync rate; reactive drops to an idle heartbeat when
	// nothing is happening. The Rust client reads the same variable; both sides
	// must agree, since egui takes the soonest repaint deadline (an immediate
	// request on either side overrides the other's heartbeat). The headless
	// host's cadence is also runtime-switchable from the viewer, so the
	// per-frame closure prefers that live value (videoState.HostReactive) over
	// this flag when a stream is connected.
	reactive := imzero2env.RenderCadence.Get() == imzero2env.RenderCadenceReactive
	// ADR-0088: the remote-stream codec control. Persists the selected codec
	// across frames; renders only when a remote viewer has reported decode
	// capabilities (so it is invisible under the desktop host).
	videoState := &videooutput.State{}
	return func() error {
		// F1 global shortcut: open or focus HelpHost. The cached value
		// was drained from egui's input queue during StateManager.Sync
		// of the previous frame (consume_key already removed it), so
		// polling here is the one owner of this binding. Skipped in
		// tour mode (host == nil) since there's no windowhost to open
		// into.
		if host != nil && c.CurrentApplicationState.StateManager.GetF1KeyPressed() {
			if _, openErr := host.Open(helphost.ManifestId); openErr != nil {
				log.Warn().Err(openErr).Msg("F1: helphost open failed")
			}
		}
		// Drive the next frame. Continuous mode (the default) and screenshot
		// capture always request an immediate repaint. Reactive mode requests
		// a slow idle heartbeat instead; egui still repaints immediately for
		// input and animation (it keeps the earliest deadline), so interaction
		// stays at vsync rate while a visible-but-idle window drops to a few
		// fps. The Rust side mirrors this (src/imzero2/app.rs).
		//
		// The headless host's cadence is runtime-switchable from the browser
		// viewer (ADR-0088/0024): prefer the host's live cadence over the
		// launch-time flag so a viewer-driven switch takes effect on the Go side
		// too. videoState carries the value the host reported on the previous
		// frame (refreshed by videooutput.ShowStatus below — a one-frame lag).
		// Without this the Go side stays pinned to the launch cadence, and the
		// immediate repaint defeats a runtime switch to reactive since egui
		// takes the soonest repaint deadline. Falls back to the launch-time flag
		// when no stream is live (desktop host, or before a viewer connects).
		reactiveNow := reactive
		if live, ok := videoState.HostReactive(); ok {
			reactiveNow = live
		}
		if screenshotMode || !reactiveNow {
			c.RequestRepaint()
		} else {
			c.RequestRepaintAfter(idleRepaintIntervalSecs)
		}
		c.CurrentApplicationState.StartServersideFrame()
		defer c.CurrentApplicationState.FinishServersideFrame()
		for range c.PanelTop(ids.PrepareStr("topPanel")).KeepIter() {
			for range c.MenuBar().KeepIter() {
				for range c.MenuButton(c.Atoms().Text("File").Keep()).KeepIter() {
					if c.Button(ids.PrepareStr("quit"), c.Atoms().Text("Quit").Keep()).SendResp().HasPrimaryClicked() {
						c.ContextSendViewPortCommandClose()
					}
				}
				for range c.MenuButton(c.Atoms().Text("Layout").Keep()).KeepIter() {
					if c.Button(ids.PrepareStr("arrangeWindows"), c.Atoms().Text("Arrange Windows").Keep()).SendResp().HasPrimaryClicked() {
						c.MemoryResetAreas()
					}
					c.GuiZoomZoomMenuButtons()
				}
				if extraMenus != nil {
					extraMenus()
				}
				c.AddSpace(styletokens.GapSections(density))
			}
		}
		if !screenshotMode {
			for range c.PanelBottom(ids.PrepareStr("bottomPanel")).Resizable(false).KeepIter() {
				for range c.Horizontal().KeepIter() {
					c.AddSpace(styletokens.GapItems(density))
					if status != nil {
						var onClick runtimestatus.ClickHandler
						if host != nil {
							onClick = func(capId string) {
								// Push the selection first so newApp()
								// pops it during Open. FIFO queue —
								// rapid clicks open multiple inspector
								// windows, each tagged with the cap
								// that was clicked.
								capinspector.PushSelection(capinspector.CapId(capId))
								_, openErr := host.Open(capinspector.ManifestId)
								if openErr != nil {
									log.Warn().Err(openErr).Str("capId", capId).
										Msg("status-bar: capinspector open failed")
								}
							}
						}
						runtimestatus.RenderInline(status, onClick)
						// Visual separator before the metrics block; the
						// status segment is process-static info, metrics
						// is per-frame telemetry — the gap signals the
						// shift in cadence.
						c.AddSpace(styletokens.GapSections(density))
					}
					metricsoverlay.RenderInline(ids.PrepareStr("fps"))
					// ADR-0088: compact active-codec indicator; clicking it opens
					// the video-output settings dialog (ShowDialog, below).
					videooutput.ShowStatus(ids, videoState)
				}
			}
		}
		// ADR-0088: the video-output settings dialog floats over the app when
		// opened from the status-bar indicator (self-hides otherwise).
		videooutput.ShowDialog(ids, videoState)
		return r()
	}
}

// buildWindowedRenderer constructs the top-level RenderLoopHandler
// for interactive mode (no IMZERO2_SCREENSHOT_DIR). It builds a
// WindowHost over app.DefaultRegistry, attaches audit wiring (runId +
// facts) before any windows open so seeded windows produce "started"
// lifecycle rows, then seeds one window per pre-resolved AppI from
// --launch, and wraps the host Frame in decorateRenderer with the
// Apps menu installed.
//
// runId / facts may be empty/nil — in that case SetAudit is skipped
// and no lifecycle rows are written. bus may be nil — in that case
// SetBus is skipped and per-app MountCtx.Bus() returns NoopBus
// (every Subscribe/Publish/Request errors out). fsSvc may be nil —
// in that case the per-frame fs picker overlay is skipped (apps that
// publish `fs.dialog.*` get no responder and time out). Errors from
// initial Open calls are logged and dropped; a bad seed entry
// shouldn't abort startup. An empty seed is fine (user opens apps
// via the Apps menu).
//
// clipSvc may be nil — in that case clipboard.write copies are not
// drained into egui copy_text ops (apps publishing clipboard.write get
// no responder and their Request times out), but rendering is otherwise
// unaffected.
func buildWindowedRenderer(apps []app.AppI, runId string, facts factsstore.FactsStoreI, bus *inprocbus.Inst, fsSvc *fsbroker.Service, clipSvc *clipboardbroker.Service, status *runtimestatus.Snapshot) (r func() error, host *windowhost.Inst) {
	host = windowhost.NewInst(app.DefaultRegistry, log.Logger)
	if bus != nil {
		// Wire the bus before seeding so the seeded windows pick up a
		// real inprocbus.Client at Open. SetBus after Open has no
		// retroactive effect on already-mounted windows.
		host.SetBus(bus)
		// Run the co-located system-metrics scraper (ADR-0090): it reads /proc
		// and publishes the metric plane so imztop (and any consumer) gets data
		// over MountCtx.Bus() without holding a collector capability itself.
		// Process-lifetime; on a /proc-restricted host this fails and the metric
		// panels stay empty — the headless-sandboxed deployment instead needs an
		// external sysmetricsd over a NATS host bus (the remaining M4 step).
		scraperPub := bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
		})
		if _, serr := sysmetricsbus.StartScraper(context.Background(), scraperPub, sysmetricsbus.DefaultHostToken(), time.Second, log.Logger); serr != nil {
			log.Warn().Err(serr).Msg("carousel: sysmetrics scraper unavailable; imztop panels will be empty")
		}
	}
	if runId != "" && facts != nil {
		host.SetAudit(runId, facts)
	}
	for _, a := range apps {
		id := a.Manifest().Id
		_, openErr := host.Open(id)
		if openErr != nil {
			log.Warn().Err(openErr).Str("id", string(id)).
				Msg("windowhost seed: open failed, skipping")
			continue
		}
		log.Info().Str("id", string(id)).Msg("windowhost seed: opened window")
	}
	// Two separate WidgetIdStacks: one for the host body (reset at
	// the top of each Frame), one for the Apps menu (lives in the top
	// bar's MenuBar scope, rendered by extraMenus BEFORE the body's
	// reset). Sharing a single stack between extraMenus and inner
	// produced stale-derived ids on the Apps-menu buttons under the
	// previous dockhost design; the same hazard exists for windowhost.
	// Distinct stacks side-step the lifecycle entirely.
	bodyIds := c.NewWidgetIdStack()
	menuIds := c.NewWidgetIdStack()
	// bridgeIds carries the per-frame stack for the fs picker overlay.
	// Distinct from body/menu stacks so picker widgets never collide
	// with app or menu ids — the overlay floats on top of the host
	// body and reuses egui's modal/popup z-order.
	var bridgeIds *c.WidgetIdStack
	var fsBridge *pickerbridge.Bridge
	if fsSvc != nil {
		fsBridge = pickerbridge.NewBridge(fsSvc, log.Logger, pickerbridge.Config{})
		bridgeIds = c.NewWidgetIdStack()
	}
	inner := func() (err error) {
		bodyIds.Reset()
		err = host.Frame(bodyIds)
		if fsBridge != nil {
			bridgeIds.Reset()
			fsBridge.Render(bridgeIds)
		}
		// Clipboard bridge (ADR-0026 Update 2026-05-30): drain the copies
		// the clipboardbroker accumulated off the bus this frame and emit
		// one CopyTextToClipboard op per pending string. Runs after the
		// host body + picker overlay; the op rides the frame-scoped egui
		// Context, not a Ui scope, so no active panel is required and no
		// WidgetIdStack is needed (it is a procedural op, not a widget).
		if clipSvc != nil {
			for _, text := range clipSvc.DrainPending() {
				c.CopyTextToClipboard(text)
			}
		}
		return
	}
	r = decorateRenderer(inner, func() {
		menuIds.Reset()
		host.RenderAppsMenu(menuIds)
	}, status, host)
	return
}

// legacyCodeToId maps the ADR-0026 §SD12 M1-window numeric aliases
// (e.g. 5 → org.pebble2.play). The map is still consulted by
// codeForId in imzero2_demo_list.go to populate the `legacy_code`
// column of the registered-apps table; once that column is dropped,
// this map goes with it.
var legacyCodeToId = map[uint64]app.AppIdT{
	1: "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets",
	2: "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/hn_explorer",
	4: "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets",
	5: "github.com/stergiotis/boxer/apps/play",
	6: "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer",
	7: "github.com/stergiotis/boxer/apps/imztop",
}

// bareAliasRe matches Go-identifier-shape inputs. The registered
// subject_alias values are all snake_case identifiers (derived from
// the last `/`-segment of the AppId), so this is the natural shape
// for the shorthand. A future hyphenated alias would miss this and
// fall through to raw SQL — the clickhouse-local parse error points
// at the unquoted value, which is a usable diagnostic.
var bareAliasRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// expandLaunchExpr rewrites the --launch flag value into the WHERE
// clause body. A bare identifier (`play`, `hn_explorer`) is the common
// case and expands to `subject_alias = '<value>'`; anything else
// (already a SQL expression with operators, an IN list, a LIKE
// pattern, …) flows through verbatim. Whitespace-only or empty values
// stay empty so resolveLaunchSql's early-return covers them.
//
// The single-quote interpolation is safe because the regex restricts
// the value to [A-Za-z_][A-Za-z0-9_]* — no quote characters reach the
// SQL string.
func expandLaunchExpr(raw string) (whereExpr string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}
	if bareAliasRe.MatchString(trimmed) {
		whereExpr = "subject_alias = '" + trimmed + "'"
		return
	}
	whereExpr = trimmed
	return
}

// resolveLaunchSql parses the --launch flag value as a SQL WHERE clause
// over the registered-applications table (the same Arrow IPC stream
// `--list` materialises). It builds `SELECT id FROM table WHERE <expr>`,
// runs it through clickhouse-local, and resolves each returned id via the
// default app registry. An empty whereExpr returns an empty slice with
// no error so screenshot-mode's "--launch is required" guard fires
// uniformly through the len(apps) == 0 check at the call site.
//
// A bare identifier on the input is expanded by expandLaunchExpr into
// `subject_alias = '<value>'` so the common screenshot/scripting case
// stays a single shell word.
//
// clickhouse-local is required: a missing binary is a hard error rather
// than a silent fallback. SQL syntax errors surface verbatim through the
// runChLocalQuery diagnostic envelope (stderr + executed query in the
// returned error's structured fields).
func resolveLaunchSql(whereExpr string) (apps []app.AppI, err error) {
	expanded := expandLaunchExpr(whereExpr)
	if expanded == "" {
		return
	}
	arrowBytes, marshalErr := manifestsToArrowIPC(app.AllManifests())
	if marshalErr != nil {
		err = eh.Errorf("launch sql: serialise manifests: %w", marshalErr)
		return
	}
	query := "SELECT id FROM table WHERE " + expanded
	var stdout bytes.Buffer
	ok, runErr := runChLocalQuery(arrowBytes, query, "TabSeparated", &stdout)
	if runErr != nil {
		err = eh.Errorf("launch sql: %w", runErr)
		return
	}
	if !ok {
		err = eb.Build().Str("expectedPath", chlocalpool.DefaultBinaryPath).
			Errorf("launch sql: clickhouse-local is required to evaluate --launch but was not found at %s nor on $PATH", chlocalpool.DefaultBinaryPath)
		return
	}
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		id := app.AppIdT(line)
		a, lookupOk := app.Lookup(id)
		if !lookupOk {
			// CH selected from the live registry table, so a miss here
			// would be a registry inconsistency mid-flight. Surface
			// loudly rather than silently dropping the result.
			err = eb.Build().Str("id", line).Str("query", query).
				Errorf("launch sql: registry inconsistency: returned id not registered")
			return
		}
		apps = append(apps, a)
	}
	return
}

// adaptToRenderer wraps an AppI as a func() error usable by mainE's existing
// renderer slice. Mount runs lazily on the first call. Per ADR-0026
// Amendment 2026-05-12, for `SurfaceWindowed` apps the runtime — not the
// app — creates the window: `a.Frame` is invoked inside a `c.Window` scope
// built from `Manifest.WindowTitle()` and `SurfaceHints.PreferredWidth/Height`.
// `SurfaceHeadless` apps still call `Frame` raw. The M3 dock host will
// replace this adapter with a tile-bound child UI per app.
func adaptToRenderer(a app.AppI) (r func() error) {
	m := a.Manifest()
	mountCtx := app.NewStaticMountContext(m.Id, app.AppLogger(log.Logger, m.Id), nil, nil, nil)
	frameCtx := app.NewStaticFrameContext(mountCtx, nil)
	hostIds := c.NewWidgetIdStack()
	var mounted bool
	r = func() (err error) {
		hostIds.Reset()
		if !mounted {
			err = a.Mount(mountCtx)
			if err != nil {
				err = eh.Errorf("app mount: %w", err)
				return
			}
			mounted = true
		}
		if m.Surface != app.SurfaceWindowed {
			err = a.Frame(frameCtx)
			return
		}
		w, h := windowDefaultSize(m.SurfaceHints)
		windowId := "app:" + string(m.Id)
		for range c.Window(hostIds.PrepareStr(windowId),
			c.WidgetText().Text(m.WindowTitle()).Keep()).
			Resizable(true).
			TitleBar(true).
			DefaultOpen(true).
			DefaultSize(w, h).
			KeepIter() {
			err = a.Frame(frameCtx)
			if err != nil {
				return
			}
		}
		return
	}
	return
}

// windowDefaultSize returns the initial Window size for a windowed app.
// Honours SurfaceHints when set, falls back to a generic large-enough
// pair that fits most laptop screens without taking the whole viewport.
func windowDefaultSize(h app.SurfaceHints) (w, height float32) {
	w = float32(h.PreferredWidth)
	if w == 0 {
		w = 960
	}
	height = float32(h.PreferredHeight)
	if height == 0 {
		height = 720
	}
	return
}
