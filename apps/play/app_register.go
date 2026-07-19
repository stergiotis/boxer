package play

import (
	"embed"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
)

// helpFS embeds the play app's inline-help corpus (apps/play/help/*.md). The
// keelson/runtime/help DefaultLibrary indexes it per-app on first access and
// the HelpHost renders it. See helphost/help/howto/add-help.md.
//
//go:embed help
var helpFS embed.FS

// BOXER_PLAY_* drive optional one-shot/scripted-screenshot
// behaviours on the play HMI. Registered with the boxer-wide env
// registry per ADR-0009, so every knob shows up in the generated
// doc/env-vars.md catalog. The typed handles cache after the first
// read — fine here: the knobs are set before launch and never change.
var (
	SQLOverride = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_SQL",
		Description: "initial SQL buffer for the play HMI; non-empty wins over the persisted-session restore",
		Category:    env.CategoryE("boxer-play"),
	})

	TimelineBandsSQLOverride = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_TIMELINE_BANDS_SQL",
		Description: "panel-local bands SQL for the Timeline tab; non-empty wins over the persisted-session restore",
		Category:    env.CategoryE("boxer-play"),
	})

	AutoRun = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_AUTORUN",
		Description: "non-empty enables auto-run of the initial SQL on mount",
		Category:    env.CategoryE("boxer-play"),
	})

	ScreenshotPath = env.NewPath(env.Spec{
		Name:        "BOXER_PLAY_SCREENSHOT",
		Description: "if set, the play HMI captures a screenshot to this path after the first frame",
		Category:    env.CategoryE("boxer-play"),
	})

	ExitOnShot = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_EXIT_ON_SHOT",
		Description: "non-empty exits the play HMI after writing BOXER_PLAY_SCREENSHOT",
		Category:    env.CategoryE("boxer-play"),
	})

	PreviewAsSent = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_PREVIEW_AS_SENT",
		Description: "non-empty starts the Preview tab in 'as sent to server' mode (post-pass wire SQL) for scripted screenshots",
		Category:    env.CategoryE("boxer-play"),
	})

	// The BOXER_PLAY_FOCUS_* knobs are registered per built-in body tab in
	// play_tabs.go (registerFocusVars, slice 6a) — derived from the tab
	// definitions instead of hand-written here.

	ObserveNode = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_OBSERVE",
		Description: "graph node id to observe in the result panels after a Run (scripted screenshots); silently ignored when the node is absent from the split",
		Category:    env.CategoryE("boxer-play"),
	})

	ShotSettleFrames = env.NewInt(env.Spec{
		Name:        "BOXER_PLAY_SHOT_SETTLE",
		Description: "settle frames before BOXER_PLAY_SCREENSHOT fires; a positive value overrides the default (5), e.g. to wait out an async panel fetch",
		Category:    env.CategoryE("boxer-play"),
	})

	MapTable = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_MAP_TABLE",
		Description: "initial table for the Map panel; empty keeps the default (planes_mercator_sample100)",
		Category:    env.CategoryE("boxer-play"),
	})

	MapZoom = env.NewFloat(env.Spec{
		Name:        "BOXER_PLAY_MAP_ZOOM",
		Description: "initial Map zoom level; a positive value overrides the default (4)",
		Category:    env.CategoryE("boxer-play"),
	})

	MapCenter = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_MAP_CENTER",
		Description: "initial Map center as \"lat,lon\" (WGS84); empty or unparseable keeps the default (40,0)",
		Category:    env.CategoryE("boxer-play"),
	})

	MapSize = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_MAP_SIZE",
		Description: "pin a fixed Map widget size as \"WxH\" logical points (deterministic scripted screenshots); empty or unparseable keeps the default (the map fills the Map tab)",
		Category:    env.CategoryE("boxer-play"),
	})
)

// NewLivePlayApp builds a PlayApp wired to a live ClickHouse Client — the same
// query-graph wiring PlayLauncher.Mount uses — and returns it ready for a
// re-user to customize before mounting (e.g. SetDetailContent to override the
// Detail body). It is the supported constructor for embedding the playground
// behind a domain-specific AppI: the live query graph type is unexported, so an
// external module cannot call NewPlayApp directly. maxHistory bounds each lane's
// result-history ring (the shipped launcher uses 100). See
// doc/howto/play-pluggable-detail.md.
//
// It also installs the client's pre-execute SQL pipeline — the standard pass
// set (ADR-0108, e.g. LW_ID_* macro expansion) plus the schema-aware leeway
// name resolver (ADR-0116) — and feeds that resolver to the Diagnostics pane.
// That wiring is unexported (it sets the client's private pass registry), so an
// embedder cannot reproduce it; folding it in here is what lets every embedder
// pre-process SQL identically to the standalone CLI and the launcher instead of
// re-implementing launcher internals. A nil client — the result-less test
// shells that never run a query — skips the install.
func NewLivePlayApp(client *Client, initialSQL string, maxHistory int) *PlayApp {
	graph := newLiveQueryGraph(client, memory.NewGoAllocator(), maxHistory)
	app := NewPlayApp(client, graph, initialSQL)
	if client != nil {
		// installLeewayNameResolution points client.passes at a fresh registry
		// (standard set + resolver) and returns the resolver so the Diagnostics
		// pane can surface client-side pre-execution warnings.
		resolver := installLeewayNameResolution(client)
		app.SetColumnResolver(resolver)
	}
	return app
}

// PlayLauncher is the AppI wrapper for the SQL Playground. Late binding —
// ClickHouse connection details are read from environment variables at
// Mount, matching the legacy resolveApplication behaviour. A simple
// LegacyFuncApp wouldn't suffice because the env-var-driven configuration
// can't be captured cleanly at init time before the cli flag parser has run.
type PlayLauncher struct {
	inner *PlayApp
}

var _ app.AppI = (*PlayLauncher)(nil)

func (inst *PlayLauncher) Manifest() (m app.Manifest) {
	m = app.Manifest{
		Id:       "github.com/stergiotis/boxer/apps/play",
		Version:  "0.1.0",
		Display:  "SQL playground",
		Title:    "SQL Playground",
		Icon:     "🛢",
		Category: "Tools",
		Surface:  app.SurfaceWindowed,
		// Inline help corpus (apps/play/help/), indexed by
		// keelson/runtime/help and shown by the HelpHost.
		Help: help.MustSub(helpFS, "help"),
		// fs Powerbox — Load .sql button publishes fs.dialog.read,
		// then issues fs.handle.{uuid}.read once the broker mints
		// the handle. ADR-0026 §SD7.
		// ch.local.exec.timerangepicker — the from/to param-slot
		// widget calls evaluator.Eval to resolve ClickHouse SQL
		// time expressions to literal bounds; routes through the
		// chlocalbroker pool per ADR-0028. Absent this cap the
		// host falls back to the simpler DateTimePickerButton pair.
		Caps: []app.SubjectFilter{
			{
				Pattern:   fsbroker.SubjectDialogRead,
				Direction: app.CapDirectionPub,
				Reason:    "Load .sql via Powerbox picker",
			},
			{
				Pattern:   fsbroker.HandleSubjectPrefix + ">",
				Direction: app.CapDirectionPub,
				Reason:    "read file contents through granted handle",
			},
			{
				Pattern:   chlocalbroker.SubjectExecPrefix + timerangepicker.PoolName,
				Direction: app.CapDirectionPub,
				Reason:    "evaluate user time-range expressions (ADR-0016 Phase 4)",
			},
			{
				Pattern:   appletstore.SubjectSave,
				Direction: app.CapDirectionPub,
				Reason:    "save the buffer as a runtime applet (ADR-0132 O4)",
			},
		},
		// PersistedKeys → host auto-injects
		// runtime.persist.play.> cap; the editor buffer survives
		// session restart. BOXER_PLAY_SQL still wins when set.
		// timelineBandsSql persists the Timeline panel's bands-SQL
		// editor across sessions; empty is a valid value (no bands).
		PersistedKeys: []string{persistKeyLastSql, persistKeyTimelineBandsSql},
	}
	return
}

func (inst *PlayLauncher) Mount(ctx app.MountContextI) (err error) {
	// Precedence for the initial SQL buffer:
	//   1. BOXER_PLAY_SQL env var — explicit user override
	//      (one-shot screenshots, scripted runs).
	//   2. runtime.persist.play.lastSql — restored from the previous
	//      session via MountCtx.Storage.
	//   3. Default literal — first run, no prior state.
	// The persist restore happens after NewPlayApp because the
	// PlayApp's Storage handle is set via SetCapabilities below;
	// RestorePersistedSql replaces inst.sql in place.
	initSQL, envProvided := SQLOverride.Lookup()
	// Per the env var's description, only a NON-EMPTY override wins over the
	// persisted restore; set-but-empty behaves like unset.
	envOverride := envProvided && initSQL != ""
	if initSQL == "" {
		initSQL = "SELECT * FROM spinnaker.facts"
	}
	cfg := ClientConfig{
		URL:      clickhouseenv.URL.Get(),
		User:     clickhouseenv.User.Get(),
		Password: clickhouseenv.Password.Get(),
	}
	client := NewClient(cfg, nil)
	// SD7 identity for the log_comment stamp (ADR-0115): the runtime's
	// run id joins captured query runs to the runtime-start fact, the
	// Manifest Id is the app identity the facts vocabulary already keys
	// on. The standalone CLI path never sets these — its runs stamp lane
	// and fingerprints only.
	client.SetStampIdentity(ctx.RunId(), string(inst.Manifest().Id))
	// NewLivePlayApp installs the pre-execute SQL pipeline on the client
	// (standard passes + schema-aware leeway name resolver, ADR-0108/0116) and
	// wires the resolver into the Diagnostics pane. The carousel-embedded play
	// is its own host, so — like the standalone CLI — it relies on that shared
	// install rather than repeating it here.
	inner := NewLivePlayApp(client, initSQL, 100)
	inner.AutoRun = AutoRun.Get() != ""
	inner.ScreenshotPath = ScreenshotPath.Get()
	inner.ExitOnShot = ExitOnShot.Get() != ""
	inner.previewAsSent = PreviewAsSent.Get() != ""
	inner.SetCapabilities(ctx.Bus(), ctx.Storage(), ctx.Log())
	if !envOverride {
		// Storage restore is best-effort — silent miss leaves the
		// default literal in place.
		inner.RestorePersistedSql()
	}
	// Bands SQL is always restored regardless of the main env override —
	// it's panel-local, not main-SQL, so BOXER_PLAY_SQL has no
	// bearing on whether the user's last bands query should come back.
	inner.RestorePersistedTimelineBandsSql()
	// Dedicated bands env override (parallel to BOXER_PLAY_SQL) lets
	// scripted screenshots seed the bands editor without interactive input.
	if bandsSQL, hasBands := TimelineBandsSQLOverride.Lookup(); hasBands && bandsSQL != "" {
		inner.timelineBandsSql = bandsSQL
	}
	inst.inner = inner
	return
}

func (inst *PlayLauncher) Frame(ctx app.FrameContextI) (err error) {
	if inst.inner == nil {
		err = eh.Errorf("playlauncher: Frame called before Mount")
		return
	}
	err = inst.inner.Render()
	return
}

func (inst *PlayLauncher) Unmount(ctx app.MountContextI) (err error) {
	// Save-on-Unmount fallback: catches sessions that edited the
	// buffer without ever clicking Run. Idempotent — same value
	// already persisted on Run paths.
	if inst.inner != nil {
		inst.inner.PersistSql()
		inst.inner.PersistTimelineBandsSql()
		// Tear down the async machinery: cancel in-flight queries and the
		// projector, release held results, close every lane.
		inst.inner.Close()
	}
	inst.inner = nil
	return
}

func init() {
	// Factory registration — each Open() yields a fresh PlayLauncher
	// with its own inner *PlayApp (allocated in Mount). PlayApp owns
	// its own WidgetIdStack (inst.ids), so two open windows produce
	// disjoint Go-side widget IDs without the SeededFuncApp scope
	// wrapper that the legacy-renderer apps need. The manifest passed
	// to RegisterFactory must match what Manifest() returns.
	m := (&PlayLauncher{}).Manifest()
	err := app.DefaultRegistry.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		a = &PlayLauncher{}
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("play: failed to register factory")
	}
}
