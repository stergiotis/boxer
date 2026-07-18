package demo

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker/pickerbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmscrape"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/runtimestatus"
	imzhost "github.com/stergiotis/boxer/public/thestack/imzero2/host"

	// Side-effect imports — each app's init() registers itself into
	// app.DefaultRegistry. Carousel is the single import site that pulls all
	// M3-migrated apps; the dock host iterates the registry directly.
	_ "github.com/stergiotis/boxer/apps/capdemo"
	_ "github.com/stergiotis/boxer/apps/capinspector"
	_ "github.com/stergiotis/boxer/apps/fibscope"
	_ "github.com/stergiotis/boxer/apps/godepview"
	_ "github.com/stergiotis/boxer/apps/imzrt"
	_ "github.com/stergiotis/boxer/apps/imztop"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/splashscreen"
	_ "github.com/stergiotis/boxer/apps/taskdemo"
	_ "github.com/stergiotis/boxer/apps/terrainscope"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/configview"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/logviewer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/idsshowcase"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/sccmap"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
)

// decorateRenderer wraps an inner renderer in the shared host chrome. The
// implementation moved to package imzhost (public/thestack/imzero2/host);
// this thin wrapper preserves carousel's original unexported signature so the
// CLI's call sites keep working unchanged. Carousel always wires the full
// chrome (videooutput codec control + F1 help), so VideoOutput/HelpHost are
// hard-coded true.
func decorateRenderer(r func() error, extraMenus func(), status *runtimestatus.Snapshot, host *windowhost.Inst) func() error {
	return imzhost.DecorateRenderer(r, imzhost.ChromeConfig{
		ExtraMenus:  extraMenus,
		Status:      status,
		Host:        host,
		VideoOutput: true,
		HelpHost:    true,
	})
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
		// System-metrics plane (ADR-0090). imztop consumes it via MountCtx.Bus()
		// either way. With IMZERO2_SYSMETRICS_NATS_URL set (headless/sandboxed),
		// an external sysmetricsd reads /proc in its own sandbox and publishes to
		// NATS; we bridge that onto this in-proc host bus, so the carrier itself
		// never reads /proc. Otherwise (desktop/dev) we run the scraper
		// co-located. Process-lifetime; failures are logged and leave the metric
		// panels empty.
		metricPub := bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
		})
		if natsURL := sysmetricsbus.NatsURL.Get(); natsURL != "" {
			if natsClient, nerr := natsbus.Connect(natsbus.Options{URL: natsURL, AppId: sysmetricsbus.ServiceAppId}); nerr != nil {
				log.Warn().Err(nerr).Str("url", natsURL).Msg("carousel: sysmetrics NATS bridge connect failed; metric panels will be empty")
			} else if _, berr := sysmetricsbus.Bridge(natsClient, metricPub, sysmetricsbus.BundleSubjectWildcard()); berr != nil {
				log.Warn().Err(berr).Msg("carousel: sysmetrics NATS bridge subscribe failed")
				_ = natsClient.Close()
			} else {
				log.Info().Str("url", natsURL).Msg("carousel: bridging system metrics from NATS onto the host bus")
			}
		} else if _, serr := sysmscrape.StartScraper(context.Background(), metricPub, sysmetricsbus.DefaultHostToken(), time.Second, log.Logger); serr != nil {
			log.Warn().Err(serr).Msg("carousel: sysmetrics scraper unavailable; metric panels will be empty")
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
// clause body. A bare identifier (`play`, `regex_explorer`) is the common
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
	// Bare-alias fast path (ADR-0128 M3): a plain identifier resolves directly
	// against the registry by SubjectAlias — no clickhouse-local — so the mesh
	// appliance boot path needs no CH binary for the common `--launch <alias>`.
	// Aliases are registry-unique (Register enforces distinctness), so the match
	// is unambiguous; a miss returns empty, matching clickhouse-local's zero
	// rows. Anything with SQL shape (operators, IN, LIKE, a quoted value, a full
	// id, …) misses bareAliasRe and falls through to the CH path below. This
	// supersedes expandLaunchExpr's `subject_alias = '…'` spelling for bare
	// aliases; that expansion stays as the CH-side form and stays unit-tested.
	if trimmed := strings.TrimSpace(whereExpr); bareAliasRe.MatchString(trimmed) {
		for _, m := range app.AllManifests() {
			if m.Id.SubjectAlias() != trimmed {
				continue
			}
			a, lookupOk := app.Lookup(m.Id)
			if !lookupOk {
				err = eb.Build().Str("id", string(m.Id)).Str("alias", trimmed).
					Errorf("launch alias: registry inconsistency: manifest present but app not registered")
				return
			}
			apps = append(apps, a)
			return
		}
		return
	}
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

// adaptToRenderer wraps an AppI as a func() error. The implementation moved to
// package imzhost (public/thestack/imzero2/host); this thin wrapper preserves
// carousel's original unexported name so the CLI's call sites keep working.
func adaptToRenderer(a app.AppI) (r func() error) {
	return imzhost.AdaptToRenderer(a)
}

// windowDefaultSize returns the initial Window size for a windowed app. The
// implementation moved to package imzhost; this thin wrapper preserves
// carousel's original unexported name for buildWindowedRenderer's callers.
func windowDefaultSize(h app.SurfaceHints) (float32, float32) {
	return imzhost.WindowDefaultSize(h)
}
