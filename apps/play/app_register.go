package play

import (
	"embed"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
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

	AllowWrites = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_ALLOW_WRITES",
		Description: "non-empty lets Run execute an INSERT … SELECT wrapper (ADR-0181 §SD8); unset, Run refuses the write with a copy-out hint. Governs every play-engined host, sqlapplet included",
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

	WindowSize = env.NewString(env.Spec{
		Name:        "BOXER_PLAY_WINDOW_SIZE",
		Description: "open the play window at \"WxH\" logical points (scripted screenshots); empty or unparseable keeps the host's archetype default",
		Category:    env.CategoryE("boxer-play"),
	})
)

// NewLivePlayApp builds a PlayApp wired to a live ClickHouse Client — the same
// query-graph wiring PlayLauncher.Mount uses — and returns it ready for a
// re-user to customize before mounting (e.g. SetDetailContent to override the
// Detail body, or SetDocsSource to point the Docs pane at a different
// corpus). It is the supported constructor for embedding the playground
// behind a domain-specific AppI: the live query graph type is unexported, so an
// external module cannot call NewPlayApp directly. maxHistory bounds each lane's
// result-history ring (the shipped launcher uses 100). See
// doc/howto/play-pluggable-detail.md and doc/howto/play-pluggable-docs.md.
//
// It also installs the client's pre-execute SQL pipeline — the standard pass
// set (ADR-0108, e.g. LW_ID_* macro expansion) plus the schema-aware leeway
// name resolver (ADR-0116) — and feeds that resolver to the Diagnostics pane.
// That wiring is unexported (it sets the client's private pass registry), so an
// embedder cannot reproduce it; folding it in here is what lets every embedder
// pre-process SQL identically to the standalone CLI and the launcher instead of
// re-implementing launcher internals. A nil client — the result-less test
// shells that never run a query — skips the install.
//
// rules is the ADR-0186 rule repository — the standing gloss rules and the
// catalog — the embedder built at wiring time; nil takes DefaultRepository.
func NewLivePlayApp(client *Client, initialSQL string, maxHistory int, rules *gloss.Repository) *PlayApp {
	graph := newLiveQueryGraph(client, memory.NewGoAllocator(), maxHistory)
	app := NewPlayApp(client, graph, initialSQL, rules)
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
	// Rules is the gloss rule repository every window this launcher opens is
	// built over (ADR-0186); nil takes DefaultRepository. The factory
	// registered in init leaves it nil, so a deployment that links play
	// registers its rule sets on DefaultRepository.
	Rules *gloss.Repository
}

var _ app.AppI = (*PlayLauncher)(nil)

// AppId is play's registered manifest id — the target another app names
// in a `windowhost.open` request (ADR-0135 §SD7).
//
// The value lives in the leaf [launchcfg] package so a requester can name
// play without importing it (ADR-0017 §SD4); this re-export keeps the
// existing `play.AppId` spelling working.
const AppId app.AppIdT = launchcfg.AppId

// playSurfaceHints reads BOXER_PLAY_WINDOW_SIZE into the manifest's window
// hints. Unset (or unparseable, or non-positive) returns the zero value, which
// the host reads as "pick the archetype default" — so this is inert outside a
// scripted capture. Clamped to uint16 because that is the hint's own width.
func playSurfaceHints() (h app.SurfaceHints) {
	w, ht, ok := parseWxH(WindowSize.Get())
	if !ok || w <= 0 || ht <= 0 || w > 65535 || ht > 65535 {
		return
	}
	h.PreferredWidth = uint16(w)
	h.PreferredHeight = uint16(ht)
	return
}

func (inst *PlayLauncher) Manifest() (m app.Manifest) {
	m = app.Manifest{
		Id:       AppId,
		Version:  "0.1.0",
		Display:  "SQL playground",
		Title:    "SQL Playground",
		Icon:     icons.PhDatabase,
		Topics:   []app.TopicT{app.TopicSql, app.TopicData},
		Keywords: []string{"query", "queries", "playground", "editor", "clickhouse", "ide"},
		Surface:  app.SurfaceWindowed,
		// No SurfaceHints: the host's archetype fallback picks the size.
		// BOXER_PLAY_WINDOW_SIZE overrides it for scripted screenshots —
		// play's dock is three zones deep and the fallback opens narrow
		// enough that the tab strip truncates, which a capture would then
		// record as the app's shape. The knob is inert when unset, so an
		// ordinary launch is unaffected (ADR-0065 owns the archetypes).
		SurfaceHints: playSurfaceHints(),
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
				Pattern:   chlocalbroker.SubjectExecPrefix + timerangepicker.PoolName,
				Direction: app.CapDirectionPub,
				Reason:    "evaluate user time-range expressions (ADR-0016 Phase 4)",
			},
			{
				Pattern:   windowhost.OpenSubject,
				Direction: app.CapDirectionPub,
				Reason:    "open the applet creator window (ADR-0135 §SD7 / ADR-0132 O4)",
			},
			{
				Pattern:   adhocdata.SubjectPublish,
				Direction: app.CapDirectionPub,
				Reason:    "publish the generated timeseries fixture as ad-hoc datasets (ADR-0163 §SD7)",
			},
		},
		// PersistedKeys → host auto-injects the runtime.persist.play.>
		// cap. Kept for the read-only bridge only (ADR-0148 §SD8, added
		// 2026-07-29): the two buffers are now saved as a workingset
		// record, and Mount reads these keys solely so a session that
		// predates the change still finds its buffers. Retire the keys
		// and this entry one release on — the cap is what the read needs.
		PersistedKeys: []string{persistKeyLastSql, persistKeyTimelineBandsSql},
		// Launch config (ADR-0135 §SD7): windows opened over
		// `windowhost.open` may carry a launchcfg.PlayLaunch; Mount
		// decodes it and seeds the editor above the env/persisted chain.
		LaunchKind: launchcfg.Kind,
		// Workingset participation (ADR-0148 §SD7): the host pulls a
		// PlayLaunch out of a window the user acted in and hands it back
		// at the next plain open. play is the reference adopter (§SD8).
		Workingset: true,
	}
	return
}

func (inst *PlayLauncher) Mount(ctx app.MountContextI) (err error) {
	// Precedence for the initial SQL buffer, highest first:
	//   1. Caller launch config (ADR-0135 §SD7) — another app opened this
	//      window over `windowhost.open` with a playLaunch config;
	//      per-window intent beats process-wide defaults.
	//   2. BOXER_PLAY_SQL env var — explicit user override (one-shot
	//      screenshots, scripted runs).
	//   3. Restored workingset (ADR-0148 §SD5) — this app's own state from
	//      a window the user acted in, delivered as a config on an
	//      otherwise plain open. Ambient, so an env override outranks it.
	//   4. runtime.persist.play.lastSql — the one-release read bridge, for
	//      sessions that predate the workingset record.
	//   5. Default literal — first run, no prior state.
	//
	// Tiers 1 and 3 arrive through the same door and differ only by
	// LaunchReason, which is exactly what tier 2 needs to sit between them.
	var launch *launchcfg.PlayLaunch
	if raw := ctx.LaunchConfig(); len(raw) > 0 {
		lc, dErr := buscodec.Decode[launchcfg.PlayLaunch](raw)
		if dErr != nil {
			// The host validated the claimed kind at the boundary, so a
			// decode failure here is a real defect (codec drift, corrupt
			// bytes). Surface it as the failed-mount label — never fall
			// through to a silently different buffer (§SD4).
			err = eh.Errorf("play: decode launch config: %w", dErr)
			return
		}
		launch = &lc
	}
	restored := ctx.LaunchReason() == app.LaunchReasonRestore
	envSQL, envProvided := SQLOverride.Lookup()
	// Per the env var's description, only a NON-EMPTY override wins over the
	// lower tiers; set-but-empty behaves like unset.
	envOverride := envProvided && envSQL != ""
	var initSQL string
	configSQL := ""
	if launch != nil {
		configSQL = launch.Sql
	}
	switch {
	case configSQL != "" && !restored:
		initSQL = configSQL
	case envOverride:
		initSQL = envSQL
	case configSQL != "":
		initSQL = configSQL // restored tier
	}
	// seededSQL records whether a tier above the legacy bridge produced a
	// buffer, so the bridge below knows to stay out of the way.
	seededSQL := initSQL != ""
	if initSQL == "" {
		initSQL = "SELECT * FROM boxer.facts"
	}
	cfg := ClientConfig{
		URL:      clickhouseenv.URL.Get(),
		User:     clickhouseenv.User.Get(),
		Password: clickhouseenv.Password.Get(),
	}
	// Reconcile leeway's SQL read surface (ADR-0171 §SD2) against the env
	// endpoint, once per process and off the open path — before the
	// introspection retarget below, because the surface belongs to the real
	// server, not the in-process /query endpoint.
	installSQLSurface(cfg.URL, cfg.User, cfg.Password, ctx.Log())
	// The env target, remembered before the retarget below can overwrite it:
	// it is what the switcher's "External (reset)" restores, and it has to
	// keep meaning "the external server" even for a window that opened on
	// the introspection plane. Reading it back off the client after the
	// retarget is what made the reset a no-op precisely where it was needed.
	externalURL := cfg.URL
	// A launch config may retarget the endpoint (ADR-0135 §SD7): an
	// EndpointIntrospection open binds the client to the in-process keelson
	// `/query` endpoint so ad-hoc `keelson('<handle>')` datasets resolve
	// (ADR-0134). NewPlayApp seeds endpointDraft from cfg.URL, so the
	// toolbar switcher reflects the retarget with no further wiring. A
	// request with no such endpoint up degrades to the env default with a
	// warning — a degraded open, not a failed one, like the Tab tier.
	if launch != nil && launch.Endpoint == launchcfg.EndpointIntrospection {
		if ep := introspect.LocalQueryEndpoint(); ep != "" {
			cfg.URL = ep
		} else {
			logger := ctx.Log()
			logger.Warn().Msg("play: launch config requested the introspection endpoint, but none is registered; opening on the default target")
		}
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
	inner := NewLivePlayApp(client, initSQL, 100, inst.Rules)
	// Restore the pre-retarget meaning of "External": NewPlayApp read it off
	// the client, which by now may be pointed at the introspection plane.
	inner.externalURL = externalURL
	inner.AutoRun = AutoRun.Get() != ""
	inner.ScreenshotPath = ScreenshotPath.Get()
	inner.ExitOnShot = ExitOnShot.Get() != ""
	inner.previewAsSent = PreviewAsSent.Get() != ""
	inner.SetCapabilities(ctx.Bus(), ctx.Storage(), ctx.Log())
	if launch == nil {
		// Legacy read bridge (ADR-0148 §SD8), one release: only a window
		// that received no config at all consults the persist keys. A
		// restored record is the authority for its own window even where
		// it is empty — falling through to the keys there would resurrect
		// exactly what the record says the user cleared. Best-effort;
		// a silent miss leaves the default literal in place.
		if !seededSQL {
			inner.RestorePersistedSql()
		}
		// Bands SQL is restored regardless of the main env override — it's
		// panel-local, not main-SQL, so BOXER_PLAY_SQL has no bearing on
		// whether the user's last bands query should come back.
		inner.RestorePersistedTimelineBandsSql()
	}
	// Timeline bands, in ascending precedence: the bridge above, then the
	// restored record, then the env override, then a caller's config.
	if launch != nil && restored {
		// Applied unconditionally, unlike the caller tier below: an empty
		// bands buffer is a value the user arrived at, and "only when
		// non-empty" would silently bring back bands they cleared.
		inner.SetTimelineBandsSql(launch.BandsSql)
	}
	// Dedicated bands env override (parallel to BOXER_PLAY_SQL) lets
	// scripted screenshots seed the bands editor without interactive input.
	if bandsSQL, hasBands := TimelineBandsSQLOverride.Lookup(); hasBands && bandsSQL != "" {
		inner.timelineBandsSql = bandsSQL
	}
	if launch != nil {
		if !restored {
			// A caller-configured window states its complete opening
			// intent, so its flags replace their env counterparts
			// wholesale and its optional fields apply when non-empty.
			inner.AutoRun = launch.AutoRun
			if launch.BandsSql != "" {
				inner.SetTimelineBandsSql(launch.BandsSql)
			}
		}
		// A restored record composes AutoRun false by construction
		// (restoration is not re-execution), so it stays out of the way of
		// BOXER_PLAY_AUTORUN rather than overriding it with a
		// meaningless false.
		inner.SetLiveMain(launch.Live)
		// The tab tier takes the SQL tiers' precedence: a caller's config wins,
		// a RESTORED record loses to an explicit BOXER_PLAY_FOCUS_* knob. See
		// launchTabActivates for what the missing half cost.
		if launchTabActivates(launch.Tab, restored, focusedTabIDs()) {
			if tabErr := inner.ActivateTab(launch.Tab); tabErr != nil {
				// An unknown tab id is a degraded open, not a failed one
				// (§SD7): warn and keep the default tab.
				logger := ctx.Log()
				logger.Warn().Err(tabErr).Str("tab", launch.Tab).
					Msg("play: launch config tab not activated")
			}
		}
	}
	inst.inner = inner
	return
}

func (inst *PlayLauncher) Frame(ctx app.FrameContextI) (err error) {
	if inst.inner == nil {
		err = eh.Errorf("playlauncher: Frame called before Mount")
		return
	}
	// The Ctrl+Enter chord is drained once from egui's shared queue and
	// is visible to every open play instance alike; only the instance in
	// the shell's active window may act on it, or one press runs a query
	// in every open playground. Hosts that cannot say (single-surface,
	// tests) don't implement the capability, and the only instance there
	// is the active one.
	focused := true
	if f, ok := ctx.(app.WindowFocusI); ok {
		focused = f.WindowFocused()
	}
	inst.inner.windowUnfocused = !focused
	inst.inner.ensureColWidthRes(ctx)
	err = inst.inner.Render()
	return
}

func (inst *PlayLauncher) Unmount(ctx app.MountContextI) (err error) {
	// Nothing is saved here any more: the window host pulls the
	// workingset through ComposeWorkingset before it calls Unmount
	// (ADR-0148 §SD4), which is why that ordering is load-bearing —
	// the inner app is released below.
	if inst.inner != nil {
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
