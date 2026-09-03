package play

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/lazypane"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pager"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/evaluator"
)

// persistKeyLastSql is the runtime.persist key the playground uses to
// stash the editor buffer between sessions. Single NATS token (no
// dots) per the persist.Client contract; matches the manifest's
// PersistedKeys entry.
const (
	persistKeyLastSql          = "lastSql"
	persistKeyTimelineBandsSql = "timelineBandsSql"
)

const (
	defaultPageSize   = 100
	editorDesiredRows = 10
	// lazyPaneHoldFrames extends the widgets/lazypane loading placeholder by
	// this many extra frames after a hidden tab is re-shown. 0 = reveal on
	// the first frame after activation (a sub-frame loading tick); bump it if
	// a specific host makes the flash objectionable. Applies to every Lazy tab.
	lazyPaneHoldFrames = 0
	// Column-width heuristic bounds (px).
	colMinWidth = 100.0
	colMaxWidth = 420.0
	// colMinContentPx is the narrowest run of *content* a column keeps when
	// the user drags it in — a couple of monospace glyphs, enough to see that
	// something is there and to find the resize handle again. The width floor
	// the grids actually enforce is this plus the cell inset on both sides
	// (colDragMinWidth): counting only the leading inset, as the floor used
	// to, left the trailing gridline sitting in the glyphs of the header.
	colMinContentPx = 24.0
	// colDragMaxWidth is the widest a column may be dragged, and the ceiling
	// stored overrides are clamped to. One constant for both so the live drag
	// and the stored value cannot disagree — a column pinned at the drag
	// ceiling would otherwise be re-clamped on load and read as a change.
	colDragMaxWidth = 1200.0
	// colCharPx is the monospace glyph advance the width seed multiplies a
	// rune count by. 7.8 is Hack at 13 px (0.6 em), measured on a headless
	// tour capture: a 20-glyph cell inks 155 px, a 7-glyph one 53. It was
	// 7.0, which under-sized every column whose cells outrun its header. The
	// seed only lives until the re-fit frame (see renderMasterTable's refit
	// and selectableCell's truncate parameter, which is what actually fits a
	// column to its cells); a calibrated seed keeps that first frame close
	// to the final layout instead of visibly jumping.
	colCharPx = 7.8
	// colMaxRunes is colMaxWidth in glyphs at colCharPx, less the cell's
	// button padding and inset: what a re-fit cell may measure at most.
	colMaxRunes      = 50
	colSampleRows    = 64
	historyLabelChar = 46 // one-line label fit target
	// previewDebounce is the idle window the editor buffer must sit for before
	// the nanopass formatting pipeline runs. Parsing is ~1–10 ms so debouncing
	// keeps the UI from thrashing under continuous keystrokes.
	previewDebounce = 300 * time.Millisecond
)

// Stable tab identifiers for the dock area. Persistent egui_dock state is
// keyed off these — never renumber and never reuse a retired value.
const (
	dockTabEditor      uint64 = 1
	dockTabHistory     uint64 = 2
	dockTabTable       uint64 = 3
	dockTabProjection  uint64 = 4
	dockTabDetail      uint64 = 5
	dockTabPreview     uint64 = 6
	dockTabTimeline    uint64 = 7
	dockTabSnippets    uint64 = 8
	dockTabMap         uint64 = 9
	dockTabGraph       uint64 = 10
	dockTabSchema      uint64 = 11
	dockTabWorld       uint64 = 12
	dockTabDiagnostics uint64 = 13
	dockTabKanban      uint64 = 14
	dockTabPasses      uint64 = 15
	dockTabNetwork     uint64 = 16
	dockTabDocs        uint64 = 17
	dockTabFlow        uint64 = 18
	dockTabSankey      uint64 = 19
	dockTabExperiments uint64 = 20
	dockTabDist        uint64 = 21
	dockTabIcicle      uint64 = 22
	dockTabSeries      uint64 = 23
	dockTabTreemap     uint64 = 24
	dockTabChart       uint64 = 25
	dockTabVocabulary  uint64 = 26
	dockTabGlosses     uint64 = 27
	dockTabCompletion  uint64 = 28
	dockTabFiles       uint64 = 29
)

type PlayApp struct {
	ids    *c.WidgetIdStack
	graph  *queryGraph
	client *Client

	// currentSplit is the ADR-0097 node graph recovered from the last-run
	// buffer (3a/3c). The sink node is what the panels observe; it backs the
	// Graph-view chrome (3e) and the materialization policy (3d). splitErr is
	// the last Run's split failure (nil on success): the raw buffer was
	// executed instead, and the Graph tab shows the reason rather than
	// silently degrading to its empty-state.
	currentSplit splitResult
	splitErr     error

	// observedNode is the graph node whose result the result panels show (3d) —
	// the sink by default, switchable from the Graph view. When it is an
	// intermediate, its fused SQL runs on intermediateLane (a nodeLane:
	// demand-driven, non-blocking, generation-tagged supersession, last-good
	// retention — the same machinery as the map/bands lanes), and
	// activeSnapshot maps the lane view into the snapshot tuple.
	observedNode     NodeID
	intermediateLane *nodeLane

	// endpointDraft is the editable URL in the toolbar endpoint switcher;
	// externalURL is what "External (reset)" restores. See
	// renderEndpointSwitcher and Client.SetURL (ADR-0094 §SD6).
	//
	// externalURL is the *external server*, never the in-process
	// introspection plane. NewPlayApp seeds it from the client, which is
	// right for a plain open and for the CLI; a launch config that retargets
	// to introspection (ADR-0135 §SD7) opens the window already pointed at
	// the local plane, so the launcher overrides it with the pre-retarget
	// target. Seeding it from the live client there would redefine
	// "External" as "wherever this window opened" — leaving the one state
	// the button exists to leave with no way out.
	endpointDraft string
	externalURL   string
	// autoEndpoint installs the keelson-aware resolver (ADR-0141): a read
	// that names only keelson tables goes to the in-process introspection
	// plane instead of wherever the switcher points. Off leaves the static
	// resolver, and any manual pick turns it off — a pin always wins.
	//
	// Default on (NewPlayApp). Nothing moves unless a buffer names a keelson
	// table, and a buffer that does had exactly one endpoint that could
	// serve it anyway.
	autoEndpoint bool

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at NewPlayApp.
	density styletokens.DensityE

	sql         string
	lastSentSql string

	// docs is the Docs pane's lookup driver — cache, debounce, and the
	// installed DocsSourceI (ClickHouse's system.documentation by default;
	// SetDocsSource overrides it) — and docsPane its view state. A tool
	// pane, not a result panel: its input is the editor's published caret
	// entity, never the query result.
	docs     *docsDriver
	docsPane *docsPaneState

	// Snippets-tab filter state (ADR-0164 §SD4). snippetsFilter backs the
	// box; snippetsQuery is the trimmed query snippetsAccepted (matching
	// section slugs, descendants expanded) was computed for — recompute on
	// change, not per frame. snippetsLiteral flags a token that degraded
	// to a literal match so the tab can say so; snippetsCoverage is how
	// much of the snippets doc the accepted set selects (the filter's
	// selectivity meter); snippetsHl is the filter box's regexedit
	// highlight-job cache. Zero values = unfiltered.
	snippetsFilter   string
	snippetsQuery    string
	snippetsAccepted map[string]bool
	snippetsLiteral  bool
	snippetsAltHint  string
	snippetsCoverage search.Coverage
	snippetsHl       regexedit.Edit

	// editor is the SQL editing surface (ADR-0147). It owns what follows
	// from the buffer and the caret alone — the colour tiers, the statement
	// split and its memo, the gutter, the run-under-cursor composition — and
	// publishes them through its Result. The zero value is ready.
	editor sqleditor.Editor

	// Slice-5a signal-store state. frameSig is the per-frame immutable
	// snapshot of the graph's signal store, taken at Render top so every
	// consumer in a frame sees a single revision (glitch-freedom as frame
	// semantics); an emit lands next frame. lastSentSigParams /
	// lastRunBound record what the last Run resolved (URL-keyed) and which
	// names its prelude bound — the signal half of the staleness condition
	// and the observed intermediates' resolution inputs. wireSignals
	// mirrors the would-be resolution for the "as sent" preview caption.
	frameSig SignalEnvI
	// sigEmit is the live SignalEmitterI panels publish through (slice 5b):
	// writes land in the store and are visible from the next frame's
	// snapshot. The selection is a store signal now — PlayApp no longer
	// carries a selectedRow field.
	sigEmit           graphEmitter
	lastSentSigParams map[string]string
	lastRunBound      map[string]bool
	wireSignals       map[string]string
	wireSigRev        uint64
	// Slice-5e state. liveMain is the `main` node's opt-in liveness bit
	// (D2): with it on, a referenced-signal move re-runs the unchanged
	// buffer (buffer edits stay Run-gated). runIsAuto marks the pending
	// requestRun as toggle-fired so executeRun skips the persist (the
	// persistence point stays user-intent-anchored). runBlockedReason
	// carries the unfilled-input refusal (D3) for the status bar. The
	// sig* fields back the Signals section: reseed-guarded value drafts,
	// the add-signal footer's drafts, and the debounce-following
	// slot-type table (conflict detection).
	liveMain         bool
	runIsAuto        bool
	runBlockedReason string
	// The write gate (ADR-0181 §SD8 M3): writeGateNotice carries Run's
	// refusal of an ungated INSERT wrapper for the status bar — set and
	// cleared only in executeRun, so it renders unconditionally, unlike
	// runBlockedReason whose validity is re-derived from the unfilled set.
	// writeRun is the async write's delivery seam.
	writeGateNotice string
	writeRun        writeRunState
	// The Live circuit breaker (the 2026-07-22 review remediation):
	// autoRunStreak counts consecutive machine-driven auto-runs against
	// autoRunStreakSql (the buffer they ran), and liveSuspendReason carries
	// the status-bar line naming what was cycling once the breaker unchecks
	// Live. A human Run clears both.
	autoRunStreak     int
	autoRunStreakSql  string
	liveSuspendReason string
	// exposeConditions is the top-bar toggle for the opt-in selection-condition
	// rewrite (ADR-0121), default off. The Client owns the authoritative
	// (atomic) flag the query path reads; this is the render thread's copy,
	// pushed to the client whenever the checkbox changes it.
	exposeConditions bool
	sigValDrafts     map[string]*string
	sigValSeeded     map[string]string
	sigAddName       string
	sigAddValue      string
	sigTypesFor      string
	sigTypes         map[string][]string
	// pendingSnippetInsert / pendingSnippetReplace hold a snippet-library
	// click until the editor consumes it on the next frame: Insert splices
	// the snippet at the caret via TextEditFluid.InsertAtCursor (ADR-0063);
	// Replace swaps the whole buffer (a plain Go assignment — no FFI). Set by
	// renderSnippetsTab, captured-and-cleared by renderSqlEditor.
	pendingSnippetInsert  string
	pendingSnippetReplace string
	// runsHist backs the History tab's "Recorded runs" section (ADR-0115
	// S2): captured KindQueryRun facts read back from the live endpoint,
	// fetched manually and on first reveal (play_runs_history.go).
	runsHist *runsHistoryDriver
	// pins / pinsBrowser are Tier-1 result pinning (ADR-0115 S4): the
	// Table tab's pin affordance and the History tab's pin browser
	// (play_pin.go).
	pins        *pinDriver
	pinsBrowser *pinsBrowserDriver
	// tabs is the instance's dock-tab set (ADR-0097 slice 6a): every tab a
	// registered TabSpec, frozen at the first Render. Embedders customize
	// it via Tabs() between construction and mounting (D4).
	tabs *TabRegistry
	// lazyPanes holds one widgets/lazypane gate per Lazy tab, keyed by
	// DockID and created on first use (embedder tabs land here too). The
	// panes are persistent render-thread state — each carries the
	// hidden/warming/live phase machine across frames.
	lazyPanes map[uint64]*lazypane.Pane
	// Slice-6c per-panel binding state. tabBindings maps a panel tab to the
	// split node it renders (unbound tabs render the active result);
	// boundLanes holds one lane per distinct bound node; boundViews and
	// resolvedNodes are the per-frame demand results (see demandBoundNodes).
	tabBindings   map[string]NodeID
	boundLanes    map[NodeID]*nodeLane
	boundViews    map[NodeID]laneView
	resolvedNodes map[string]NodeID
	// System-graph drawing state (play_graph_viz.go): the layout is cached
	// on the topology fingerprint (vizKey); vizView carries pan/zoom;
	// vizSeed keeps two live instances' canvas ids apart.
	vizLayout *layeredgraph.Layout
	vizKey    string
	vizErr    error
	vizView   view.ViewState
	vizSeed   uint64
	// Passes-tab drawing state (play_passes_tab.go, ADR-0119 M3): the
	// pipelineview layout cached on the pass-catalog fingerprint.
	passesTab passesTabState
	// Vocabulary-tab state (play_vocab_panel.go, ADR-0174): the filter and
	// its accepted set; vocabHl is the filter box's regexedit state, held
	// per-instance like snippetsHl for the same reason (the widget owns a
	// compiled pattern across frames).
	vocabTab vocabTabState
	// completion is the ADR-0190 pane's state and this frame's answer. It is
	// refreshed from the editor's Bind whether or not the tab is open, because
	// the editor's own tint reads the same result.
	completion completionState
	vocabHl    regexedit.Edit
	// Per-buffer outcomes of the client-side rewrite (play_passes_tab.go),
	// shared by the Passes and Diagnostics tabs and computed on first demand
	// per frame — both tabs are lazy, so a session with neither open pays
	// nothing.
	rewriteTrace rewriteTraceState
	// pendingDockActivate focuses a dock tab on the next dock send (0 =
	// none): set by affordances that deliver content into a tab body (the
	// snippet library targeting the editor), consumed once per frame in the
	// dock scope. A hidden tab's body buffer is discarded uninterpreted, so
	// delivery ops must ride an activated tab.
	pendingDockActivate uint64
	// raisedTab is the last dock tab play itself raised (0 = none), kept
	// after pendingDockActivate is consumed so ComposeLaunch can report an
	// active tab (ADR-0148 §SD8). Not the dock's own notion of focus — that
	// lives on the Rust side; see raiseDockTab.
	raisedTab  uint64
	requestRun bool
	// requestSubquery narrows the pending requestRun to the innermost query
	// the caret is in (the Ctrl+Shift+Enter gesture). Consumed with the run
	// request it qualifies.
	requestSubquery bool
	// subqueryMode is the top-bar display toggle for that gesture: while on,
	// the editor tints the query it would run and the environment travelling
	// with it. Off by default — the decoration is only wanted while working on
	// a nested query, and it is the gesture that does the work, not the mode.
	subqueryMode bool
	// windowUnfocused says the shell's active window is some OTHER window
	// this frame (app.WindowFocusI, stamped by PlayLauncher.Frame). It
	// gates the Ctrl+Enter poll alone: the chord is process-global input
	// every open play instance sees, and only the instance the user is in
	// may act on it. Inverted so the zero value — tests building PlayApp
	// directly, hosts without the capability — stays focused. Buttons need
	// no gate: a click already names its window.
	windowUnfocused bool
	// lastRunScope records what the previous run actually shipped, for the
	// status line. A gesture that silently degraded to the whole query is the
	// one thing the tints cannot explain — they are not drawn in that case.
	lastRunScope   runScopeE
	cards          *CardDriver
	detailTimeline *DetailTimeline
	// components is the Detail pane's typed per-component report over play's
	// registered component kinds (play_detail_components.go), drawn above
	// the leeway card for a facts-shaped row.
	components *componentDetail
	// identity is the Detail pane's canonical-identity strip (ADR-0219 SD5,
	// play_detail_identity.go); identityJob the Table pane's background
	// digest job over the whole result (SD7, play_table_identity.go).
	identity    *identityDetail
	identityJob *identityJob
	// tableResult / detailResult are the ResultIDs of the results the Table
	// and Detail panels are rendering this frame, set by the panels before
	// their bodies run (a bound tab renders its own node's result).
	tableResult  ResultID
	detailResult ResultID
	projector    *Projector
	// experiments backs the Experiments tool pane: a leeway sink playground
	// over the fixture or the current result.
	experiments *experimentsDriver

	// tableOpts holds the Table pane's leeway display-mode configuration — the
	// options bar's three orthogonal controls (row granularity, reveal support
	// columns, reveal membership columns; see play_table_leeway.go). The zero
	// value is the default view (one row per DB row, support + membership hidden);
	// the bar only appears when the result is leeway-shaped.
	tableOpts tableDisplayOpts

	// tableSort is the Table pane's header-click sort: a permutation over the
	// record already in hand, never a re-issued query (play_table_sort.go).
	tableSort tableSortState

	// schemaModel backs the Schema dock tab: the schemaview inspector bound to
	// a leeway TableDesc inferred from the active result's Arrow schema (plain
	// opaque columns — tagged sections/memberships aren't recoverable from an
	// ad-hoc result; see play_schema_infer.go). schemaForSchema is the pointer-
	// identity cache that gates the rebuild, mirroring colWidthsForSchema and
	// the projector's forSchema.
	schemaModel     *schemaview.Model
	schemaForSchema *arrow.Schema

	// detailContent, when non-nil, replaces the Detail panel's built-in body
	// (RenderDefaultDetailContent). A library re-using PlayApp installs one via
	// SetDetailContent to render a domain-specific card for the selected row.
	detailContent DetailContentFunc

	// projFSM mirrors projector lifecycle into a fsmview.Machine so the
	// renderer can show a chip + drill-down popup (table / graph /
	// history). statetrooper FSM is render-thread-only; renderProjection
	// reads the projector's snapshot status and forwards into
	// projFSM.Transition each frame. Rule declarations enumerate every
	// observed status transition so the popup graph view paints the full
	// lifecycle.
	projFSM       *fsmview.Machine[projectorStatusE]
	projFSMWidget *fsmview.Widget[projectorStatusE]
	// queryFSM tracks the result↔input lifecycle (play_querystate.go) so the
	// status bar names the state and flags stale/empty output; queryFSMWidget
	// surfaces the graph + transition history + provenance as a status-bar chip.
	queryFSM       *fsmview.Machine[queryStateE]
	queryFSMWidget *fsmview.Widget[queryStateE]
	// progress folds the observed lane's live ticks into a smoothed rate and
	// a damped ETA (play_progress.go); frameProgress is this frame's answer,
	// computed once in Render and read by every display site — the top bar,
	// the pane strips, the loading empty-state and the status line.
	progress               progressTracker
	frameProgress          progressView
	timeline               *TimelineDriver
	timelineBandsSql       string
	timelineNowLineEnabled bool

	// mapDriver is the ADR-0096 geo-raster map panel (Map dock tab): a slippy
	// map whose viewport drives an in-DB-rendered raster on a panel-local lane.
	mapDriver *MapDriver

	// worldDriver is the ADR-0114 schematic world-choropleth panel (World dock
	// tab): a plain observer of the active result — no lane, nothing to Close.
	worldDriver *WorldDriver

	// kanbanDriver is the ADR-0122 board panel (Kanban dock tab): likewise a
	// plain observer of the active result — no lane, nothing to Close.
	kanbanDriver *KanbanDriver

	// networkDriver is the ADR-0129 layered-graph panel (Network dock tab): a
	// node-link view whose vertices and edges come from two named CTEs of the
	// user's query, each on its own lane (closed in Close).
	networkDriver *NetworkDriver

	// sankeyDriver is the ADR-0159 flow-diagram panel (Sankey dock tab): the
	// same two-private-lane shape as the Network's, over the `flows` and
	// `nodes` CTEs (closed in Close, forgotten on Run).
	sankeyDriver *SankeyDriver

	// distDriver is the ADR-0161 distribution panel (Distribution dock tab):
	// a plain observer of the active result claiming the series/n/ps/qs
	// contract — no lane, nothing to Close.
	distDriver *DistDriver

	// icicleDriver is the ADR-0160 icicle/flamegraph panel (Icicle dock tab):
	// likewise a plain observer of the active result, claiming either the
	// folded `stack`+`value` contract or the `id`/`parent`/`value` one — no
	// lane, nothing to Close.
	icicleDriver *IcicleDriver

	// treemapDriver is the ADR-0166 treemap panel (Treemap dock tab): the same
	// observer shape and the same hierarchy contract as the Icicle tab, read as
	// nested areas rather than depth rows — no lane, nothing to Close.
	treemapDriver *treemapDriver

	// filesDriver is the Files panel (ADR-0200): the active result interned
	// into a read-only file system and browsed with widgets/fsbrowser — the
	// widget apps/tally hosts over a lading snapshot, second host.
	filesDriver *filesDriver

	// chartDriver is the ADR-0172 Chart panel (Chart dock tab): the plain
	// chart — a category against a count, a number against a number, a
	// two-key grid against a third value. Another observer of the active
	// result, claiming `x` plus numeric lanes or `x`/`y`/`z` — no lane,
	// nothing to Close.
	chartDriver *ChartDriver

	// seriesDriver is the ADR-0163 Series panel (Series dock tab): an observer
	// of the active result under the typed (time, numbers) claim. Unlike the
	// observers above it also reads two OPTIONAL CTEs by name — `scores` and
	// `spans` (§SD1) — so it carries two lanes, closed in Close and forgotten
	// on Run like the Sankey's.
	seriesDriver     *SeriesDriver
	seriesScoresLane *nodeLane
	seriesSpansLane  *nodeLane
	// seriesLabelsLane reads the adjudicated verdicts for the charted input
	// (ADR-0163 §SD6); seriesLabels writes them.
	seriesLabelsLane *nodeLane
	seriesLabels     *tsLabelsWriter
	// seriesLabelsSeen is the write counter the read lane was last refreshed
	// at, so exactly one memo-forget follows each completed write.
	seriesLabelsSeen uint64
	// fixtures is the ADR-0163 M4 fixture lab: generate a labelled synthetic
	// series and publish it as ordinary ad-hoc datasets. Needs the bus, so it
	// is inert in a session without capabilities.
	fixtures     *fixtureState
	fixtureSpec  fixtureSpec
	fixturesSeen uint64

	// tsCollisions caches whether the server has functions whose names play's
	// own `ts*` vocabulary shadows (ADR-0163 §SD4). Chrome only — the answer
	// never changes what runs, just what the panes say about it.
	tsCollisions *tsCollisionProbe

	// vocab caches the endpoint's user-defined function set for the
	// Vocabulary tab (ADR-0174 §SD2). Chrome only, like tsCollisions: it
	// reports what is installed and never changes what runs.
	vocab *vocabProbe

	// flow is the ADR-0153 Flow dock tab: the active node's clause-level
	// dataflow, derived statically from the split; the EXPLAIN lenses add
	// three lanes (closed in Close, forgotten on Run like the Network's).
	flow *flowDriver

	// richCells memoises the ADR-0123 content-typed cells of the Detail pane's
	// selected row (a parsed markdown doc, a highlighted job, decoded pixels).
	richCells *richCellCache

	// rules is the ADR-0186 rule repository the host handed the constructor:
	// the catalog every `<label>@<media type>` declaration resolves against
	// and the standing rules (sets declared in code, then the affinities) a
	// column is offered after the buffer's directives. Read through
	// glossRules(), which defaults it for a bare test app.
	rules *gloss.Repository
	// glossRes caches the per-column gloss resolution of the schema last seen
	// by the Table grids (play_gloss.go).
	glossRes glossResolution

	// diag owns the Diagnostics dock tab's EXPLAIN AST probe (its own lane
	// against the live endpoint); the pane itself is the single owner of the
	// playground's error prose — result tabs only point here.
	diag *DiagnosticsDriver

	// colorByFeature picks the EntityFeatures field whose value drives the
	// projection scatter's per-point colour. -1 means monochrome (default);
	// 0..card.NumFeatures-1 indexes card.FeatureNames(). Persisted across
	// recomputes so the user's chosen colouring sticks.
	colorByFeature int8

	// Auto-run + screenshot (driven by env vars for one-shot captures).
	AutoRun        bool
	ScreenshotPath string
	ExitOnShot     bool
	frame          int
	didAutoRun     bool
	shotPhase      int // 0=idle, 1=settle, 2=requested, 3=done
	shotSettle     int

	// Debounced canonical-form preview.
	lastSeenSql  string
	lastEditAt   time.Time
	formatted    string
	formattedFor string
	formattedErr error

	// Workingset intent tracking (ADR-0148 §SD4/§SD8). workingsetSeen is
	// last frame's baseline — the launch ComposeLaunch would compose,
	// normalized by workingsetBaseline — so a field added to PlayLaunch and
	// ComposeLaunch participates in dirty detection automatically, with no
	// parallel field list to keep in step. The baseline is taken on the
	// first frame — after Mount has finished seeding — so the seeded state
	// itself never reads as an edit. That is what closes the
	// poisoned-inheritance case: a window opened on a launch config and
	// closed untouched stores nothing.
	//
	// The comparison is per-frame rather than response-driven because the
	// toolkit forces it: the SQL editor and the Live checkbox write through
	// SendRespVal, whose change-detection callback never fires, so there is
	// no edit event to hang this on.
	workingsetSeen      launchcfg.PlayLaunch
	workingsetSeenTaken bool
	workingsetDirty     bool

	// caretByte is the caret's byte offset into inst.sql, taken from the
	// editor's published Result once per frame at the top of the editor
	// render so every consumer sees the same value. The raw packed CHAR
	// range and its one-frame lag are the widget's business now (ADR-0147);
	// what play keeps is the resolved offset, because its own producers —
	// the param pane, the Run gate — run before the editor's turn in the
	// frame and need a caret to read.
	caretByte int
	// One-entry memo for the per-statement syntax check the error underline
	// runs on multi-statement buffers (see statementSyntaxError).
	stmtErrFor string
	stmtErrPos syntaxErrorPos
	stmtErrOk  bool
	// One-entry memo for the CST-tier subquery split of the caret's statement
	// (see statementSubqueries) — what run-subquery narrows to, and what the
	// editor tints while the caret is inside a nested query.
	subqFor   string
	subqUnits []subqueryUnit
	subqOk    bool

	// "As sent" preview toggle (ADR-0108): when on, the Preview tab shows
	// the statement Client.BuildStatement would ship — params harvested,
	// pre-execute passes applied, FORMAT rewritten — instead of the
	// canonical form. wireFor keys the debounced cache like formattedFor;
	// wireParams holds the harvested URL params for the caption line.
	previewAsSent bool
	wireFor       string
	wireBody      string
	wireParams    map[string]string
	// wireConditions keys the cache on the condition-columns toggle too (ADR-0121): it
	// changes what ships without touching the buffer, and a view whose whole
	// job is to show what ships must not go stale behind it.
	wireConditions bool
	// wireStmtNumber / wireStmtTotal are the caret's statement and the body's
	// statement count for the cached wire body (ADR-0130 L3). They key the
	// cache on caret travel — which changes what ships without an edit — and
	// caption the view with "statement N of M".
	wireStmtNumber int
	wireStmtTotal  int

	// Results pagination. pagerSeenExecuted tracks the QueryStore's
	// "executed" timestamp — when it advances, the pager snaps back to
	// page 0 because the dataset changed.
	pager             *pager.Pager
	pagerSeenExecuted time.Time

	// Column-width cache, keyed by Arrow *Schema pointer. Widths are sampled
	// once on schema change; recomputing per-frame would make the table reflow
	// every time the pager advances because different pages have different
	// string lengths.
	colWidthsForSchema *arrow.Schema
	colWidths          []float32

	// The column set the results grid last emitted. egui_table fits a
	// column to its content only on a table's first show, so play asks for
	// a re-fit whenever this changes; see tableColsChanged. Distinct from
	// colWidthsForSchema above, which caches the width *estimate* — this
	// one decides when the crate is told to measure for itself.
	tableFitSchema *arrow.Schema
	tableFitCols   []int

	// Friendly leeway column labels for the current result: physical column
	// name → display label (section / section:column, via lwsql.BuildLabels).
	// Rebuilt on schema change like colWidths; nil when the result is not
	// leeway-shaped, in which case raw physical names are shown. The SQL sent
	// to the server always keeps physical names — this is presentation only.
	colLabelsForSchema *arrow.Schema
	colLabels          map[string]string
	// colWidthRes resolves and captures persisted table column widths
	// (ADR-0151 M4). Distinct from colWidths above, which is the per-schema
	// estimator for the row-oriented results grid: this one is the durable
	// override layer, and the estimator feeds it as the default.
	//
	// Nil when the host exposes no column-width store — every width
	// affordance still works, nothing persists. Acquired once on the first
	// Frame, since the capability rides the frame context.
	colWidthRes     *colwidth.Resolver
	colWidthResInit bool
	// attrWidthsSeen gates the first width report for the attr grid. The
	// crate force-autofits on its first show, and that result is the
	// estimator's rather than the user's — capturing it would freeze a
	// width nobody chose. The first report a table makes is that frame.
	attrWidthsSeen bool
	// masterWidthsSeen is the same gate for the per-DB-row grid. It is a
	// separate flag because the two grids are separate etables with separate
	// first shows — one is not evidence about the other.
	masterWidthsSeen bool
	// attrFitSchema / attrFitCols are the per-attribute grid's memory of the
	// column set it last re-fitted for (attrColsChanged), the sibling of
	// tableFitSchema / tableFitCols.
	attrFitSchema *arrow.Schema
	attrFitCols   []int

	// attrSink is the per-attribute Table view's exploder (play_table_attr.go),
	// pooled across frames: the per-attribute grid is re-driven every frame it is
	// shown, so keeping one sink and resetting its backing arrays with [:0] keeps
	// the render path allocation-free in steady state instead of building a fresh
	// sink (and thousands of throwaway row maps) each frame.
	attrSink attrExplodeSink

	// Analytical FunctionEvaluator that runs alongside the canonicalisers in
	// updatePreview. Its handlers return ControlFlow{PassDiscardOutput} so
	// the runner forwards the input unchanged; the side channel is the
	// OnObservation callback fired per visited registered call. Built once
	// in NewPlayApp and reused across debounce ticks.
	affordanceEval *passes.FunctionEvaluator

	// Observations populated by affordanceEval each pipeline run; cleared at
	// the start of updatePreview so the slice mirrors the current SQL.
	observations []nanopass.Observation

	// Affordance instances rendered against observations. Order is checked
	// in registration order; first Matches wins. State (test inputs etc.)
	// lives on the affordance struct so it survives across debounce ticks.
	affordances []sqlAffordanceI

	// Shared regex test-input buffer for affordances that match against a
	// user-typed string (the multiMatch* / multiFuzzyMatch* families).
	affordanceTestInput string

	// saveAppletMu guards the Save-as-applet launch request state (ADR-0135
	// §SD7): the "Save as applet…" button opens the standalone creator
	// window (apps/sqlappletcreator) over windowhost.open, seeded with the
	// current buffer; the goroutine holds the bus round-trip and the toolbar
	// reads busy + err under the lock (the openPlay pattern). The authoring
	// form itself moved out of play with the ADR-0132 "O4" seam.
	saveAppletMu   sync.Mutex
	saveAppletBusy bool
	saveAppletErr  string

	// toolbarMinimal attenuates the top bar to the applet surface
	// (ADR-0132 §SD3): Load .sql, the endpoint switcher, the prelude and
	// conditions toggles disappear, and the "Open in Playground" escape
	// hatch appears. Set via SetToolbarMinimal between construction and
	// mount.
	toolbarMinimal bool

	// definition is the document this instance was defined by, when an
	// embedder handed one over (SetDefinitionMarkdown) — a sqlapplet's
	// markdown source. Non-nil puts the "Definition" toggle in the top bar
	// and the drawer behind it; nil in an ordinary playground, where the
	// buffer is the user's own and stands behind no document. See
	// play_definition.go.
	definition *definitionView

	// preamble is the explanatory passage that rides above the result panes,
	// from an applet's `md preamble` fence (SetPreambleMarkdown). nil renders
	// nothing. Parsed once; see play_definition.go.
	preamble *markdown.Doc

	// datasetNotice is the host's runtime notice about this instance's data
	// preconditions (SetDatasetNotice) — an unbound ad-hoc dataset alias, in
	// practice. It rides above the preamble and, unlike it, is re-set as the
	// condition changes. nil renders nothing. See play_definition.go.
	datasetNotice *markdown.Doc

	// Param-slot UI (see play_param_render.go). paramSlots mirrors what
	// the debounced parse extracted from inst.sql; paramDrafts owns the
	// stable string pointers each widget binds via SendRespVal;
	// paramSyncedValues is the drift-detection cache that mirrors the
	// editor's leading SET prelude so the post-render sync stays a
	// no-op until a widget mutates a draft. paramWidgets is the
	// match-order registry — pair widget first, scalar text fallback
	// last. paramHidePrelude (default false) is the "show/hide
	// parameter prelude" toggle; when true, the editor TextEdit binds
	// to paramSqlEdit (the residual after slicing the prelude) and a
	// secondary read-only label renders the prelude above the residual
	// editor.
	// paramLiveSeeded is the LIVE tier's counterpart to paramSyncedValues
	// (ADR-0124's 2026-07-22 §SD4 amendment): per live name, the value the
	// pane last wrote to the store or last took from it. It is both the
	// drift baseline (write only when the draft moved away from it) and the
	// reseed guard (follow the store only when the store moved away from
	// it), so a co-writing panel and the pane do not chase each other.
	paramSlots        []paramSlot
	paramDrafts       map[string]*string
	paramSyncedValues map[string]string
	paramLiveSeeded   map[string]string
	// paramSyncedExprs is the same baseline for the SQL-valued slots (ADR-0187
	// §SD3): per spliced name, the value its `-- play: expr` line
	// last declared. Separate from paramSyncedValues rather than merged into
	// it, because that map is also the PINNED-tier bit (paramPinned reads its
	// membership) and an expression has no prelude tier to be pinned to.
	paramSyncedExprs map[string]string
	// exprMarks is the per-field error underline §SD6 derives by splicing the
	// declared values in and parsing the result. Refreshed by the debounced
	// parse, keyed by slot name, and empty when the substituted buffer parses.
	exprMarks map[string]nanopass.SourceRange
	// securityCeiling is the strongest class a substituted body may reach
	// before the run gate refuses it (ADR-0187 §SD5). The zero value
	// is analysis.QuerySecurityMutating, which is the strongest class there is
	// and therefore refuses nothing — so play, which sets it never, reports the
	// class without enforcing it, and an applet sets its mint-time class.
	securityCeiling analysis.QuerySecurityClassE
	// paramDefaults is what Reset restores: the prelude values the buffer was
	// LOADED with, captured when a buffer is installed and not touched again.
	// It cannot be re-read from the prelude on demand, because a widget's drift
	// rewrites that prelude — the default would then be whatever the reader
	// last did, and the gesture would restore nothing (ADR-0124 Update
	// 2026-08-14). A name with no entry has no default: a live name is
	// panel-driven, and Reset leaves it to its panel.
	paramDefaults          map[string]string
	paramWidgets           []paramWidgetI
	paramEvaluator         *evaluator.Evaluator
	paramHidePrelude       bool
	paramSqlEdit           string
	paramSqlEditSyncedFrom string

	// M2 capability handles, populated by SetCapabilities from the
	// runtime's MountCtx. Both may be nil when running outside the
	// carousel (legacy CLI command, unit tests, screenshot tour).
	// bus drives "Load .sql" via fs.dialog.read; storage persists
	// the SQL buffer between sessions on Run + Unmount.
	bus     app.BusI
	storage app.StorageI
	logger  zerolog.Logger

	// pickMu guards the goroutine-side load state. The Load button
	// fires loadFromPicker in a goroutine; the Render loop reads
	// pickInFlight + pickErr under the lock to render the status
	// indicator. pickedSql is the loaded buffer awaiting handoff:
	// inst.sql itself is render-thread-only (the editor binding and
	// Run path read and write it unlocked), so the goroutine must
	// never assign it directly — it stashes here and Render consumes
	// once per frame (consumePickedSql). nil = nothing pending.
	pickMu       sync.Mutex
	pickInFlight bool
	pickErr      string

	// openPlayMu guards the Open in Playground request state (ADR-0135
	// §SD7), the pickMu pattern: the button spawns a goroutine holding
	// the bus round-trip; the toolbar reads busy + err under the lock.
	openPlayMu   sync.Mutex
	openPlayBusy bool
	openPlayErr  string
	pickedSql    *string
}

// SetCapabilities is the host-side seam for wiring the runtime's M2
// capabilities (ADR-0026). Called once from PlayLauncher.Mount with
// ctx.Bus() and ctx.Storage(). Either argument may be nil — the
// "Load .sql" button stays hidden when bus is nil; persist save/
// restore is skipped when storage is nil.
func (inst *PlayApp) SetCapabilities(bus app.BusI, storage app.StorageI, logger zerolog.Logger) {
	inst.bus = bus
	inst.storage = storage
	inst.logger = logger

	// Wire the time-range evaluator + fan it out to widgets that
	// opt into evaluatorAwareI. Nil-bus or constructor failure
	// leaves paramEvaluator nil; the range widget then declines
	// matches and the simpler DateTimePickerButton-pair widget
	// (registered next in the order) claims the from/to slots.
	//
	// Only fan the evaluator out when actually constructed —
	// passing a typed-nil *evaluator.Evaluator through an interface
	// parameter would land non-nil on the widget side and trip the
	// classic Go interface-nil trap.
	ev, evErr := evaluator.NewEvaluator(bus, timerangepicker.PoolName)
	if evErr != nil {
		logger.Debug().Err(evErr).Msg("play: time-range evaluator unavailable (falling back to dateTimePairWidget)")
		return
	}
	inst.paramEvaluator = ev
	for _, w := range inst.paramWidgets {
		if ea, ok := w.(evaluatorAwareI); ok {
			ea.SetTimeRangeEvaluator(ev)
		}
	}
}

// RestorePersistedSql replaces inst.sql with the value stored under
// persistKeyLastSql when storage is wired and the value is non-empty.
// Best-effort: errors are logged at debug level and the existing
// inst.sql stays.
//
// Read-only bridge (ADR-0148 §SD8, added 2026-07-29): nothing writes this
// key any more — the editor buffer now survives as a workingset record
// the window host pulls at close. Mount consults the key only for a
// window that received no config at all, so a session that predates the
// workingset era still finds its buffer. Retire the key, this function,
// and the PersistedKeys entry one release on.
func (inst *PlayApp) RestorePersistedSql() {
	if inst.storage == nil {
		return
	}
	value, found, err := inst.storage.Get(persistKeyLastSql)
	if err != nil {
		inst.logger.Debug().Err(err).Msg("play: persist restore failed (continuing with default sql)")
		return
	}
	if !found || len(value) == 0 {
		return
	}
	inst.sql = string(value)
	// The restored buffer is the one this session starts from, so its prelude
	// is what Reset restores to — not the compiled-in default it replaced.
	inst.captureParamDefaults(inst.sql)
}

// RestorePersistedTimelineBandsSql loads the bands-SQL editor buffer
// from the persist cap. Same best-effort semantics — and the same
// one-release read-only bridge — as RestorePersistedSql.
func (inst *PlayApp) RestorePersistedTimelineBandsSql() {
	if inst.storage == nil {
		return
	}
	value, found, err := inst.storage.Get(persistKeyTimelineBandsSql)
	if err != nil {
		inst.logger.Debug().Err(err).Msg("play: persist restore (bands) failed")
		return
	}
	if !found || len(value) == 0 {
		return
	}
	inst.timelineBandsSql = string(value)
}

// loadFromPicker is the goroutine driving an fs.dialog.read +
// fs.handle.{uuid}.read round-trip. State updates happen under
// pickMu so the Render loop sees a consistent snapshot. Errors
// surface on inst.pickErr and render below the toolbar as a small
// muted label; the editor buffer is untouched on failure.
//
// Matches capdemo.runPick — the goroutine pattern is the
// recommended template for any synchronous Request that the Frame
// goroutine can't block on directly.
func (inst *PlayApp) loadFromPicker() {
	if inst.bus == nil {
		return
	}
	inst.setLoadBusy(true)
	defer inst.setLoadBusy(false)

	rawReply, rerr := inst.bus.RequestWithTimeout(fsbroker.SubjectDialogRead, nil, fsbroker.DialogTimeout)
	if rerr != nil {
		inst.setLoadErr("fs.dialog.read: " + rerr.Error())
		return
	}
	dr, jerr := fsbroker.UnmarshalDialogReply(rawReply)
	if jerr != nil {
		inst.setLoadErr("dialog reply parse: " + jerr.Error())
		return
	}
	if !dr.Granted {
		inst.setLoadErr("dialog denied: " + dr.Reason)
		return
	}
	body, rerr := inst.bus.RequestWithTimeout(dr.HandleSubjectPrefix+".read", nil, fsbroker.HandleOpTimeout)
	if rerr != nil {
		inst.setLoadErr("handle read: " + rerr.Error())
		return
	}
	// Successful load — stash the buffer for the render thread. inst.sql is
	// render-thread-only (read/written unlocked by the editor binding and the
	// Run path), so assigning it from this goroutine would race a concurrent
	// frame (review finding); consumePickedSql applies it at the next frame
	// top, after which the debounce re-formats and the next Run persists.
	loaded := string(body)
	inst.pickMu.Lock()
	inst.pickedSql = &loaded
	inst.pickErr = ""
	inst.pickMu.Unlock()
}

// consumePickedSql applies a picker-loaded buffer to inst.sql, on the render
// thread, at most once per stash. Called at the top of Render so the load
// lands regardless of which dock tab is active (unlike the snippet pendings,
// which the Editor tab consumes).
func (inst *PlayApp) consumePickedSql() {
	inst.pickMu.Lock()
	picked := inst.pickedSql
	inst.pickedSql = nil
	inst.pickMu.Unlock()
	if picked != nil {
		inst.sql = *picked
	}
}

func (inst *PlayApp) setLoadBusy(b bool) {
	inst.pickMu.Lock()
	inst.pickInFlight = b
	if b {
		inst.pickErr = ""
	}
	inst.pickMu.Unlock()
}

func (inst *PlayApp) setLoadErr(s string) {
	inst.pickMu.Lock()
	inst.pickErr = s
	inst.pickMu.Unlock()
}

// playInstanceSeq numbers PlayApp constructions within the process; mixed
// into each instance's widget-id base salt by playInstanceSalt.
var playInstanceSeq atomic.Uint64

// playInstanceSalt derives a per-instance widget-id base salt. Two PlayApp
// instances rendering in the same frame (two applet windows, two play
// windows) would otherwise derive identical effective ids from their
// root-seeded stacks — colliding in the global seenIds registry and sharing
// egui widget state across windows. The app fleet fixed this by adopting the
// host-salted ctx.Ids() stack (ADR-0026 §SD9); play deviates from that
// literal shape because one instance spans a dozen driver-owned stacks, so
// the equivalent here is salting every stack's base with one per-instance
// value (SetBaseSalt survives the per-frame Reset). The play-unique constant
// keeps another package's equal counter from colliding on a shared label —
// the cross-app parity failure the fleet migration was about.
func playInstanceSalt() uint64 {
	const playSaltTag = 0x506c6179_41707021 // "PlayApp!"
	return (playInstanceSeq.Add(1) * 0x9e3779b97f4a7c15) ^ playSaltTag
}

// NewPlayApp builds the playground over a client, its query graph, the
// buffer it opens with, and the gloss rule repository (ADR-0186) — the
// standing rules and the catalog the host hands in; nil means play's own
// DefaultRepository.
func NewPlayApp(client *Client, graph *queryGraph, initialSQL string, rules *gloss.Repository) *PlayApp {
	salt := playInstanceSalt()
	if rules == nil {
		rules = DefaultRepository()
	}
	mk := func() *c.WidgetIdStack {
		s := c.NewWidgetIdStack()
		s.SetBaseSalt(salt)
		return s
	}
	cardIds := mk()
	pagerIds := mk()
	projectorIds := mk()
	projFSMIds := mk()
	queryFSMIds := mk()
	timelineIds := mk()
	cards := NewCardDriver(cardIds, nil)
	projFSM := newProjectorFSM()
	queryFSM := newQueryFSM()
	// client may be nil in tests and the legacy CLI path; the endpoint switcher
	// is guarded behind a non-nil client in renderTopBar, so an empty external
	// URL is harmless here.
	externalURL := ""
	if client != nil {
		externalURL = client.URL()
	}
	inst := &PlayApp{
		ids:              mk(),
		graph:            graph,
		client:           client,
		intermediateLane: newNodeLane(clientExecutor{client: client, opts: newExecOptions("intermediate")}, memory.NewGoAllocator(), 0),
		endpointDraft:    externalURL,
		externalURL:      externalURL,
		autoEndpoint:     true,
		density:          styletokens.ActiveDensity(),
		sql:              initialSQL,
		rules:            rules,
		sigEmit:          graphEmitter{graph: graph},
		cards:            cards,
		projector:        NewProjector(projectorIds, cards),
		schemaModel:      schemaview.NewModel(nil),
		projFSM:          projFSM,
		projFSMWidget: fsmview.New(projFSMIds, "projector-fsm", projFSM).
			Title("UMAP projector").
			ShowSubscript(true).
			AutoAnchor(true),
		queryFSM: queryFSM,
		queryFSMWidget: fsmview.New(queryFSMIds, "query-state-fsm", queryFSM).
			Title("Query result state").
			Tethered().
			BadgeTone(queryStateTone).
			AutoAnchor(true),
		colorByFeature: -1,
		pager:          pager.New(pagerIds, int64(defaultPageSize)),
		affordances: []sqlAffordanceI{
			&multiMatchAffordance{},
		},
		paramDrafts:       map[string]*string{},
		paramSyncedValues: map[string]string{},
		paramLiveSeeded:   map[string]string{},
		lazyPanes:         map[uint64]*lazypane.Pane{},
		sigValDrafts:      map[string]*string{},
		sigValSeeded:      map[string]string{},
		// Range widget first so the Grafana-style picker (when the
		// host has wired an evaluator via SetCapabilities) folds the
		// from/to pair; otherwise its Matches returns ok=false and
		// the simpler dateTimePairWidget claims the slots. The enum
		// widget comes after both: a declared option list is a
		// per-slot statement, and a range is a statement about two
		// slots at once, so letting the pair fold first keeps a
		// stray enum hint on one half from splitting a picker.
		// Scalar text widget is the tail catch-all — one TextEdit per
		// remaining slot.
		paramWidgets: []paramWidgetI{
			newDateTimeRangeWidget(),
			newDateTimePairWidget(),
			newEnumWidget(),
			// The SQL field sits after the enum and before the tail: a declared
			// option list is more specific than a declared category, so a
			// dropdown of predicates over an {x:Expr} slot stays possible
			// (ADR-0187 M1).
			newExprWidget(),
			newScalarTextWidget(),
		},
	}
	// The buffer this instance opens with is the one Reset restores to. For an
	// applet that is the document's declared prelude, which is the case the
	// gesture exists for.
	inst.captureParamDefaults(initialSQL)
	inst.timeline = NewTimelineDriver(timelineIds, client, &inst.timelineBandsSql, &inst.timelineNowLineEnabled)
	inst.mapDriver = NewMapDriver(mk(), client)
	inst.worldDriver = NewWorldDriver(mk())
	inst.kanbanDriver = NewKanbanDriver(mk(), client)
	inst.networkDriver = NewNetworkDriver(mk(), client)
	inst.sankeyDriver = NewSankeyDriver(mk(), client)
	inst.distDriver = NewDistDriver(mk())
	inst.icicleDriver = NewIcicleDriver(mk())
	inst.treemapDriver = newTreemapDriver(mk())
	inst.filesDriver = newFilesDriver(mk())
	inst.chartDriver = NewChartDriver(mk())
	// The scaffold seam is the PUBLIC delivery op, not a private reach into
	// the editor: a pane that writes SQL into the buffer is exactly the
	// snippet-class capability play_delivery.go was made public for.
	inst.seriesDriver = NewSeriesDriver(mk(), func(sql string) { inst.InsertSqlAtCaret(sql) })
	inst.tsCollisions = newTsCollisionProbe(client)
	inst.vocab = newVocabProbe(client)
	inst.seriesLabels = newTsLabelsWriter(client)
	inst.fixtures = &fixtureState{}
	inst.flow = newFlowDriver(mk(), client)
	inst.richCells = newRichCellCache(mk())
	inst.detailTimeline = NewDetailTimeline(mk())
	inst.components = newComponentDetail(mk())
	inst.identity = newIdentityDetail(mk())
	// The Experiments pane's card emitters get a stack on a DIFFERENT base
	// salt, not merely a different instance. PrepareSeq maps its argument
	// through makeHighEntropy alone and Derive XORs it with the enclosing
	// scope, which on an empty stack is the base salt — so two stacks built by
	// mk() produce byte-identical ids for the same argument. The pane and the
	// Detail tab both render a Table2CardEmitter over the same result in the
	// same frame, so sharing a salt makes every cell id a duplicate.
	expCardIds := mk()
	expCardIds.SetBaseSalt(salt ^ experimentsCardSaltMix)
	inst.experiments = newExperimentsDriver(mk(), expCardIds)
	inst.diag = NewDiagnosticsDriver(client)
	var docsSource DocsSourceI
	if client != nil {
		docsSource = NewClickHouseDocsSource(client)
	}
	inst.docs = newDocsDriver(docsSource)
	inst.docsPane = newDocsPaneState()
	inst.runsHist = newRunsHistoryDriver(client)
	inst.pins = newPinDriver(client)
	inst.pinsBrowser = newPinsBrowserDriver(client)
	inst.affordanceEval = newAffordanceEvaluator(&inst.observations)
	// Last: the tab set closes over the drivers above (slice 6a).
	inst.tabs = defaultTabs(inst)
	inst.vizSeed = nextVizSeed()
	return inst
}

// Tabs is the instance's dock-tab set (ADR-0097 slice 6a, D4): an embedder
// customizes it — Add/Replace/Remove — between construction and mounting;
// the first Render freezes it. See TabSpec for the registration shape.
func (inst *PlayApp) Tabs() *TabRegistry { return inst.tabs }

// editorTabPresent reports whether the tab set still carries the Editor tab.
// An embedder that removed it (a sqlapplet window, ADR-0132 §SD3) gets the
// params strip pinned into the top panel instead of above the editor.
func (inst *PlayApp) editorTabPresent() bool {
	_, ok := inst.tabs.dockIDForSlug("editor")
	return ok
}

// Close tears down the app's async machinery (Unmount): cancels in-flight
// work, releases held results, and closes every lane. Late completions from
// still-running goroutines hit their generation/closed guards and are
// dropped. Idempotent; the app is unusable afterwards.
func (inst *PlayApp) Close() {
	if inst.projector != nil {
		inst.projector.Detach()
	}
	inst.components.release()
	inst.identityJob.stop()
	if inst.intermediateLane != nil {
		inst.intermediateLane.close()
	}
	inst.closeBoundLanes()
	if inst.mapDriver != nil && inst.mapDriver.lane != nil {
		inst.mapDriver.lane.close()
	}
	if inst.timeline != nil && inst.timeline.bandsLane != nil {
		inst.timeline.bandsLane.close()
	}
	if inst.kanbanDriver != nil && inst.kanbanDriver.lanesLane != nil {
		inst.kanbanDriver.lanesLane.close()
	}
	if inst.networkDriver != nil {
		if inst.networkDriver.edgesLane != nil {
			inst.networkDriver.edgesLane.close()
		}
		if inst.networkDriver.verticesLane != nil {
			inst.networkDriver.verticesLane.close()
		}
	}
	if inst.sankeyDriver != nil {
		if inst.sankeyDriver.flowsLane != nil {
			inst.sankeyDriver.flowsLane.close()
		}
		if inst.sankeyDriver.nodesLane != nil {
			inst.sankeyDriver.nodesLane.close()
		}
	}
	// The Series tab's two optional-CTE lanes are created on first demand, so
	// both are nil until a buffer names `scores` or `spans` (ADR-0163 §SD1).
	if inst.seriesScoresLane != nil {
		inst.seriesScoresLane.close()
	}
	inst.tsCollisions.close()
	inst.vocab.close()
	if inst.seriesSpansLane != nil {
		inst.seriesSpansLane.close()
	}
	if inst.seriesLabelsLane != nil {
		inst.seriesLabelsLane.close()
	}
	inst.flow.closeLanes()
	inst.docs.close()
	if inst.diag != nil {
		inst.diag.close()
	}
	if inst.graph != nil {
		inst.graph.close() // also closes the main lane
	}
}

// newProjectorFSM seeds the fsmview.Machine with every transition the
// Projector is known to take, plus operator-friendly edge labels. The
// rule set mirrors the actual mutation sites in play_projection.go
// (Start / Cancel / run-goroutine / fail / markCancelled / Invalidate);
// any divergence shows up as a "transition rejected" log warning in
// renderProjection's mirror step, and a missing arrow in the popup
// graph view.
func newProjectorFSM() *fsmview.Machine[projectorStatusE] {
	m := fsmview.NewMachine(projectorStatusIdle, 64,
		fsmview.WithLabel(func(s projectorStatusE) string { return s.String() }),
		fsmview.WithStateOrder([]projectorStatusE{
			projectorStatusIdle,
			projectorStatusExtracting,
			projectorStatusRunning,
			projectorStatusCancelling,
			projectorStatusCancelled,
			projectorStatusDone,
			projectorStatusFailed,
		}),
	)
	m.AddRule(projectorStatusIdle, projectorStatusExtracting).
		AddRule(projectorStatusExtracting, projectorStatusRunning, projectorStatusCancelling, projectorStatusFailed).
		AddRule(projectorStatusRunning, projectorStatusDone, projectorStatusCancelling, projectorStatusFailed).
		AddRule(projectorStatusCancelling, projectorStatusCancelled, projectorStatusFailed).
		AddRule(projectorStatusDone, projectorStatusIdle, projectorStatusExtracting).
		AddRule(projectorStatusFailed, projectorStatusIdle, projectorStatusExtracting).
		AddRule(projectorStatusCancelled, projectorStatusIdle, projectorStatusExtracting).
		EdgeLabel(projectorStatusIdle, projectorStatusExtracting, "Compute").
		EdgeLabel(projectorStatusExtracting, projectorStatusRunning, "features ready").
		EdgeLabel(projectorStatusExtracting, projectorStatusCancelling, "Cancel").
		EdgeLabel(projectorStatusExtracting, projectorStatusFailed, "fail").
		EdgeLabel(projectorStatusRunning, projectorStatusDone, "UMAP fit").
		EdgeLabel(projectorStatusRunning, projectorStatusCancelling, "Cancel").
		EdgeLabel(projectorStatusRunning, projectorStatusFailed, "fail").
		EdgeLabel(projectorStatusCancelling, projectorStatusCancelled, "drained").
		EdgeLabel(projectorStatusCancelling, projectorStatusFailed, "fail").
		EdgeLabel(projectorStatusDone, projectorStatusIdle, "Invalidate").
		EdgeLabel(projectorStatusDone, projectorStatusExtracting, "Recompute").
		EdgeLabel(projectorStatusFailed, projectorStatusIdle, "Invalidate").
		EdgeLabel(projectorStatusFailed, projectorStatusExtracting, "retry").
		EdgeLabel(projectorStatusCancelled, projectorStatusIdle, "Invalidate").
		EdgeLabel(projectorStatusCancelled, projectorStatusExtracting, "retry")
	return m
}

// activeSnapshot returns the result the panels should render: the observed
// node's (ADR-0097 3d). By default that is the sink (the main lane); when the
// user observes an intermediate node from the Graph view, its fused SQL is
// demanded on the intermediate lane — non-blocking, latest-wins (a changed
// fused SQL supersedes the in-flight run), last-good retained — and the lane
// view maps into the snapshot tuple. The caller MUST Release the returned
// record (nil-safe), exactly as for MainSnapshot.
func (inst *PlayApp) activeSnapshot() (rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, elapsed time.Duration, summary Summary, executed time.Time, err error, id ResultID) {
	split := inst.currentSplit
	if inst.observedNode != "" && inst.observedNode != split.Sink && len(split.Nodes) > 0 {
		if node, ok := findSplitNode(split, inst.observedNode); ok {
			// The intermediate's signal values resolve from its Reads (the
			// split's signal edges) against the frame snapshot; names the
			// last Run's prelude bound are constants and travel inside the
			// fused SQL's SET prelude instead (slice 5a).
			view := inst.intermediateLane.demand(
				compileNodeFor(split, node, inst.lastRunBound, inst.frameSig))
			if view.rec != nil {
				numRows = view.rec.NumRows()
			}
			return view.rec, view.schema, numRows, view.loading, view.elapsed, view.summary, view.executedAt, view.err, view.id
		}
	}
	return inst.graph.MainSnapshot()
}

// activeTruncation reports why the result activeSnapshot returned is a
// prefix, or "" when it is whole (E3, R9). Only the main lane carries it: an
// intermediate is a fused sub-query, and whatever bounds it is visible in
// the SQL the user is already looking at.
func (inst *PlayApp) activeTruncation() (reason string) {
	split := inst.currentSplit
	if inst.observedNode != "" && inst.observedNode != split.Sink && len(split.Nodes) > 0 {
		if _, ok := findSplitNode(split, inst.observedNode); ok {
			return
		}
	}
	if inst.graph == nil {
		return
	}
	reason = inst.graph.MainTruncation()
	return
}

// MainSnapshot returns a retained view of the `main` node's current result —
// the sink result the Table tab shows by default — with its metadata. It is the
// thread-safe embedder seam for reading the live resultset OFF the render loop
// (e.g. an on-demand report server behind a re-user's own pane): the read is
// guarded by the main lane's QueryStore lock, so it is safe from any goroutine,
// unlike the render-thread-only activeSnapshot (which observes the intermediate
// lane and per-frame split state). The caller MUST Release the returned record
// (nil-safe); rec is nil until the first result lands, with loading/err then
// reflecting the lane state. id is the result's ResultID, read under the same
// lock as everything else here. See doc/howto/play-pluggable-detail.md for the
// companion body/tab seams.
func (inst *PlayApp) MainSnapshot() (rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, elapsed time.Duration, summary Summary, executed time.Time, err error, id ResultID) {
	if inst.graph == nil {
		return
	}
	return inst.graph.MainSnapshot()
}

// MainSQL returns the executed SQL text behind the `main` node's current result
// — the query MainSnapshot's record was produced by — or "" when none has
// landed yet. It is the thread-safe companion to MainSnapshot for embedders
// that need the query behind the live result (e.g. deriving per-result query
// lineage): the read is guarded by the main lane's lock, so it is safe off the
// render loop. Being a second, independent lock acquisition, a result that
// completes between a MainSnapshot and a MainSQL call can momentarily pair rows
// from one query with the SQL of the next; for human-driven single-query flows
// that window is not observable.
func (inst *PlayApp) MainSQL() string {
	if inst.graph == nil {
		return ""
	}
	return inst.graph.MainSQL()
}

// Frame renders one frame with the per-frame capabilities the host offers on
// ctx. It is the entry point for every host: the launcher below, and equally
// any library that builds a PlayApp of its own and re-hosts it.
//
// Two capabilities arrive this way, both in the ADR-0155 §SD1 shape — an
// optional interface type-asserted off the context, so a host that cannot
// offer one owes nothing and nothing errors:
//
//   - [app.WindowFocusI] gates the process-global Ctrl+Enter chord. The chord
//     is drained once from egui's shared queue and is visible to every open
//     instance alike, so without the gate one press runs a query in all of
//     them. A host that cannot say is taken as focused: single-surface hosts
//     and tests have only the one instance, and it is the active one.
//   - [colwidth.HostI] carries the column-width store (ADR-0151). It is
//     acquired here rather than at Mount because it is a frame-context
//     capability; ensureColWidthRes does the once-only work.
//
// The render pass itself is unexported precisely because of this: a re-host
// holding a frame context has no reason to skip either capability, and every
// re-host that called the pass directly silently lost both. Making the
// context the only way in turns that from a rule to remember into a signature.
func (inst *PlayApp) Frame(ctx app.FrameContextI) (err error) {
	focused := true
	if f, ok := ctx.(app.WindowFocusI); ok {
		focused = f.WindowFocused()
	}
	inst.windowUnfocused = !focused
	inst.ensureColWidthRes(ctx)
	err = inst.render()
	return
}

func (inst *PlayApp) render() error {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	ids := inst.ids
	ids.Reset()

	// Apply a picker-loaded buffer before anything reads inst.sql this frame.
	inst.consumePickedSql()

	// One signal snapshot per frame (slice 5a): every compile and consumer
	// this frame sees a single store revision.
	inst.frameSig = inst.graph.signals()

	// One Snapshot per frame, with a matching release at end-of-frame.
	// Tab bodies are captured into detached buffers by the DockArea
	// iterator and flushed when the dock scope exits — all per-frame
	// state syncs (selection clamp, pager configure, projector
	// invalidate) must run here, before any tab body executes, so the
	// values the tab callees observe are consistent.
	rec, schema, numRows, loading, elapsed, summary, executed, err, resultID := inst.activeSnapshot()
	if rec != nil {
		defer rec.Release()
	}
	// Drive the bound nodes' lanes against this frame's snapshot (slice 6c)
	// — one demand per distinct bound node; the views feed frameFor below.
	// The pager/projector/schema syncs moved into their tabs, which since 6c
	// render from their OWN (possibly bound) frame view.
	releaseBound := inst.demandBoundNodes()
	defer releaseBound()
	if rec != nil {
		inst.syncSelectionClamp(rec)
	}

	// Mirror the result↔input lifecycle into the query FSM every frame —
	// runs outside the rec!=nil guard so idle / empty / failed are observed
	// too. The status bar and its chip both read inst.queryFSM.
	inst.syncQueryFSM(loading, numRows, executed, err)

	// Fold this frame's progress tick into the ETA estimator, once, before
	// any display site reads it (the damped ETA is stateful — see
	// progressTracker.observe).
	inst.syncProgress()

	// The `main` live toggle (slice 5e, D2): a referenced-signal move
	// re-runs the unchanged buffer through the ordinary Run path below.
	// Re-checking Live retires the breaker's notice: the checkbox on screen
	// is the state of the system, so turning it back on IS the resume
	// gesture (the breaker unchecked it when it tripped).
	if inst.liveMain && inst.liveSuspendReason != "" {
		inst.resumeLiveAfterHumanAction()
	}
	if inst.shouldAutoRun() {
		inst.requestRun = true
		inst.runIsAuto = true
		// Count it against the loop witness before it fires: an emit-driven
		// cycle is only visible in the sequence of auto-runs.
		inst.noteAutoRunFired()
	}
	inst.pollRunShortcuts()

	// Fold this frame's state into the workingset intent flag (ADR-0148
	// §SD8), once, before anything reads it. Runs every frame regardless
	// of which tabs are open — an edit in the Timeline's bands editor is
	// intent whether or not the Editor tab is visible.
	inst.syncWorkingsetDirty()

	// Run the canonical-form pipeline once per frame regardless of which
	// tab is active. The pipeline is debounced internally (previewDebounce),
	// so most frames are a no-op; running it here keeps the Preview tab's
	// output fresh even when the user has the Editor tab hidden.
	inst.updatePreview()

	// Keep the Diagnostics EXPLAIN probe warm regardless of tab visibility:
	// an armed probe (grammar-rejected buffer) executes on its lane so the
	// verdict is ready when the tab opens; unchanged/unarmed probes are a
	// memo-hit no-op.
	_, _ = inst.diag.probeView()

	// Layout inside the runtime-created window scope (ADR-0026
	// Amendment 2026-05-12). Mirrors imztop's shape: a pinned topbar
	// with controls, a single DockArea hosting the body panes as
	// drag-rearrangeable tabs, and a non-resizable status bar for
	// per-result metrics. The DockArea's initial split lives in the
	// InitRoot/Split block; once the user drags, the persistent
	// dock_state on the Rust side wins.
	for range c.PanelTopInside(ids.PrepareStr("topbar")).Resizable(false).KeepIter() {
		inst.renderTopBar(schema)
		// The params strip (ADR-0132 §SD3): with the Editor tab removed, the
		// param widgets — normally drawn above the SQL editor — re-home here,
		// pinned. Exactly one site renders per frame (the editor path when
		// the tab exists, this strip when it does not), so the drafts and
		// widget ids stay single-writer. refreshParamSlotsFromParse runs in
		// updatePreview, so the slots are fresh without an editor.
		if !inst.editorTabPresent() {
			inst.renderParamSlots()
		}
	}
	for range c.PanelBottomInside(ids.PrepareStr("status")).Resizable(false).KeepIter() {
		inst.renderStatus(numRows, elapsed, summary, executed, err, inst.activeTruncation())
	}
	// The definition drawer, when an embedder gave this instance a document
	// and the reader opened it. Before the central panel: side panels claim
	// their width in the order they are added.
	inst.renderDefinitionPanel()
	for range c.PanelCentralInside().KeepIter() {
		// The host's data-precondition notice, then the author's explanatory
		// passage, above every result pane and below the controls — an
		// applet's `md preamble` fence. The notice sits first: it says why
		// the panes below it are empty, which outranks what they mean.
		inst.renderDatasetNotice()
		inst.renderPreamble()
		for dock := range c.DockArea(ids.PrepareStr("play-dock")) {
			if inst.pendingDockActivate != 0 {
				dock.ActivateTab(inst.pendingDockActivate)
				inst.pendingDockActivate = 0
			}
			// One loop over the tab registry (ADR-0097 slice 6a): the
			// initial layout derives from the zones, the focus knob is a
			// reorder over the body zone, and every tab body renders from
			// the same per-frame view. First Render freezes the set (D4).
			inst.tabs.freeze()
			frame := TabFrame{
				Rec: rec, Schema: schema, NumRows: numRows,
				Loading: loading, Elapsed: elapsed, Summary: summary,
				Executed: executed, Err: err, Result: resultID,
				Sig: inst.frameSig, Emit: inst.sigEmit,
			}
			// Every zone runs through the same reorder: a fresh leaf
			// activates its first tab, so raising the focused one is what a
			// BOXER_PLAY_FOCUS_* knob means in any of them (ADR-0097 Update
			// 2026-08-01 — it used to mean it only in the body zone).
			focused := focusedTabIDs()
			editorIDs := zoneTabOrder(inst.tabs.byZone(TabZoneEditor), focused)
			bodyIDs := zoneTabOrder(inst.tabs.byZone(TabZoneBody), focused)
			rootIDs := editorIDs
			if len(rootIDs) == 0 {
				rootIDs = bodyIDs // an embedder removed the editor zone
			}
			rootLeaf := dock.InitRoot(rootIDs...)
			bodyLeaf := rootLeaf
			if len(editorIDs) > 0 && len(bodyIDs) > 0 {
				bodyLeaf = dock.Split(rootLeaf, c.DockBelow, 0.45, bodyIDs...)
			}
			// Bottom before side, and the order is the layout: this split takes
			// the whole body's width, and the side split then narrows only what
			// is left above it. Reversing them would put the bottom pane under
			// the body column alone, beside the side pane rather than under it.
			//
			// bodyLeaf keeps addressing the surviving top half: the interpreter
			// remaps a split's parent handle to the old node and gives the new
			// node its own, which is the same property the tools zone relies on
			// when it splits rootLeaf after the body already did.
			if bottom := zoneTabOrder(inst.tabs.byZone(TabZoneBottom), focused); len(bottom) > 0 {
				_ = dock.Split(bodyLeaf, c.DockBelow, 0.60, bottom...)
			}
			if side := zoneTabOrder(inst.tabs.byZone(TabZoneSide), focused); len(side) > 0 {
				_ = dock.Split(bodyLeaf, c.DockRight, 0.70, side...)
			}
			if tools := zoneTabOrder(inst.tabs.byZone(TabZoneTools), focused); len(tools) > 0 {
				_ = dock.Split(rootLeaf, c.DockRight, 0.55, tools...)
			}
			for _, spec := range inst.tabs.all() {
				// Per-tab frame view (slice 6c): a bound tab renders its
				// node's lane view instead of the active result, and its
				// dock title names the node — plus, since the 2026-07-27
				// Update, one mark for what the graph knows about the pane
				// (can it draw this shape, does it drive this query).
				tabFrame := inst.frameFor(spec.ID, &frame)
				title := inst.tabTitle(&spec, &tabFrame)
				if spec.NoScroll {
					for range dock.TabNoScroll(spec.DockID, title) {
						inst.renderTabBody(&spec, title, &tabFrame)
					}
					continue
				}
				for range dock.Tab(spec.DockID, title) {
					inst.renderTabBody(&spec, title, &tabFrame)
				}
			}
		}
	}

	// Execute after rendering — keeps the UI responsive on the submit frame.
	if inst.requestRun && !inst.graph.MainLoading() {
		inst.requestRun = false
		auto := inst.runIsAuto
		inst.runIsAuto = false
		sub := inst.requestSubquery
		inst.requestSubquery = false
		inst.executeRun(auto, sub)
	}

	inst.frame++
	inst.autoShotTick()
	c.RequestRepaint()
	return nil
}

// renderTabBody emits one tab's body, routed through its lazy pane when the
// spec opts in (TabSpec.Lazy): while the host discards the tab's buffer, the
// pane emits only a visibility probe plus a loading placeholder, and the
// heavy body lands one frame after activation (widgets/lazypane). Send-once
// protocols under a revealed body re-arm through the starved-texture report
// as usual; the panes are per-DockID and persistent across frames.
func (inst *PlayApp) renderTabBody(spec *TabSpec, title string, f *TabFrame) {
	if spec.Lazy {
		pane := inst.lazyPanes[spec.DockID]
		if pane == nil {
			pane = lazypane.New("play-dock-tab-"+spec.ID, title)
			pane.HoldFrames = lazyPaneHoldFrames
			inst.lazyPanes[spec.DockID] = pane
		}
		pane.Title = title // bound tabs rename themselves (slice 6c)
		if pane.Skip() {
			return
		}
	}
	// A run replacing a result the pane is ALREADY showing: the body below
	// keeps drawing the previous rows (last-good, no flicker — see
	// nodeLane.demand), and without this strip nothing in the pane says a
	// new result is on the way. The empty-state spinner cannot cover this:
	// it is reached only when there is no result at all.
	//
	// Full-width result panes only. Chrome tabs (Graph, and every tool
	// pane in its own leaf) do not render the frame at all, and the side
	// zone is narrow by design — Detail is ~250 pt wide, where bar +
	// numbers clip, and the body pane beside it is already carrying the
	// same strip. The bottom zone is full width, so it keeps the strip:
	// the reason to withhold one is the pane's width, not its leaf.
	if spec.Panel != nil && (spec.Zone == TabZoneBody || spec.Zone == TabZoneBottom) && f.Loading && f.Rec != nil {
		inst.renderPaneProgressStrip(inst.tabOnActiveLane(spec.ID))
	}
	spec.Render(f)
}

// pollRunShortcuts turns this frame's Ctrl+Enter / Ctrl+Shift+Enter press into
// a run request. Ctrl+Enter is the Run button; Ctrl+Shift+Enter is the same
// run narrowed to the query under the caret.
//
// The press was drained from egui's input queue during the PREVIOUS frame's
// Sync, and the StateManager holds it as per-frame state every reader sees —
// so with more than one play instance open, each instance's poll observes the
// same press. The focus gate below is what makes the binding belong to ONE of
// them: the instance whose window the shell reports active (app.WindowFocusI,
// via PlayLauncher.Frame). Without it, one press ran a query in every open
// playground. A press while a run is already in flight is dropped rather
// than queued, which is what the toolbar does too: it shows Cancel, not Run.
//
// The caret this reads is refreshed later in the same frame, when the editor
// renders, and the request is consumed after that — so a narrowed run always
// resolves against the caret the user can see. With the Editor tab closed the
// caret is wherever it last was, exactly as for the Run button.
func (inst *PlayApp) pollRunShortcuts() {
	run, sub := c.CurrentApplicationState.StateManager.GetCommandEnterPressed()
	inst.claimRunChord(run, sub)
}

// claimRunChord is pollRunShortcuts minus the fetch — the seam a test can
// drive with a synthetic press. Split the way applyRunShortcut is split from
// the poll: no c.* calls past this point.
func (inst *PlayApp) claimRunChord(run, sub bool) {
	if !run && !sub {
		return
	}
	if inst.windowUnfocused {
		// The press belongs to whichever instance IS focused; acting here
		// too is the multi-instance broadcast this gate exists to stop.
		return
	}
	if inst.graph.MainLoading() {
		return
	}
	inst.applyRunShortcut(run, sub)
}

// applyRunShortcut turns a press into a run request. Split from the poll above
// the way executeRun is split from Render — no c.* calls, no lane state — so a
// test can assert that the Run subquery button and Ctrl+Shift+Enter leave the
// app in the same state, which is the rule the toggle rests on.
func (inst *PlayApp) applyRunShortcut(run, sub bool) {
	if !run && !sub {
		return
	}
	inst.requestRun = true
	inst.runIsAuto = false
	inst.requestSubquery = sub
}

// executeRun is the Run path (manual and, since 5e, live-toggle-fired): split
// the buffer, resolve its signal inputs, and launch the main lane. Extracted
// from Render's requestRun block verbatim (no c.* calls) so tests can drive
// it without a UI frame. auto marks a live-toggle run: it skips the persist
// (the persistence point stays anchored to user intent, not signal churn).
// subquery narrows what ships to the innermost query the caret is in; it is
// the Ctrl+Shift+Enter gesture and never fires from the live toggle.
func (inst *PlayApp) executeRun(auto bool, subquery bool) {
	sql := strings.TrimSpace(inst.sql)
	if sql == "" {
		return
	}
	// Run-under-cursor (ADR-0130 L3): a multi-statement body ships the SET
	// prelude plus the statement under the caret; run-subquery narrows one
	// level further, to the innermost query the caret is in, with the
	// enclosing WITH items hoisted in front of it (degrading to the
	// statement when there is nothing narrower). A single-statement,
	// no-subquery buffer ships itself, byte-identical to what it always
	// was. Narrowing runs FIRST so the signal resolution and the unfilled
	// gate below judge the text that actually ships — a slot referenced
	// only outside it neither blocks the run nor rides its request, and
	// narrowing away a broken half is precisely what the gesture is for.
	runSQL, _, _ := inst.runBuffer()
	scope := runScopeWhole
	if subquery {
		runSQL, scope = inst.runSubqueryBuffer()
	}
	// Resolve the SHIPPED text's unbound param slots against the frame's
	// signal snapshot (slice 5a): the values ride the request URL and the
	// run's history entry snapshots them (D4). The narrowed text carries
	// the full SET prelude (withPrelude), so the bound-name set matches
	// the buffer's; only the referenced-slot set narrows. The resolution
	// and the bound-name set also feed the staleness condition — scoped to
	// these same params, see runSignalsDiverged — and the observed
	// intermediates.
	sigParams, boundNames, unfilled := inst.resolveRunSignals(runSQL)
	// An unfilled input (referenced in what ships, neither SET-bound nor
	// signal-written) can only fail server-side — block the doomed request
	// with an actionable hint instead (slice-5 D3's empty-state, applied
	// at the Run gate). The raw-fallback path (parse failure) resolves
	// nothing and reports nothing unfilled, so it still defers to the
	// server.
	if len(unfilled) > 0 {
		// The hint points AT the fix: since the pane writes the live tier
		// (ADR-0124's 2026-07-22 amendment) every unfilled name has its own
		// typed widget in PARAMETERS, marked "needs a value". The Signals
		// editor stays the fallback for names the buffer does not reference.
		inst.runBlockedReason = "unfilled parameter {" + strings.Join(unfilled, "}, {") +
			"} — fill it in the PARAMETERS pane, or bind it with SET param_<name> = …"
		return
	}
	// The class ceiling (ADR-0187 §SD5), judged on what the
	// substitution produces rather than on what the document says. It runs
	// after the unfilled gate — an unfilled buffer has nothing substituted to
	// judge, and "fill this in" is the more actionable of the two answers —
	// and before the lane, because refusing the request is the whole point.
	if reason := inst.exprCeilingRefusal(runSQL); reason != "" {
		inst.runBlockedReason = reason
		return
	}
	inst.runBlockedReason = ""
	// ADR-0181 §SD8 M3: an INSERT wrapper executes only behind the explicit
	// write opt-in, and never through the Arrow result machinery — a write
	// answers with a summary, not a stream. Refusal and outcome both ride
	// the query summary line; the result panels keep the last read.
	inst.writeGateNotice = ""
	if runIsInsertWrapper(runSQL) {
		if AllowWrites.Get() == "" {
			inst.writeGateNotice = "the INSERT is gated — set BOXER_PLAY_ALLOW_WRITES=1 to execute writes from play, or copy Preview → As sent and run it via `clickhouse client`"
			return
		}
		inst.executeWriteRun(runSQL, sigParams)
		if !auto {
			inst.noteWorkingsetIntent()
			inst.resumeLiveAfterHumanAction()
		}
		return
	}
	inst.writeRun.clear()
	inst.lastRunScope = scope
	// ADR-0097 3c: split the buffer into the node graph and fuse to the
	// sink for execution. For a single statement the fused SQL is the
	// original (the client re-lifts the SET prelude either way), so this
	// is behaviour-identical. On a split/parse failure, fall back to the
	// raw buffer so ClickHouse reports the error exactly as before.
	executable, split, fErr := fuseToSink(runSQL)
	if fErr != nil {
		executable = runSQL
		split = splitResult{}
	}
	inst.currentSplit = split
	inst.splitErr = fErr
	// A fresh run resets the observed node to the new sink and forgets
	// the intermediate lane's memo (3d): re-observing an intermediate
	// after a Run re-executes against the possibly-changed data.
	inst.observedNode = split.Sink
	inst.intermediateLane.forget()
	// Bound lanes re-execute against the possibly-changed data too; the
	// bindings themselves survive the Run (they revive by node name, 6c).
	inst.forgetBoundLanes()
	// The Network panel's `edges`/`vertices` CTEs are nodes of this query on
	// their own lanes (ADR-0129); forget them on Run so a corrected endpoint or
	// changed data is picked up, rather than memo-hitting a prior error (whose
	// key is the SQL, which a re-Run leaves unchanged).
	inst.networkDriver.forgetLanes()
	// The Kanban panel's `lanes` CTE (ADR-0122 §SD6) is likewise a node of this
	// query on its own lane; forget it on Run for the same reason — a re-Run
	// after a transient failure would otherwise memo-hit the stored error (its
	// key is the SQL, unchanged) and the lane inventory would stay stuck-errored
	// though the board recovered.
	inst.kanbanDriver.forgetLanes()
	// The Sankey panel's `flows`/`nodes` CTEs are the same shape again.
	inst.sankeyDriver.forgetLanes()
	// And the Series panel's optional `scores`/`spans` CTEs (ADR-0163 §SD1).
	inst.forgetSeriesLanes()
	// The Flow tab's EXPLAIN lenses (ADR-0153) wrap this query on their own
	// lanes; same reason again.
	inst.flow.forgetLanes()
	// Scripted-screenshot affordance: observe a named node on run so a
	// capture can show the panels rendering an intermediate (mirrors
	// BOXER_PLAY_FOCUS_*). Ignored when the node is absent.
	if obs := ObserveNode.Get(); obs != "" {
		if _, ok := findSplitNode(split, NodeID(obs)); ok {
			inst.observedNode = NodeID(obs)
		}
	}
	inst.lastSentSql = sql
	inst.lastSentSigParams = sigParams
	inst.lastRunBound = boundNames
	// The history entry records the whole buffer, not just what ran: restoring
	// a run of a multi-statement buffer must bring its siblings back too.
	sourceBuffer := ""
	if runSQL != sql {
		sourceBuffer = sql
	}
	inst.graph.RunMain(executable, sigParams, sourceBuffer)
	if !auto {
		// A manual Run is intent by construction — "this is the query I
		// want" — even when it re-runs an unchanged buffer, so it marks
		// the workingset dirty (ADR-0148 §SD4). The host does the saving,
		// at close; a live-toggle run is signal churn and marks nothing.
		inst.noteWorkingsetIntent()
		// A human Run is the reset for the Live circuit breaker: whatever
		// the streak was measuring, a person has just said what to run.
		inst.resumeLiveAfterHumanAction()
	}
}

// syncSelectionClamp keeps the selection signal valid for the result its
// cursor indexes (slice 5b; node-aware since 6c): an absent or out-of-range
// selection resets to row 0, so a fresh result auto-selects its first row.
// The cursor's node comes from selection_node — a selection made on a BOUND
// node clamps against that node's lane view, not the active result; a
// selection whose node vanished this frame (unbound, or gone from the
// split) retargets home to the active node. The write lands in the store
// immediately and is visible from the NEXT frame's snapshot; this frame's
// panels guard out-of-range rows themselves, so the one-frame window is
// benign. An in-range selection writes nothing (a repeated identical write
// does not bump the store revision).
func (inst *PlayApp) syncSelectionClamp(rec arrow.RecordBatch) {
	if rec == nil {
		return
	}
	target := rec
	if raw := inst.selectionNodeRaw(); raw != "" && raw != string(inst.activeNodeID()) {
		v, visible := inst.boundViews[NodeID(raw)]
		if !visible || v.rec == nil {
			// The cursor's node is not on screen any more — send it home.
			inst.graph.setSignalRawFrom(signalSelection, "0", signalWriterClamp)
			inst.graph.setSignalRawFrom(signalSelectionNode, string(inst.activeNodeID()), signalWriterClamp)
			return
		}
		target = v.rec
	}
	row, found := readSelection(inst.frameSig)
	if !found || row < 0 || row >= target.NumRows() {
		inst.graph.setSignalRawFrom(signalSelection, "0", signalWriterClamp)
	}
}

// resolveRunSignals resolves a Run buffer's unbound param slots against the
// frame's signal snapshot (slice 5a): a fresh parse (the debounced caches may
// lag the buffer) yields the slot list and the SET-bound names; unbound names
// with a store value become URL params. Also returns the bound-name set (the
// prelude constants — D1: a SET pins, so those names never consult the
// store) and the unfilled names (referenced, neither bound nor held — 5e's
// Run gate refuses on those). On a parse failure — the raw-fallback Run path
// — nothing resolves, nothing reports unfilled, and the server reports the
// real problem, exactly as for the SQL itself.
func (inst *PlayApp) resolveRunSignals(sql string) (sigParams map[string]string, bound map[string]bool, unfilled []string) {
	slots, vals, err := extractSlotsAndParams(sql)
	if err != nil {
		return
	}
	bound = make(map[string]bool, len(vals))
	for urlKey := range vals {
		bound[strings.TrimPrefix(urlKey, "param_")] = true
	}
	names := make([]string, 0, len(slots))
	for _, s := range slots {
		names = append(names, s.Name)
	}
	// resolveSignalNamesWithDefaults applies the reserved-String empty default
	// (e.g. selection_country before the first World click) so such a query
	// runs from the first frame instead of blocking as unfilled; a later panel
	// write resolves via the store and takes precedence. runSignalsDiverged
	// resolves through the same helper so the shipped params and the staleness
	// check agree (otherwise the default reads as perpetual divergence).
	sigParams = resolveSignalNamesWithDefaults(names, bound, inst.frameSig)
	// A SQL-valued slot is filled by its own `-- play: expr` line in the text
	// that ships, and applyExprSplice substitutes it before the body reaches
	// the wire — so the gate asks the shipped buffer, exactly as it asks it for
	// the SET prelude, rather than reading pane state (ADR-0187
	// §SD3/§SD4).
	exprValues := scanExprHints(sql)
	for _, s := range slots {
		if bound[s.Name] {
			continue
		}
		if exprCategoryFor(s.Type).spliced() {
			// An expression is never a URL parameter, filled or not: the
			// splice is its only route into the query, and leaving it in
			// sigParams would ALSO ship the predicate as a `param_*` string
			// that nothing reads. Dropped before the resolved-signal test
			// below, which would otherwise accept it and skip this.
			delete(sigParams, "param_"+s.Name)
			if v, declared := exprValues[s.Name]; declared && v != "" {
				continue
			}
			if raw, held := inst.signalRawFor(s.Name); held && raw != "" {
				continue
			}
			unfilled = append(unfilled, s.Name)
			continue
		}
		if _, resolved := sigParams["param_"+s.Name]; resolved {
			continue
		}
		unfilled = append(unfilled, s.Name)
	}
	return
}

// restoreHistoryEntry restores a past run: the buffer, plus the signal values
// the run shipped seeded back into the store (slice-5 D4), so re-running
// reproduces the same inputs. A SET-bound name still shadows a seeded signal
// at execution (D1).
func (inst *PlayApp) restoreHistoryEntry(entry HistoryEntry) {
	// Buffer is set only when the run shipped less than the buffer — a
	// multi-statement buffer under run-under-cursor (ADR-0130 L3). Restoring
	// it puts the siblings back rather than silently discarding them.
	inst.sql = entry.SQL
	if entry.Buffer != "" {
		inst.sql = entry.Buffer
	}
	for urlKey, raw := range entry.SigParams {
		inst.graph.setSignalRawFrom(strings.TrimPrefix(urlKey, "param_"), raw, signalWriterHistory)
	}
}

// playShotSvgPath returns the SVG sibling path for a screenshot PNG path.
func playShotSvgPath(pngPath string) string {
	return strings.TrimSuffix(pngPath, ".png") + ".svg"
}

// shotArtifactReady reports whether a capture artifact exists and is non-empty.
func shotArtifactReady(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// autoShotTick implements: first frame → kick auto-run; once results
// settle → request screenshot; wait for the PNG to land on disk → quit
// if asked. Driven by PlayApp.AutoRun / ScreenshotPath / ExitOnShot.
//
// The disk-stat gate in shotPhase 2 closes a race: c.RequestScreenshot
// only queues an egui::ViewportCommand::Screenshot, and the actual
// readback + PNG encode happens asynchronously across the next 1+
// frames (depending on GPU pipeline depth). If we sent
// ContextSendViewPortCommandClose immediately after RequestScreenshot,
// the Rust event loop could exit before handle_screenshot_event ever
// observed the Screenshot input event — eframe returned Ok() and the
// PNG never made it to disk. Polling os.Stat is the only timing-
// independent signal the Go side has that the Rust write_screenshot_png
// path actually completed.
func (inst *PlayApp) autoShotTick() {
	if inst.AutoRun && !inst.didAutoRun && inst.frame >= 3 {
		inst.didAutoRun = true
		inst.requestRun = true
	}
	if inst.ScreenshotPath == "" || inst.shotPhase == 3 {
		return
	}
	switch inst.shotPhase {
	case 0:
		// Wait until a query has completed — with results OR with an error
		// (a failed run has no record; scripted captures of the Diagnostics
		// tab's failure states still need the shot to fire).
		if inst.didAutoRun && !inst.graph.MainLoading() {
			rec, _, _, _, _, _, _, err, _ := inst.graph.MainSnapshot()
			if rec != nil {
				rec.Release()
			}
			if rec != nil || err != nil {
				inst.shotPhase = 1
				inst.shotSettle = inst.frame
			}
		}
	case 1:
		// Let layout settle for a few frames so the table is fully
		// laid out, AND wait for the canonical-form preview to
		// populate (updatePreview is debounced 300ms ≈ 18 frames at
		// 60fps after the SQL changes). Without the preview gate the
		// Preview tab captures its placeholder hint instead of the
		// syntax-highlighted SQL. A 60-frame ceiling guards against
		// the formatter never running (parse error already covered
		// by formattedErr != nil).
		previewReady := inst.formatted != "" || inst.formattedErr != nil
		// BOXER_PLAY_SHOT_SETTLE bumps the settle window so scripted
		// captures can wait out an async panel fetch (e.g. the Map tab's
		// debounced raster round-trip) before the screenshot fires.
		settleFrames := 5
		if n := ShotSettleFrames.Get(); n > 0 {
			settleFrames = int(n)
		}
		settled := inst.frame-inst.shotSettle >= settleFrames
		ceiling := inst.frame-inst.shotSettle >= settleFrames+60
		if settled && (previewReady || ceiling) {
			c.RequestScreenshot(inst.ScreenshotPath)
			// Also dump an SVG alongside the PNG: the headless render host
			// can't do PNG framebuffer readback, but its SVG visitor captures
			// the frame — including painter-drawn textures like the Map raster
			// overlay — so scripted captures work headless (ADR-0057 tour idiom).
			c.ExportSvg(playShotSvgPath(inst.ScreenshotPath), true, 0x1e1e1eff)
			inst.shotPhase = 2
		}
	case 2:
		// Done once either artifact lands: the windowed path writes the PNG;
		// the headless host writes only the SVG. Stat-gating closes the async
		// readback/encode race described in the docstring.
		if shotArtifactReady(inst.ScreenshotPath) || shotArtifactReady(playShotSvgPath(inst.ScreenshotPath)) {
			inst.shotPhase = 3
			if inst.ExitOnShot {
				c.ContextSendViewPortCommandClose()
			}
		}
	}
}

// renderTopBar is the pinned controls row at the window top: Run/Cancel
// with the loading spinner, Load .sql Powerbox button (only when the
// runtime wired a bus client), and the ClickHouse connection label.
// History/Detail/Projection visibility lives in the DockArea tab bar,
// so the legacy toggle buttons are gone.
func (inst *PlayApp) renderTopBar(schema *arrow.Schema) {
	ids := inst.ids
	for range c.Horizontal().KeepIter() {
		if inst.graph.MainLoading() {
			c.Spinner().Size(16).Send()
			if c.Button(ids.PrepareStr("cancel"), c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.graph.CancelMain()
			}
			// The bar rides beside Cancel so an in-flight run is legible
			// from any tab, with or without a result already on screen.
			inst.renderTopBarProgress()
		} else {
			// The two keyboard gestures have no chrome of their own — the
			// button they duplicate is where anyone would look for them.
			for range c.HoverText("Ctrl+Enter runs this. Ctrl+Shift+Enter runs just the query the caret is in — a subquery, a CTE body, or one statement of several — with the enclosing WITH items carried along.").KeepIter() {
				if c.Button(ids.PrepareStr("run"), c.Atoms().Text("Run").Keep()).
					SendResp().HasPrimaryClicked() {
					inst.requestRun = true
				}
			}
			// Run subquery: the mouse path for Ctrl+Shift+Enter, offered
			// beside the Subquery mode that explains what it would ship.
			//
			// Always enabled, and exactly the chord's action — degrade
			// included, so a caret at statement level runs the whole query and
			// the status line says so. Greying it out when there is nothing to
			// narrow to would be the more informative shape, but egui shows no
			// hover text on a disabled widget, so it could not say why it was
			// grey; a button that quietly means something else is worse than
			// one that does what its twin keystroke does, always.
			if inst.subqueryMode {
				for range c.HoverText("Runs just the query the caret is in, with the WITH items and SET prelude it needs carried along — the tinted region in the editor. Same as Ctrl+Shift+Enter. With the caret at statement level there is nothing narrower, and this runs the whole query.").KeepIter() {
					if c.Button(ids.PrepareStr("runSubquery"), c.Atoms().Text("Run subquery").Keep()).
						SendResp().HasPrimaryClicked() {
						inst.requestRun = true
						inst.requestSubquery = true
					}
				}
			}
		}

		// Definition — the ADR-0132 §SD3 escape hatch, widened: the document
		// the applet is defined BY rather than only the SQL it runs. Offered
		// whenever an embedder handed one over, with no bus needed — reading
		// the definition reaches nothing outside the process. The per-fence
		// Copy inside the drawer is what the old "Copy SQL" button became,
		// and it is the part that still needs the clipboard cap.
		if inst.definition != nil {
			c.Separator().Vertical().Send()
			for range c.HoverText("The markdown document this applet is defined by: the frontmatter that is its manifest, the prose, and the SQL it runs.").KeepIter() {
				if c.Button(ids.PrepareStr("definition"), c.Atoms().Text("Definition").Keep()).
					Selected(inst.definition.open).
					SendResp().HasPrimaryClicked() {
					inst.definition.open = !inst.definition.open
				}
			}
		}

		if inst.toolbarMinimal && inst.bus != nil {
			c.Separator().Vertical().Send()

			// Open in Playground — the recorded §SD3 escape-hatch upgrade
			// (ADR-0135 §SD7): compose the buffer + run flags into a
			// PlayLaunch and ask the window host for a full play window.
			// Offered whenever a bus is wired — the honest gate: a missing
			// cap or absent open service surfaces as the refusal label
			// below instead of a silently hidden button. Off the frame
			// goroutine, the clipboard rule (see renderDefinitionDoc).
			inst.openPlayMu.Lock()
			openBusy := inst.openPlayBusy
			openErr := inst.openPlayErr
			inst.openPlayMu.Unlock()
			if openBusy {
				c.Label("Opening…").Send()
			} else if c.Button(ids.PrepareStr("openPlayground"),
				c.Atoms().Text("Open in Playground").Keep()).
				SendResp().HasPrimaryClicked() {
				// Carry the endpoint so a buffer authored against the
				// in-process introspection /query endpoint reopens there
				// (bare keelson('…') is that endpoint's dialect); the
				// env-default target stays default. Same probe the
				// Save-as-applet button uses (appletstore.ComposeAppletDoc
				// stamps the frontmatter from it). Classified off the frame
				// goroutine — see runsOnIntrospection.
				sql, autoRun, live, bands := inst.sql, inst.AutoRun, inst.liveMain, inst.timelineBandsSql
				go func() {
					endpoint := ""
					if inst.runsOnIntrospection(sql) {
						endpoint = launchcfg.EndpointIntrospection
					}
					inst.requestOpenPlayground(launchcfg.PlayLaunch{
						At:       time.Now().UTC(),
						Sql:      sql,
						AutoRun:  autoRun,
						Live:     live,
						BandsSql: bands,
						Endpoint: endpoint,
					})
				}()
			}
			if openErr != "" {
				for rt := range c.RichTextLabel("Open failed: " + openErr) {
					rt.Small().Weak()
				}
			}
		}

		// Load .sql via fs Powerbox — only when the runtime wired a
		// bus client. The picker overlay lives at the host level
		// (carousel renders pickerbridge between Frame and metrics);
		// this button only kicks the fs.dialog.read request that puts
		// a pending entry on the broker's queue.
		var pickErr string
		if inst.bus != nil && !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			inst.pickMu.Lock()
			busy := inst.pickInFlight
			pickErr = inst.pickErr
			inst.pickMu.Unlock()
			if busy {
				c.Label("Loading…").Send()
			} else {
				if c.Button(ids.PrepareStr("loadSql"),
					c.Atoms().Text("Load .sql…").Keep()).
					SendResp().HasPrimaryClicked() {
					go inst.loadFromPicker()
				}
			}
			if pickErr != "" {
				c.Separator().Vertical().Send()
				for rt := range c.RichTextLabel("Load failed: " + pickErr) {
					rt.Small().Weak()
				}
			}
		}

		// Save as applet (ADR-0132 "O4" / ADR-0135 §SD7) — opens the
		// standalone authoring window (apps/sqlappletcreator) over
		// windowhost.open, seeded with the current buffer; the slug/title/icon
		// form that used to live inline here moved out with the O4 seam.
		// Authoring surface only: attenuated windows are consumers, and the
		// launch needs the bus. Off the frame goroutine, the clipboard rule.
		if inst.bus != nil && !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			inst.saveAppletMu.Lock()
			saveBusy := inst.saveAppletBusy
			saveErr := inst.saveAppletErr
			inst.saveAppletMu.Unlock()
			if saveBusy {
				c.Label("Opening creator…").Send()
			} else if c.Button(ids.PrepareStr("saveApplet"),
				c.Atoms().Text("Save as applet…").Keep()).
				SendResp().HasPrimaryClicked() {
				// Carry the endpoint so a buffer authored against the
				// in-process introspection endpoint composes with the right
				// frontmatter; the env-default target stays default. Same
				// probe as the Open in Playground button; classified off the
				// frame goroutine — see runsOnIntrospection.
				sql := inst.sql
				go func() {
					endpoint := ""
					if inst.runsOnIntrospection(sql) {
						endpoint = appletcreatecfg.EndpointIntrospection
					}
					inst.requestSaveApplet(appletcreatecfg.AppletCreate{
						At:       time.Now().UTC(),
						Sql:      sql,
						Endpoint: endpoint,
					})
				}()
			}
			if saveErr != "" {
				for rt := range c.RichTextLabel("Save as applet failed: " + saveErr) {
					rt.Small().Weak()
				}
			}
		}

		if inst.client != nil && !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			inst.renderEndpointSwitcher()
		}

		// The Panes menu (the 2026-07-27 Update): the strip's marks with room
		// to explain themselves. Not offered under toolbarMinimal, where an
		// applet's pane set is the author's decision rather than the reader's
		// to navigate.
		if !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			inst.renderPanesMenu(schema)
		}

		// Hide-prelude toggle (visible only when there's at least one
		// param slot — no point in offering it for queries with no
		// placeholders, nor under toolbarMinimal, where no editor shows
		// a prelude to hide). Mutates the canonical state on the next
		// frame; the editor binding flips at the start of the next
		// renderEditorTab.
		if len(inst.paramSlots) > 0 && !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			c.Checkbox(ids.PrepareStr("hidePrelude"), inst.paramHidePrelude, "Hide prelude").
				SendRespVal(&inst.paramHidePrelude)
		}

		// The `main` live toggle (slice 5e, D2): offered when the buffer has
		// at least one signal input to react to — or while already on, so it
		// can be unchecked after an edit removes the last unbound slot.
		if inst.liveMain || inst.hasUnboundSlots() {
			c.Separator().Vertical().Send()
			c.Checkbox(ids.PrepareStr("liveMain"), inst.liveMain, "Live").
				SendRespVal(&inst.liveMain)
		}

		// The conditions toggle (ADR-0121), off by default: it rewrites an
		// information-retrieval query so each condition of its WHERE becomes a
		// result column. Offered only where a rewrite could happen — the pass
		// needs the schema probe installLeewayNameResolution builds — and it
		// pushes to the Client, which owns the flag the query path reads.
		if inst.client != nil && inst.client.conditionsPass.Apply != nil && !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			c.Checkbox(ids.PrepareStr("exposeConditions"), inst.exposeConditions, "Conditions").
				SendRespVal(&inst.exposeConditions)
			// Pushed unconditionally rather than on an observed change:
			// SendRespVal does not write the field synchronously, so comparing
			// it against a value read moments earlier never sees the flip. The
			// store is a plain atomic; paying it per frame is cheaper than a
			// timing assumption about when the response lands.
			inst.client.SetExposeConditions(inst.exposeConditions)
		}

		// Subquery mode: a display toggle for the run-subquery gesture, off by
		// default. The gesture works either way — what this turns on is the
		// editor saying, before you press it, which query would run and what
		// travels with it. Offered only where there is an editor to decorate.
		if inst.editorTabPresent() && !inst.toolbarMinimal {
			c.Separator().Vertical().Send()
			for range c.HoverText("Marks the query the caret is in: its extent, the WITH items and SET prelude it closes over, and any reference to an outer table alias that would not resolve if it ran alone. The gutter's | says when running it alone would differ from Run. Adds a Run subquery button; changes nothing about what Run or Ctrl+Enter do.").KeepIter() {
				c.Checkbox(ids.PrepareStr("subqueryMode"), inst.subqueryMode, "Subquery").
					SendRespVal(&inst.subqueryMode)
			}
		}

		// Unfilled inputs (D3): the buffer references a name nothing fills —
		// a Run would be refused, so say what to do while still typing.
		if unfilled := inst.unfilledInputs(); len(unfilled) > 0 {
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabel("unfilled: {" + strings.Join(unfilled, "}, {") +
				"} — fill it in PARAMETERS, or SET param_<name> = …") {
				rt.Small().Weak()
			}
		}
	}
}

// renderEndpointSwitcher is the toolbar control for the query target. It shows
// where queries go, read-only, beside a fixed-label "Endpoint" menu (a
// dynamic MenuButton label would shift its derived id and drop menu state). The
// menu offers the Auto preset, a manual URL, and two fixed presets: the
// in-process keelson introspection /query endpoint (shown only when a
// co-resident host published one via introspecthost.Start →
// introspect.LocalQueryEndpoint, ADR-0094 §SD6) and the external server
// ("External"). Every widget uses an explicit stable id, so conditionally
// showing the keelson preset never drifts the others' ids.
//
// Auto installs the keelson-aware resolver (ADR-0141) — a read naming only
// keelson tables goes to the introspection plane on its own. While it is on,
// the label reports the last run's resolved target and the reason for it,
// rather than the pinned base, since the base is no longer where queries
// necessarily went. It reports the *last* decision rather than resolving the
// current buffer because resolving runs the client-side rewrites, which can
// reach the network — not something to do per frame on the render thread.
//
// The menu names the pinned base unconditionally, and marks whichever preset
// the base currently is. Under Auto the toolbar label stops reporting the
// base, so the menu is the only place left to read it — and a query that
// names neither a keelson table nor a plain one (`system.*` only: those
// resolve on either engine, so they carry no placement signal) stays on the
// base without saying which engine's `system` it read.
func (inst *PlayApp) renderEndpointSwitcher() {
	ids := inst.ids
	// Installed unconditionally rather than on an observed change, for the
	// reason the Conditions toggle records: SendRespVal writes the bound
	// field a frame late, so comparing it against a value read moments
	// earlier never sees the flip.
	if inst.autoEndpoint {
		inst.client.SetResolver(autoResolver)
	} else {
		inst.client.SetResolver(nil)
	}

	base := inst.client.URL()
	label := fmt.Sprintf("%s  as %s", truncateRunes(base, 40), inst.client.cfg.User)
	full := fmt.Sprintf("%s  as %s", base, inst.client.cfg.User)
	if inst.autoEndpoint {
		if dec, ok := inst.client.LastDecision(); ok {
			// No arrow glyph: the host font has no →, and it renders as tofu.
			label = "auto: " + truncateRunes(dec.describe(), 72)
			full = "auto — last run went to " + dec.describe() +
				"\npinned base: " + base
		} else {
			// Name the base even before the first run: under Auto it is
			// still where anything that names no keelson table will go.
			label = "auto (" + truncateRunes(base, 40) + ") — no query run yet"
			full = "auto — nothing has run yet; a query naming no keelson " +
				"table goes to the pinned base: " + base
		}
	}
	// The label is truncated twice over (runes here, pixels by Truncate), and
	// a host:port that differs only in its tail is exactly the case where
	// that hurts. Hover carries the untruncated text.
	for range c.HoverText(full).KeepIter() {
		c.Label(label).Truncate().Send()
	}

	local := introspect.LocalQueryEndpoint()
	for range c.MenuButton(c.Atoms().Text("Endpoint").Keep()).KeepIter() {
		for rt := range c.RichTextLabel("pinned: " + base) {
			rt.Small().Weak()
		}
		c.Checkbox(ids.PrepareStr("endpointAuto"), inst.autoEndpoint,
			"Auto — send keelson-only reads to introspection").
			SendRespVal(&inst.autoEndpoint)
		c.Separator().Send()
		c.TextEdit(ids.PrepareStr("endpointDraft"), inst.endpointDraft, false).
			SendRespVal(&inst.endpointDraft)
		if c.Button(ids.PrepareStr("endpointApply"), c.Atoms().Text("Apply").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.setEndpoint(strings.TrimSpace(inst.endpointDraft))
		}
		c.Separator().Send()
		// Presets carry the URL they would pin and mark the one already
		// pinned, so "which of these am I on?" is answerable without
		// clicking one to find out.
		if local != "" {
			if c.Button(ids.PrepareStr("endpointKeelson"),
				c.Atoms().Text("Keelson introspection — "+local).Keep()).
				Selected(base == local).
				SendResp().HasPrimaryClicked() {
				inst.setEndpoint(local)
			}
		}
		// Offered only when there is something to reset to: SetURL ignores an
		// empty target, so a button that could only no-op is worse than an
		// absent one — it reads as "reset is broken".
		if inst.externalURL != "" {
			if c.Button(ids.PrepareStr("endpointExternal"),
				c.Atoms().Text("External (reset) — "+inst.externalURL).Keep()).
				Selected(base == inst.externalURL).
				SendResp().HasPrimaryClicked() {
				inst.setEndpoint(inst.externalURL)
			}
		}
		c.Separator().Send()
		// Switching endpoint already drops the schema caches; this is the
		// same endpoint changing under us, which nothing here can observe.
		// It lives in this menu because that is where the other cause of a
		// stale schema is handled, and it does not touch the pin — so, unlike
		// the presets, it leaves Auto alone.
		for range c.HoverText("Re-probe system.columns for every table the next query names. " +
			"Needed after a table or view changed on this endpoint — a new column, " +
			"a recreated view — which play cannot see happen.").KeepIter() {
			if c.Button(ids.PrepareStr("endpointReloadSchema"),
				c.Atoms().Text("Reload schema").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.client.ReloadSchemas()
			}
		}
	}
}

// setEndpoint repoints the client and syncs the draft TextEdit, telling the
// frontend to drop its cached buffer so the new URL shows (the "Stubborn Text"
// override — a programmatic write to an interactive-widget binding). Pinning
// an endpoint by hand is an instruction, so it also turns Auto off.
//
// An empty target is refused whole rather than half-applied: Client.SetURL
// ignores it, and turning Auto off around a pin that did not happen leaves the
// switcher claiming a state nothing established.
func (inst *PlayApp) setEndpoint(u string) {
	if u == "" {
		return
	}
	inst.client.SetURL(u)
	inst.endpointDraft = u
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.endpointDraft)
	inst.clearAutoEndpoint()
}

// clearAutoEndpoint turns Auto off from Go, overriding the checkbox's cached
// frontend state so the box actually clears.
func (inst *PlayApp) clearAutoEndpoint() {
	if !inst.autoEndpoint {
		return
	}
	inst.autoEndpoint = false
	c.CurrentApplicationState.StateManager.OverrideDatabindingBPtr(&inst.autoEndpoint)
}

// renderEditorTab is the Editor dock tab body: multi-line SQL editor
// followed by the SQL function affordances. The syntax-highlighted
// canonical form lives in its own Preview tab (split to the right by
// default); the toolbar lives in the topbar.
//
// The TextEdit's desired_rows is computed from the height the editor
// measured for its own pane last frame (sqleditor's seq-keyed R21 probe)
// so the editor fills the dock pane vertically. egui's TextEdit otherwise
// allocates a fixed desired_rows × row_height and leaves the rest of the
// pane blank. First frame falls back to editorDesiredRows; the editor's
// own internal scroll handles content overflow.
func (inst *PlayApp) renderEditorTab() {
	// The reserve covers chrome below the editor: the TextEdit's own bottom
	// margin always, plus room for the affordances block when at least one
	// observation was captured by the most recent updatePreview run. The
	// parameter block renders ABOVE the editor and is deliberately absent from
	// this reserve: PaneHeight is measured where the editor itself starts, so
	// the param block's height is already out of it.
	const editorBaseReservePx float32 = 8.0
	const editorAffordanceReservePx float32 = 120.0

	// Both numbers come from the editor, which measures its own pane and its
	// own row height. NOT CaptureAvailableSize (r18): that register is one
	// process-wide slot won by the frame's last capture, so with a Detail or
	// Distribution pane on screen the editor was sized by THAT pane. And not a
	// row-height constant either — the host's monospace metrics are not ours to
	// guess, and the guess is what decides whether the pane is filled.
	rows := uint32(editorDesiredRows)
	availH, rowPx := inst.editor.PaneHeight(), inst.editor.RowHeight()
	if availH > 0 && rowPx > 0 {
		reserve := editorBaseReservePx
		if len(inst.observations) > 0 {
			reserve += editorAffordanceReservePx
		}
		usable := availH - reserve
		if usable > 0 {
			if r := uint32(usable / rowPx); r > rows {
				rows = r
			}
		}
	}

	for range c.Vertical().KeepIter() {
		// Param-slot widgets render above the editor; they author the
		// leading SET prelude. Rendered first so the editor below claims
		// the remaining vertical space.
		inst.renderParamSlots()

		// Editor binding. Default mode keeps the leading SET prelude
		// inside the main editor; hide-prelude mode slices the prelude
		// off, binds the editor to the residual-only mirror, and
		// recomposes inst.sql when the residual mirror diverges. The
		// prelude itself is re-rendered as a small read-only label
		// (and the widget section above stays authoritative for
		// editing values).
		inst.renderSqlEditor(rows)

		// SQL function affordances (regex testers etc.) for call sites the
		// affordanceEval observed during updatePreview.
		inst.renderAffordances()
	}
}

// consumePendingSnippet applies the snippet-library delivery ops staged since
// the last frame (InsertSqlAtCaret / ReplaceSql, play_delivery.go) and returns
// the insert text handed to whichever editor renders this frame (empty when
// none, or when superseded). Replace swaps the whole buffer here — before the
// mode branch — so it works in both: non-hide binds inst.sql directly, and
// hide-mode recomposeMirror re-derives the residual from the new canonical. A
// same-frame Replace supersedes an Insert. Both pendings are cleared eagerly,
// so each click applies exactly once.
func (inst *PlayApp) consumePendingSnippet() (insert string) {
	insert = inst.pendingSnippetInsert
	inst.pendingSnippetInsert = ""
	if replace := inst.pendingSnippetReplace; replace != "" {
		inst.pendingSnippetReplace = ""
		inst.sql = replace
		// A whole-buffer swap is a new buffer, so its prelude is the new
		// default Reset restores to. An insert is not: it edits the buffer the
		// reader already has.
		inst.captureParamDefaults(replace)
		insert = ""
	}
	return
}

// renderSqlEditor binds the sqleditor widget and the show/hide
// parameter-prelude toggle. Default mode binds the editor to inst.sql
// verbatim — the user sees and can hand-edit the SET prelude. Hide mode
// delegates the canonical/mirror state machine to recomposeMirror (see
// play_param_inject.go) and renders the sliced-off prelude as a read-only
// label above the residual editor.
//
// The binding is resolved BEFORE Bind, not after. The caret arrives in the
// coordinates of whichever buffer rendered last frame, so binding first is
// what lets the widget resolve it against that same buffer and lift it into
// inst.sql coordinates in one step. (The pre-extraction code resolved twice in
// hide mode — once against the canonical buffer, which was short by the elided
// prelude, and again against the mirror after the overlays had already been
// derived from the first answer. The ordering here is what retires that.)
func (inst *PlayApp) renderSqlEditor(rows uint32) {
	const mainHint = "-- type SQL, press Run"
	pending := inst.consumePendingSnippet()

	f := sqleditor.Frame{
		IDSlot:  "sqlEditor",
		Value:   &inst.sql,
		Hint:    mainHint,
		Rows:    rows,
		Insert:  pending,
		Density: inst.density,
		// Tab is asked for only while last frame's answer had something to
		// complete, so it means a tab character the rest of the time
		// (ADR-0190 §SD10). Last frame's, because the flag is part of the
		// frame this Bind is about to resolve.
		CaptureTab: inst.completionWantsTab(),
	}
	var prelude string
	if inst.paramHidePrelude {
		// A parse we cannot make sense of falls through to the unsliced
		// editor so the user can fix the syntax — don't try to slice a
		// buffer we don't understand.
		if pre := recomposeMirror(inst.sql, inst.paramSqlEdit, inst.paramSqlEditSyncedFrom); pre.OK {
			inst.sql = pre.Canonical
			inst.paramSqlEdit = pre.Mirror
			inst.paramSqlEditSyncedFrom = pre.SyncedFrom
			prelude = pre.Prelude
			// recomposeMirror guarantees Canonical == Prelude+Mirror, so the
			// mirror is a suffix view: the widget rebases the overlays onto it
			// by the elided prelude's length rather than dropping them.
			f.IDSlot = "sqlEditorResidual"
			f.Value = &inst.paramSqlEdit
			f.Offset = len(pre.Prelude)
			f.Canonical = pre.Canonical
			f.Hint = "-- type SQL (prelude hidden)"
		}
	}

	res := inst.editor.Bind(f)
	// One caret per frame, in inst.sql coordinates, for the producers below
	// and for everything outside this render that reads it.
	inst.caretByte = res.Caret
	// The completion answer is derived here rather than in the tab body, for
	// the same reason: the editor's resolved-token tint below reads it, and a
	// tab that is closed must not change what the editor says (ADR-0190 §SD9).
	inst.refreshCompletion(res)
	// A captured Tab arrives one frame after the press, like the caret. What
	// it inserts goes through the same pending-insert seam the pane's click
	// and the Snippets tab use.
	if res.TabPressed && !res.TabShift {
		inst.applyTabCompletion()
	}

	if prelude != "" {
		for rt := range c.RichTextLabel(strings.TrimRight(prelude, "\n")) {
			rt.Small().Weak().Monospace()
		}
	}
	// ADR-0130 L3 overlays, in inst.sql coordinates. Composed after Bind
	// because every one of them reads the caret the Bind just published; the
	// statement tint is absent because the widget emits that itself.
	inst.editor.Render(inst.ids, sqleditor.Decoration{
		Styled: inst.editorStyledSections(),
		// The subquery mark travels beside the sections rather than inside
		// them: it is drawn whether or not the Subquery toggle produced any.
		SubqueryMark: inst.caretSubqueryRange(),
	})
}

// renderPreviewTab is the Preview dock tab body: the canonical-form
// SQL rendered as a syntax-highlighted CodeView. The pipeline itself
// runs once per frame from Render() (debounced via previewDebounce),
// so this helper just renders the latest cached output.
func (inst *PlayApp) renderPreviewTab() {
	ids := inst.ids
	// The toggle renders unconditionally so the pane never reflows around
	// it; the client-nil guard (tests, legacy CLI) only disables the wire
	// view, not the checkbox row.
	c.Checkbox(ids.PrepareStr("previewAsSent"), inst.previewAsSent, "As sent to server").
		SendRespVal(&inst.previewAsSent)
	if inst.previewAsSent {
		inst.renderWirePreview()
		return
	}
	switch {
	case inst.formattedErr != nil:
		// Pointer only — the Diagnostics tab owns the parse advice (whether
		// this is a boxer grammar gap or genuinely broken SQL, per the
		// EXPLAIN probe) and the full error texts.
		for rt := range c.RichTextLabel("No canonical form — boxer's grammar does not parse this statement; see the Diagnostics tab. Run sends the buffer verbatim.") {
			rt.Small().Weak()
		}
	case inst.formatted != "":
		// PrepareSql: this body runs every frame the Preview pane is open, and
		// the formatted SQL only changes when the editor does (ADR-0125).
		c.CodeView(ids.PrepareStr("sqlPreview"),
			codeview.PrepareSql(inst.formatted)).
			Wrap().
			Send()
	default:
		for rt := range c.RichTextLabel("Type SQL in the Editor tab to see its canonical form here.") {
			rt.Small().Weak()
		}
	}
}

// renderWirePreview is the "as sent" body of the Preview tab: the exact
// statement BuildStatement ships (ADR-0108 §SD6) — pre-execute passes
// applied, FORMAT rewritten — plus a caption naming the params that ride
// the URL instead of the body. Unlike the canonical view it renders even
// for SQL outside Grammar1, because that is what would be POSTed.
func (inst *PlayApp) renderWirePreview() {
	ids := inst.ids
	switch {
	case inst.client == nil:
		for rt := range c.RichTextLabel("No client in this session — the wire form is unavailable.") {
			rt.Small().Weak()
		}
	case inst.wireBody == "":
		for rt := range c.RichTextLabel("Type SQL in the Editor tab to see the statement as shipped.") {
			rt.Small().Weak()
		}
	default:
		// The engine split, where "as sent" is the pane's whole claim
		// (ADR-0163 §SD5). A buffer holding a client call ships text that
		// mentions it — ClickHouse ignores an unused CTE — but the call
		// itself is never executed there, and a pane titled "as sent" must
		// not let that read as the server having done the analysis.
		for _, n := range inst.currentSplit.Nodes {
			if n.Client == nil {
				continue
			}
			for rt := range c.RichTextLabel("`" + string(n.ID) + "`: " + clientNodeCaption(n.Client)) {
				rt.Small().Weak()
			}
		}
		// Multi-statement buffers ship one statement (ADR-0130 L3); say which,
		// so the body below is never mistaken for the whole buffer.
		if inst.wireStmtTotal > 1 {
			for rt := range c.RichTextLabel(fmt.Sprintf(
				"statement %d of %d — the caret's statement ships, with the SET prelude",
				inst.wireStmtNumber, inst.wireStmtTotal)) {
				rt.Small().Weak()
			}
		}
		if len(inst.wireParams) > 0 {
			names := make([]string, 0, len(inst.wireParams))
			for k := range inst.wireParams {
				names = append(names, k)
			}
			sort.Strings(names)
			for rt := range c.RichTextLabel("params on URL: " + strings.Join(names, ", ")) {
				rt.Small().Weak()
			}
		}
		if len(inst.wireSignals) > 0 {
			// Signal values the store would supply at Run for the SHIPPED
			// statement's unbound slots (slice 5a) — name=value, truncated.
			pairs := make([]string, 0, len(inst.wireSignals))
			for k, v := range inst.wireSignals {
				pairs = append(pairs, k+"="+truncateRunes(v, 24))
			}
			sort.Strings(pairs)
			for rt := range c.RichTextLabel("signals on URL: " + strings.Join(pairs, ", ")) {
				rt.Small().Weak()
			}
		}
		// PrepareSql: per-frame body; the wire text changes only with the
		// editor or the signal set (ADR-0125).
		c.CodeView(ids.PrepareStr("sqlWire"),
			codeview.PrepareSql(inst.wireBody)).
			Wrap().
			Send()
	}
}

// updatePreview runs the nanopass formatting pipeline on inst.sql when the
// buffer has been idle for previewDebounce. No-op if nothing changed or the
// debounce window hasn't elapsed yet.
func (inst *PlayApp) updatePreview() {
	if inst.sql != inst.lastSeenSql {
		inst.lastSeenSql = inst.sql
		inst.lastEditAt = time.Now()
	}
	inst.updateWirePreview()
	if inst.sql == inst.formattedFor {
		return
	}
	if time.Since(inst.lastEditAt) < previewDebounce {
		return
	}
	inst.formattedFor = inst.sql
	// Reset observations: the slice is populated by affordanceEval's
	// OnObservation callback during the pipeline run below. Whatever was
	// there is for the previous SQL.
	inst.observations = inst.observations[:0]
	// Parse the UNTRIMMED buffer. Every byte range this pipeline records —
	// observation Src (renderAffordances / extractCallArgs) and param-slot
	// Src (ADR-0124 §SD1, and the ADR-0130 L3 span consumers) — is sliced
	// against inst.sql by its consumers, so trimming here would skew them
	// by exactly the leading whitespace. The lexer skips leading whitespace,
	// so parsing the untrimmed buffer is otherwise a no-op, and
	// CanonicalizeWhitespace trims the canonical output itself. Emptiness is
	// still judged on a trimmed copy.
	raw := inst.sql
	if strings.TrimSpace(raw) == "" {
		inst.formatted = ""
		inst.formattedErr = nil
		inst.refreshParamSlotsFromParse(nil, nil)
		inst.diag.noteParse("", nil)
		return
	}
	// Param-slot extraction runs unconditionally on the raw buffer:
	// failures here only suppress widget rendering for the broken
	// frame, never the canonical-form preview itself. One parse
	// (extractSlotsAndParams) covers both the slot list and the
	// SET-prelude value cache.
	if slots, vals, slotErr := extractSlotsAndParams(raw); slotErr == nil {
		inst.refreshParamSlotsFromParse(slots, vals)
	}
	// Reparse first so syntax errors surface with line/column info —
	// the Sequence's error drops position because its internal listener
	// does not capture it. A failure arms the Diagnostics EXPLAIN probe
	// (a success disarms it) — the server-side classification of buffers
	// boxer's grammar cannot model.
	if err := formatSyntaxError(raw); err != nil {
		inst.formatted = ""
		inst.formattedErr = err
		inst.diag.noteParse(raw, err)
		return
	}
	// affordanceEval is analytical: its handlers return discard ControlFlow,
	// so the runner forwards `raw` to the canonicalisers unchanged. The
	// side effect is OnObservation firing per detected call site.
	out, err := nanopass.Sequence("sqlPreview",
		inst.affordanceEval.Pass(),
		passes.StripComments,
		passes.CanonicalizeKeywordCase,
		passes.CanonicalizeWhitespace,
		passes.RemoveRedundantParens,
	).Run(raw)
	// Lift the observations from body space into buffer space. Pass.Run hands
	// each pass the body env.Extract split off — SET prelude removed, leading
	// whitespace trimmed — so a recorded range is relative to that suffix,
	// while every consumer (renderAffordances, extractCallArgs, and the
	// ADR-0130 L3 span producers) slices inst.sql. Runs on the error path too:
	// the analytical pass is first in the sequence, so it has already observed
	// by the time a later canonicaliser fails.
	shiftObservationsToBuffer(inst.observations, raw, env.BodyOffset(raw))
	if err != nil {
		inst.formatted = ""
		inst.formattedErr = err
		inst.diag.noteParse(raw, err)
		return
	}
	inst.formatted = out
	inst.formattedErr = nil
	inst.diag.noteParse(raw, nil)
}

// updateWirePreview keeps the "as sent" cache in sync with inst.sql on the
// same debounce as the canonical preview. Computed only while the toggle
// is on: BuildStatement re-parses per pass, and paying that per edit for a
// hidden view would be waste. Toggling the checkbox on picks the current
// buffer up on the next frame (wireFor is stale and the debounce window
// has long elapsed). The signal caption additionally refreshes when the
// store revision moves (a signal can change without a buffer edit), and the
// condition-columns toggle likewise rewrites the wire SQL without an edit
// (ADR-0121) — all three key the cache, or the view silently shows the
// previous query while a different one ships. Only a *buffer* change is
// debounced: a toggle is a deliberate act with nothing to settle.
//
// Run-under-cursor (ADR-0130 L3) adds the caret's statement number to the key:
// on a multi-statement buffer, moving the caret changes what Run ships without
// touching a single byte of the buffer, and a view whose whole job is to show
// what ships must not go stale behind that either.
func (inst *PlayApp) updateWirePreview() {
	if !inst.previewAsSent || inst.client == nil {
		return
	}
	sigRev := uint64(0)
	if inst.frameSig != nil {
		sigRev = inst.frameSig.Revision()
	}
	conds := inst.client.ExposeConditions()
	runSQL, number, total := inst.runBuffer()
	if inst.sql == inst.wireFor && sigRev == inst.wireSigRev && conds == inst.wireConditions &&
		number == inst.wireStmtNumber && total == inst.wireStmtTotal {
		return
	}
	if inst.sql != inst.wireFor && time.Since(inst.lastEditAt) < previewDebounce {
		return
	}
	inst.wireFor = inst.sql
	inst.wireSigRev = sigRev
	inst.wireConditions = conds
	inst.wireStmtNumber, inst.wireStmtTotal = number, total
	if runSQL == "" {
		inst.wireBody = ""
		inst.wireParams = nil
		inst.wireSignals = nil
		return
	}
	inst.wireBody, inst.wireParams = inst.client.BuildStatement(runSQL)
	// Signals follow the Run gate's scoping (executeRun): the caption says
	// "signals on URL", and since the gates judge what ships, the URL
	// carries only the narrowed text's unbound slots — a sibling
	// statement's signal on this caption would be a value no Run sends.
	// The cache key above already covers the inputs: the caret's statement
	// number keys the text, sigRev keys the store.
	inst.wireSignals, _, _ = inst.resolveRunSignals(runSQL)
}

// renderStatus is the bottom-bar status line. Per-frame snapshot values
// are passed in from Render() so this helper does not take its own
// Snapshot+Release — the frame already owns one retained reference.
// renderStatus draws the bottom status bar as the tethered query-result
// inspector summary: a severity-colored state badge + a stat line
// (rows/elapsed/age, or the empty/stale/error message) + an arrow-square-out
// toggle that pops out the bezier-tethered inspector window (state graph /
// history / provenance). The FSM is mirrored each frame in Render so the badge
// and summary agree.
func (inst *PlayApp) renderStatus(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error, truncation string) {
	inst.queryFSMWidget.
		Provenance(inspector.Provenance{
			Subject:   "app.play.query.result-state",
			SourceApp: "github.com/stergiotis/boxer/apps/play",
			SampledAt: executed,
		}).
		Summary(func() { inst.renderQuerySummary(numRows, elapsed, summary, executed, err, truncation) }).
		Render()
}

// renderHistoryTab is the History dock tab body. The tab title already
// labels the pane so the legacy heading and inner ScrollArea are gone;
// the outer ScrollArea wrap lives in Render().
func (inst *PlayApp) renderHistoryTab() {
	ids := inst.ids
	hist := inst.graph.MainHistory()
	// Newest first.
	for i := len(hist) - 1; i >= 0; i-- {
		entry := hist[i]
		label := historyLabel(entry)
		for range c.IdScope(ids.PrepareSeq(uint64(i))) {
			if c.Button(ids.PrepareStr("entry"),
				c.Atoms().Text(label).Keep()).
				Frame(false).
				Truncate().
				SendResp().HasPrimaryClicked() {
				inst.restoreHistoryEntry(entry)
			}
		}
	}
	// The durable half: captured runs from boxer.facts (ADR-0115 S2).
	inst.renderRecordedRuns()
	// Tier-1 pins: frozen resultsets on the endpoint (ADR-0115 S4).
	inst.renderPinnedResults()
}

// renderTableTab is the Table dock tab body: pager strip atop the master
// table, with a centred empty-state when there is no result yet. loading is
// the ACTIVE snapshot's flag (activeSnapshot), not MainLoading(): an observed
// intermediate loads on its own lane, and gating the spinner on the main lane
// showed "0 rows" during its first fetch (review finding). Same for the
// Projection/Timeline/Schema tabs below.
func (inst *PlayApp) renderTableTab(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, err error, executed time.Time, result ResultID) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		// No batch: distinguish "never ran" from "ran, returned nothing"
		// via the FSM (which uses the executed token) so an empty result
		// reads clearly instead of looking idle.
		if inst.queryFSM.Current() == queryStateIdle {
			inst.renderResultsEmpty()
		} else {
			inst.renderResultsZeroRows()
		}
		return
	}
	if numRows == 0 {
		inst.renderResultsZeroRows()
		return
	}
	// The pager tracks the result THIS tab renders (which since 6c may be a
	// bound node's, not the active one) — sync moved here from Render.
	if executed != inst.pagerSeenExecuted {
		inst.pagerSeenExecuted = executed
		inst.pager.Reset()
	}
	inst.pager.Configure(rec.NumRows())
	// Leeway display-mode bar: a collapsible toolbar of the three orthogonal
	// controls (row granularity, reveal support / membership columns). Shown only
	// for a leeway-shaped result — a non-leeway result has no structure to reshape
	// — so it also serves as the "this result is leeway" affordance.
	if inst.leewayColumnClasses(schema) != nil {
		inst.renderTableOptionsBar()
	}
	// Give the pager strip vertical breathing room off the tab bar and rule it
	// off from the grid, so the toolbar reads as its own band rather than being
	// jammed against the table's first header row.
	pad := styletokens.PaddingTight(inst.density)
	c.AddSpace(pad)
	inst.pager.Render()
	// Tier-1 pin affordance (ADR-0115 S4): freeze the rows this tab
	// shows into a queryable table.
	inst.renderPinControl(rec)
	// ADR-0186 raw toggle: bypass every gloss for the session — the escape
	// hatch a wrong rule needs. Offered only once a column is glossed.
	inst.renderGlossControl(schema)
	c.AddSpace(pad)
	c.Separator().Send()
	dispatchPanel(tablePanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("table"), rec: rec, schema: schema, sig: inst.frameSig, result: result},
	}, inst.sigEmit)
}

// renderProjectionTab is the Projection dock tab body: the UMAP scatter
// with its own toolbar/status. Same empty/error guards as the Table tab.
func (inst *PlayApp) renderProjectionTab(rec arrow.RecordBatch, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		inst.renderResultsEmpty()
		return
	}
	// The projector invalidates against the result THIS tab renders (which
	// since 6c may be a bound node's) — sync moved here from Render.
	inst.projector.Invalidate(rec.Schema(), executed)
	dispatchPanel(projectionPanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("projection"), rec: rec, schema: rec.Schema(), sig: inst.frameSig},
	}, inst.sigEmit)
}

// renderTimelineTab is the Timeline dock tab body: the calendar-axis
// interval/point/annotation widget driven by the strict `_tl_*` column
// contract. The Timeline is an ADR-0097 PanelI observer of the `main` node:
// this method runs the panel's Accept (the column-contract negotiation) and
// renders either its reject reason (+ the contract help, so the SQL author can
// debug from the panel) or, on a claim, the panel body. Same empty/error guards
// as the other result tabs.
func (inst *PlayApp) renderTimelineTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		// Timeline-specific empty state: pair the generic "run a query"
		// hint with the column contract so first-time users see what
		// shape of SELECT the panel expects without leaving the tab.
		for range c.Vertical().KeepIter() {
			c.Label("Run a query to see the timeline.").Send()
			c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
			inst.timeline.RenderContractHelp()
		}
		return
	}
	// Negotiate the events contract BEFORE demanding the bands node (SD2 at
	// the margin: a rejected timeline must not run the bands query).
	// resolveContract is the same pure, schema-only check AcceptForChannel
	// runs during dispatch below.
	if ct := resolveContract(schema); ct.Mode == timelineModeNone {
		inst.renderTimelineReject(ct.Reject)
		return
	}
	// Demand the bands node (its own lane) for the chBands channel; since 5d
	// it compiles against the frame snapshot — the events extent arrives as
	// the tl_min/tl_max signals the Timeline published, one frame behind
	// (absorbed by the fetch latency). Both nil (empty bands SQL / no result
	// yet) → chBands unfilled; a schema-only view (successful empty fetch)
	// still fills the channel so it maps to "0 bands" rather than "pending".
	bandsRec, bandsSchema := inst.timeline.demandBands(inst.frameSig)
	if bandsRec != nil {
		defer bandsRec.Release()
	}
	inputs := map[ChannelID]channelInput{
		chEvents: {node: inst.resolvedTabNode("timeline"), rec: rec, schema: schema, sig: inst.frameSig},
	}
	if bandsRec != nil || bandsSchema != nil {
		inputs[chBands] = channelInput{node: bandsNodeID, rec: bandsRec, schema: bandsSchema, sig: inst.frameSig}
	}
	if reject := dispatchPanel(timelinePanel{driver: inst.timeline}, inputs, inst.sigEmit); reject != "" {
		inst.renderTimelineReject(reject)
		return
	}
}

// renderTimelineReject shows a contract-reject reason + the contract help —
// the debug-in-panel affordance, shared by the pre-negotiation and dispatch
// reject paths.
func (inst *PlayApp) renderTimelineReject(reason string) {
	for range c.Vertical().KeepIter() {
		for rt := range c.RichTextLabel(reason) {
			rt.Strong()
		}
		c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
		inst.timeline.RenderContractHelp()
	}
}

// renderWorldTab is the World dock tab body (ADR-0114): the schematic world
// choropleth over the active result. The panel is a plain PanelI observer of
// the observed node — same guards as the Table tab, plus the executed
// timestamp handed to the driver as its extraction-cache key.
func (inst *PlayApp) renderWorldTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query with a country column (ISO code or name) to see the world map.") {
			rt.Small().Weak()
		}
		return
	}
	inst.worldDriver.noteExecuted(executed)
	reject := dispatchPanel(worldPanel{driver: inst.worldDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("world"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// renderKanbanTab is the Kanban dock tab body (ADR-0122): the active result as
// a board. A plain PanelI observer with the same guards as the World tab, plus
// the executed timestamp handed to the driver as its fold-cache key.
func (inst *PlayApp) renderKanbanTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query naming a `lane` and a `title` column to see a board.") {
			rt.Small().Weak()
		}
		return
	}
	inst.kanbanDriver.noteExecuted(executed)
	// Demand the lanes node (its own lane) for the chLanes channel. Both nil
	// (no `lanes` CTE in the buffer) → the channel stays unfilled and the board
	// reads its lanes off the rows; a schema-only view still fills it, so an
	// inventory that legitimately returned nothing reads as "no lanes
	// declared" rather than as "pending".
	lanesRec, lanesSchema := inst.demandKanbanLanes()
	if lanesRec != nil {
		defer lanesRec.Release()
	}
	inputs := map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("kanban"), rec: rec, schema: schema, sig: inst.frameSig},
	}
	if lanesRec != nil || lanesSchema != nil {
		inputs[chLanes] = channelInput{node: kanbanLanesNodeID, rec: lanesRec, schema: lanesSchema, sig: inst.frameSig}
	}
	reject := dispatchPanel(kanbanPanel{driver: inst.kanbanDriver}, inputs, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// demandKanbanLanes compiles the board query's `lanes` CTE — if it has one —
// and demands it on the Kanban driver's lane, returning the retained result for
// the chLanes channel (ADR-0122 §SD6; the caller MUST Release rec).
//
// The node comes from the last Run's split, so the lane inventory is part of
// the user's own query rather than a second buffer to keep in sync. Its signal
// reads resolve like any other node's; a name the prelude bound is a constant
// and travels inside the fused SQL instead.
func (inst *PlayApp) demandKanbanLanes() (rec arrow.RecordBatch, schema *arrow.Schema) {
	d := inst.kanbanDriver
	if d == nil || d.lanesLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, kanbanLanesNodeID)
	if !ok {
		d.lanesLoading = false
		d.lanesErr = nil
		return
	}
	view := d.lanesLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, kanbanLanesNodeID),
		NodeID: kanbanLanesNodeID,
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	d.lanesLoading = view.loading
	d.lanesErr = view.err // mirrored every demand — nil clears (no latch)
	return view.rec, view.schema
}

// renderDetailTab is the Detail dock tab body: the leeway card stack for the
// currently selected row. Detail is an ADR-0097 PanelI observer of the `main`
// node and the consumer of the `selection` signal the Timeline/Table/Projection
// publish — this method runs the panel's Accept (which reads the selection from
// the signal env) and renders its reject reason or the card body. The executed
// timestamp is handed to the ADR-0123 cell cache as half its key (the row is
// the other half). renderDetailPane
// scrolls its own content (the leeway card table owns its scroll; the ad-hoc
// fallback adds one), so the dock tab must NOT add an outer ScrollArea —
// wrapping the self-scrolling card table hands it unbounded height and crops its
// tail (tagged) sections.
func (inst *PlayApp) renderDetailTab(rec arrow.RecordBatch, schema *arrow.Schema, executed time.Time, result ResultID) {
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query, then select a row to see its detail.") {
			rt.Small().Weak()
		}
		return
	}
	inst.richCells.noteExecuted(executed)
	reject := dispatchPanel(detailPanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("detail"), rec: rec, schema: schema, sig: inst.frameSig, result: result},
	}, nil)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
		return
	}
}

func (inst *PlayApp) renderResultsLoading() {
	for range c.VerticalCentered().KeepIter() {
		c.AddSpace(styletokens.Px(inst.density, 7))
		c.Spinner().Size(32).Send()
		c.Label("Executing query…").Send()
		// Live tick from the in-band progress headers (ADR-0115 plane A);
		// absent until the first tick lands or when the endpoint cannot
		// stream them (non-http, mocks).
		if v := inst.frameProgress; v.fresh {
			renderProgressBar(v, loadingProgressWidth)
			diagWeak(formatProgressLine(v))
		}
	}
}

func (inst *PlayApp) renderResultsEmpty() {
	for range c.VerticalCentered().KeepIter() {
		c.AddSpace(styletokens.Px(inst.density, 7))
		c.Label("Run a query to see results.").Send()
	}
}

// renderResultsZeroRows is the empty-state for a query that completed with no
// rows — distinct from renderResultsEmpty ("never ran") so the user can tell
// the query worked and simply matched nothing.
func (inst *PlayApp) renderResultsZeroRows() {
	for range c.VerticalCentered().KeepIter() {
		c.AddSpace(styletokens.Px(inst.density, 7))
		c.Label("0 rows — the query ran but matched nothing.").Send()
	}
}

// renderResultsFailed is the shared failed-state for the result tabs: a
// pointer, not the error itself — the Diagnostics tab is the single owner of
// the full error prose (parse advice, split status, execution error), so the
// same text is never maintained across five panes.
func (inst *PlayApp) renderResultsFailed() {
	for range c.VerticalCentered().KeepIter() {
		c.AddSpace(styletokens.Px(inst.density, 7))
		c.Label("Query failed — see the Diagnostics tab.").Send()
	}
}

func (inst *PlayApp) renderMasterTable(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, selectedRow int64, emit SignalEmitterI) {
	ids := inst.ids
	totalRows := min(rec.NumRows(), numRows)

	// Slice to the current page. The pager was Configure()d with totalRows
	// before this function is called.
	pageStart, pageEnd := inst.pager.Range()
	if pageEnd > totalRows {
		pageEnd = totalRows
	}
	if pageStart > pageEnd {
		pageStart = pageEnd
	}
	displayRows := pageEnd - pageStart

	// visCols is the ordered set of Arrow columns to render: every column for a
	// non-leeway result, or the value + backbone columns plus whichever support /
	// membership columns the options bar reveals for a leeway result, minus any
	// tagged section the hide-empty-sections toggle drops for having no
	// attribute on this page (needs pageStart/pageEnd, hence computed above).
	// egui_table column positions are 1-based after the "#" selector, so table
	// position p renders visCols[p-1]. Button ids key on the Arrow column index
	// (not the display position) so revealing/hiding a column never shifts
	// another column's cell identity.
	visCols := inst.visibleTableCols(rec, schema, pageStart, pageEnd)
	// The synthetic identity columns (ADR-0219 SD7) sit after the Arrow
	// columns at positions len(visCols)+1+k; their sentinel indices take part
	// in the column-set change detection below and in nothing that indexes
	// an Arrow-keyed cache. The job that fills them starts here, once per
	// result, and the cells poll it.
	synth := inst.identitySynthCols(schema)
	var idJob *identityJob
	if len(synth) > 0 {
		idJob = inst.ensureIdentityJob(rec, inst.tableResult)
	}

	// egui_table draws cell content flush to the cell edge ("Does not add any
	// margins to cells" — egui_table's own docs say to add them yourself). We
	// lead every header and body cell with a horizontal AddSpace so content
	// isn't jammed against the gridline or the neighbouring column's header
	// type string. The cell ui is laid out left-to-right, so AddSpace advances
	// the cursor along the row → a left inset. ensureColWidths reserves the
	// same amount so the inset doesn't eat into a column's fitted width.
	cellPadX := styletokens.PaddingTight(inst.density)

	// Emit per-column widths from the schema-keyed cache. Resampling every
	// frame from the current page's content would reflow the table each
	// time the pager advances, since different pages have different string
	// lengths. The cache is invalidated when the Arrow *Schema pointer
	// changes, i.e. on a new query.
	inst.ensureColLabels(schema)
	glossCols := inst.glossColumns(schema)
	inst.ensureColWidths(rec, schema, pageStart, pageEnd)

	// ADR-0151: a width the user set outranks the sampled estimator, which
	// becomes the default for columns they have not touched. cols and the
	// EtColumn sequence stay index-aligned — the leading "#" column takes
	// part so the positional read-back lines up, and since it is not
	// resizable the binding reports our own width straight back for it and
	// nothing is ever captured.
	cols := inst.masterColumnKeys(schema, visCols)
	for k := range synth {
		cols = append(cols, identityColumnKey(identityColKindE(k)))
	}
	resolved := make([]float64, 0, len(cols))
	resolved = append(resolved, masterRowNumColWidth)
	for _, arrowCol := range visCols {
		resolved = append(resolved, float64(inst.colWidths[arrowCol]))
	}
	for range synth {
		resolved = append(resolved, float64(identityColWidth(cellPadX)))
	}
	if inst.colWidthRes != nil {
		// Font size 0: play has no single text size to attribute a width to,
		// and passing one it does not render at would rescale stored widths
		// against a fiction. Zero disables rescaling, the documented meaning.
		resolved = inst.colWidthRes.Resolve(masterTableTag, cols, 0, resolved)
	}

	// egui_table keeps its own width per column *position*, and re-fits to
	// content only on a table's first show ever — so without this a second
	// query inherits the first one's widths by position, and a revealed
	// support column lands in a slot sized for whatever used to sit there.
	// Asking for a re-fit on the frame the column set changes is what makes
	// the widths belong to the result on screen. It has to be one frame:
	// re-fitting continuously would move the columns while the user reads.
	// On that frame the cells go out untruncated (selectableCell), so the
	// re-fit measures their real width rather than a truncated one.
	//
	// A column whose resolved width is not the seed's carries a user
	// override, which outranks any fit, so it sits the re-fit out. Without
	// that test every Run measured the drag away — tableColsChanged compares
	// the *Schema pointer, and a Run always returns a fresh one, so a re-fit
	// fires even for a repeat of the same query.
	refit := inst.tableColsChanged(schema, append(append([]int(nil), visCols...), synth...))
	if refit && inst.colWidthRes != nil {
		// Tell the resolver the crate is about to choose these widths. The
		// read-back lags, so the fit's result lands a report *after* refit
		// has gone false; without this window it reads as a drag and every
		// auto-sized column acquires an override nobody asked for.
		inst.colWidthRes.MarkReseed(masterTableTag)
	}

	// Leading "#" selector column (click to select row) + the data columns.
	// It never takes part in a re-fit: it is not resizable, so egui_table
	// never stores a width for it and the one emitted here always stands.
	c.EtColumn(float32(resolved[0])).Resizable(false).Send()
	for i, arrowCol := range visCols {
		col := c.EtColumn(float32(resolved[i+1])).
			Resizable(true).
			RangeMinMax(inst.colDragMinWidth(), colDragMaxWidth)
		if refit && resolved[i+1] == float64(inst.colWidths[arrowCol]) {
			col = col.AutoSizeThisFrame(true)
		}
		col.Send()
	}
	for k := range synth {
		pos := len(visCols) + 1 + k
		col := c.EtColumn(float32(resolved[pos])).
			Resizable(true).
			RangeMinMax(inst.colDragMinWidth(), colDragMaxWidth)
		if refit && resolved[pos] == float64(identityColWidth(cellPadX)) {
			col = col.AutoSizeThisFrame(true)
		}
		col.Send()
	}

	// The header-click sort (play_table_sort.go) is a permutation of the rows
	// this record already holds: display position p draws record row
	// order[p]. nil means "record order", the unsorted case.
	order := inst.tableSort.orderFor(rec, totalRows)

	// The grid is the last thing in the Table tab, so it takes the rest of
	// it. Without this it falls to the auto-fit cap (400 px) and leaves the
	// bottom of the tab empty on any taller window.
	et := c.EndETable(ids.PrepareStr(masterTableTag),
		uint64(displayRows),
		18.0, 1, 1).
		Striped(true).
		FillPane(true)
	if inst.colWidthRes != nil {
		et = et.ApplyWidths(inst.colWidthRes.Epoch(masterTableTag))
	}
	// Selection is stored as an absolute row index; translate to the
	// page-local row when highlighting so the stripe lands on the right line.
	// Under a sort that is the row's *displayed* position, not its record
	// index — the selection survives sorting rather than following the line.
	if selPos := inst.tableSort.displayPos(order, selectedRow); selPos >= pageStart && selPos < pageEnd {
		et = et.SelectedRow(uint64(selPos - pageStart))
	}

	// Visibility prefetch: the previous frame's egui_table::prepare pushed
	// the visible (row, col) ranges + num_sticky_columns. We only emit
	// cells and headers for columns egui_table will actually draw — cuts
	// the per-frame cell count from ~pageSize*ncols to ~visibleRows*visibleCols.
	// First frame has no prefetch yet; ok=false and ColVisible returns
	// true for everything so egui_table can populate its block-map cache.

	// Header: selector column + data column headers. Tabular data reads
	// as monospace — column names align with their cells, type strings
	// stay legible at small size, and the "#" gutter matches the row
	// numbers below.
	if vis, _ := et.ColVisible(0); vis {
		for range et.Headers(0, 0) {
			inst.headerCell(0, cellPadX, func() {
				for rt := range c.RichTextLabel("#") {
					rt.Weak().Monospace()
				}
			})
		}
	}
	for pos, arrowCol := range visCols {
		colPos := uint32(pos + 1)
		if vis, _ := et.ColVisible(colPos); !vis {
			continue
		}
		for range et.Headers(0, colPos) {
			field := schema.Field(arrowCol)
			gc := &glossCols[arrowCol]
			name := field.Name
			if label := inst.colLabels[name]; label != "" {
				name = label
			}
			// A declared column (ADR-0186) is captioned by its label; the
			// declared media type rides beside the type tag, small, and the
			// resolution — or the refusal — goes on hover with the rest.
			if gc.label != "" {
				name = gc.label
			}
			hover := field.Name + " — " + field.Type.String()
			if gh := glossHover(gc); gh != "" {
				hover += " — " + gh
			}
			inst.headerCell(uint64(arrowCol)+1, cellPadX, func() {
				// The header is a frameless button: clicking it cycles this
				// column's sort (asc → desc → unsorted). The physical column
				// name and the full type stay on hover, as the name did when
				// the header was a plain label — so a leeway handle never
				// hides the name it stands for, and abbreviating the type
				// below loses nothing.
				for range c.HoverText(hover + " — click to sort").KeepIter() {
					if c.Button(ids.PrepareSeq(tableSortIDSalt+uint64(arrowCol)),
						c.Atoms().BeginRichText(name+inst.tableSort.glyph(arrowCol)).Strong().Monospace().End().Keep()).
						Frame(false).
						Truncate().
						SendResp().HasPrimaryClicked() {
						inst.tableSort.clicked(arrowCol)
					}
				}
				for rt := range c.RichTextLabel(shortArrowType(field.Type)) {
					rt.Small().Weak().Monospace()
				}
				if gc.mediaType != "" {
					for rt := range c.RichTextLabel(gc.mediaType) {
						rt.Small().Weak().Monospace()
					}
				}
			})
		}
	}
	for k, sentinel := range synth {
		kind := identityColKindE(k)
		colPos := uint32(len(visCols) + 1 + k)
		if vis, _ := et.ColVisible(colPos); !vis {
			continue
		}
		for range et.Headers(0, colPos) {
			inst.headerCell(uint64(sentinel)+1, cellPadX, func() {
				for range c.HoverText(kind.hover()).KeepIter() {
					for rt := range c.RichTextLabel(kind.name()) {
						rt.Strong().Monospace()
					}
				}
				for rt := range c.RichTextLabel("hex") {
					rt.Small().Weak().Monospace()
				}
			})
		}
	}

	// Cells: every cell is a frameless selectable button so clicking anywhere
	// on a row selects it (not just the "#" column). Button ids use a
	// (row, col) composite seq to avoid collisions with other PrepareSeq
	// sites in the app. `local` is the page-relative row; `absRow` is the
	// index into the underlying record batch (used for formatCell and for
	// the persistent selection).
	const cellIdBase uint64 = 0x01000000
	const cellColStride uint64 = 0x00010000
	rowLo, rowHi := uint64(0), uint64(displayRows)
	if rb, re, _, _, _, ok := et.VisibleRange(); ok {
		rowLo, rowHi = rb, re
		if rowHi > uint64(displayRows) {
			rowHi = uint64(displayRows)
		}
	}
	for local := rowLo; local < rowHi; local++ {
		// The display position walks the page; the record row it draws comes
		// from the sort permutation (identity when unsorted).
		absRow := inst.tableSort.rowAt(order, pageStart+int64(local))
		selected := absRow == selectedRow
		rowBase := cellIdBase + uint64(absRow)*cellColStride

		if vis, _ := et.ColVisible(0); vis {
			for range et.Cells(local, 0) {
				if inst.selectableCell(rowBase, cellPadX, fmt.Sprintf("%d", absRow+1), false, selected, false, false, gloss.ToneNeutral, "", !refit) {
					emit.Emit(signalSelection, absRow)
				}
			}
		}
		for pos, arrowCol := range visCols {
			colPos := uint32(pos + 1)
			if vis, _ := et.ColVisible(colPos); !vis {
				continue
			}
			leftAlign := stringLikeArrowType(schema.Field(arrowCol).Type)
			for range et.Cells(local, colPos) {
				// ADR-0186: the cell text is the column's gloss inline face when
				// one resolves (and the raw toggle is off), else formatCell; a
				// gloss/url cell is a hyperlink to its value.
				text, tone := inst.glossCell(&glossCols[arrowCol], rec.Column(arrowCol), absRow, false)
				link := inst.glossLink(&glossCols[arrowCol], rec.Column(arrowCol), absRow)
				if refit {
					// The untruncated re-fit cell must not size a column to a
					// paragraph: cut what egui measures at the runes that fit
					// colMaxWidth, the same ceiling the seed has always had.
					text = fitRunes(text, colMaxRunes)
				}
				if inst.selectableCell(rowBase+uint64(arrowCol)+1, cellPadX, text, false, selected, false, leftAlign, tone, link, !refit) {
					emit.Emit(signalSelection, absRow)
				}
			}
		}
		for k, sentinel := range synth {
			colPos := uint32(len(visCols) + 1 + k)
			if vis, _ := et.ColVisible(colPos); !vis {
				continue
			}
			text, hover := idJob.cell(identityColKindE(k), absRow)
			for range et.Cells(local, colPos) {
				for range c.HoverText(hover).KeepIter() {
					if inst.selectableCell(rowBase+uint64(sentinel)+1, cellPadX, text, text == "…", selected, false, true, gloss.ToneNeutral, "", !refit) {
						emit.Emit(signalSelection, absRow)
					}
				}
			}
		}
	}
	et.Send()
	inst.captureMasterWidths(et, cols)
}

// masterTableTag is the stable instance-tier scope for the per-DB-row grid's
// column-width overrides, and the etable's id. One constant so the two cannot
// drift apart. It differs from attrTableTag so the two grids keep separate
// instance-tier entries; the column tier is kept apart by tableViewTagRow.
const masterTableTag = "results"

// masterRowNumColWidth is the fixed width of the leading "#" column. It is not
// resizable, so it never acquires an override; it takes part in the resolver's
// column list only to keep indices aligned with the EtColumn sequence and with
// the positional width read-back.
const masterRowNumColWidth = 44.0

// masterColumnKeys builds the resolver's column identities for the per-DB-row
// grid, index-aligned with the EtColumn sequence: the leading "#" column
// first, then the visible data columns.
//
// Identity is the raw Arrow field name and type rather than the friendly label
// the header renders, for the reason attrColumnKeys records: the label is
// derived and would re-key every stored width if the label builder changed.
// Every type carries tableViewTagRow so a width fitted to this grid's packed
// `[len=N]` rendering never reaches the per-attribute grid's exploded scalars
// through the column tier.
func (inst *PlayApp) masterColumnKeys(schema *arrow.Schema, visCols []int) (cols []colwidth.Column) {
	cols = make([]colwidth.Column, 0, len(visCols)+1)
	cols = append(cols, colwidth.Column{Name: "#", Type: "rownum" + tableViewTagRow})
	for _, arrowCol := range visCols {
		f := schema.Field(arrowCol)
		cols = append(cols, colwidth.Column{Name: f.Name, Type: f.Type.String() + tableViewTagRow})
	}
	return
}

// captureMasterWidths feeds the widths egui_table settled on back into the
// resolver and lets the debounce write them.
//
// The first report a table makes is its force-autofit frame, whose widths are
// the crate's idea rather than the user's; passing firstShow on that frame is
// what stops the estimator's first result being frozen as an override nobody
// chose. A re-fit frame is the same kind of frame, and is covered by the
// MarkReseed at the emission site rather than by a flag here — its result
// arrives a report later than the frame that asked for it.
func (inst *PlayApp) captureMasterWidths(et c.EndETableFluid, cols []colwidth.Column) {
	if inst.colWidthRes == nil {
		return
	}
	now := time.Now()
	if fetched, ok := et.ColumnWidths(); ok {
		widths := make([]float64, len(fetched))
		for i, w := range fetched {
			widths[i] = float64(w)
		}
		firstShow := !inst.masterWidthsSeen
		inst.masterWidthsSeen = true
		// refit is not passed here: it describes *this* frame, and the fit it
		// asks for is only visible a report later. MarkReseed at the emission
		// site is what covers that, and it covers this frame too.
		inst.colWidthRes.Observe(masterTableTag, cols, widths, 0, firstShow, now)
	}
	if _, err := inst.colWidthRes.Flush(now); err != nil {
		// inst.logger, not the package logger the attr grid's twin uses:
		// this file logs through the app's own logger throughout.
		inst.logger.Warn().Err(err).Msg("play: storing column widths failed; will retry")
	}
}

// ensureColWidths samples per-column widths the first time a given schema
// is seen and caches them. Subsequent calls with the same schema are a
// cheap pointer compare. The sample window is the first colSampleRows rows
// of whichever page happens to be active when the cache gets populated.
//
// This is a *seed*, not the width the user ends up looking at. egui_table
// re-fits the column to what it can actually measure — real glyph advances,
// the header, the visible cells — on the frame renderMasterTable asks it to
// (tableColsChanged), and keeps its own width from then on. What this
// estimate buys is the frame before that measurement lands, and the columns
// egui_table never stores a width for. Keeping it roughly right is worth the
// few lines; making it exact would be work spent on a number that is
// overwritten a frame later.
func (inst *PlayApp) ensureColWidths(rec arrow.RecordBatch, schema *arrow.Schema, pageStart, pageEnd int64) {
	if schema == inst.colWidthsForSchema && len(inst.colWidths) == schema.NumFields() {
		return
	}
	ncols := schema.NumFields()
	widths := make([]float32, ncols)
	sampleN := min(pageEnd-pageStart, colSampleRows)
	// Reserve the inset renderMasterTable gives each cell, on both sides
	// (cellInset), so a padded cell doesn't truncate content that would
	// otherwise fit.
	cellPadX := styletokens.PaddingTight(inst.density)
	glossCols := inst.glossColumns(schema)
	for col := range ncols {
		// The header shows the friendly label when there is one, so size to it
		// rather than the (longer) physical name — plus the short type tag it
		// renders beside the label, which is part of the header's width. A
		// declared column's caption is its label, and its media-type tag also
		// counts (ADR-0186); the cells sample through the gloss, since that is
		// what they render.
		field := schema.Field(col)
		gc := &glossCols[col]
		headerText := field.Name
		if lbl := inst.colLabels[headerText]; lbl != "" {
			headerText = lbl
		}
		if gc.label != "" {
			headerText = gc.label
		}
		maxChars := utf8.RuneCountInString(headerText) + 1 + utf8.RuneCountInString(shortArrowType(field.Type))
		if gc.mediaType != "" {
			maxChars += 1 + utf8.RuneCountInString(gc.mediaType)
		}
		for r := range sampleN {
			text, _ := inst.glossCell(gc, rec.Column(col), pageStart+r, false)
			if n := utf8.RuneCountInString(text); n > maxChars {
				maxChars = n
			}
		}
		w := float32(maxChars)*colCharPx + 16.0 + 2*cellPadX
		if w < colMinWidth {
			w = colMinWidth
		}
		if w > colMaxWidth {
			w = colMaxWidth
		}
		widths[col] = w
	}
	inst.colWidthsForSchema = schema
	inst.colWidths = widths
}

// tableColsChanged reports whether the results grid is about to emit a
// different set of columns than it did last frame, and records the new one.
// It is the trigger for a one-frame re-fit, so it must answer true exactly
// once per change.
//
// Both halves matter. The schema pointer catches a new query; the visCols
// contents catch a column set that changed under an unchanged schema, which
// is what the options bar's support/membership reveals and the hide-empty
// toggle do. Keying on the schema alone would leave a revealed column
// wearing the width of whichever column used to occupy its position.
func (inst *PlayApp) tableColsChanged(schema *arrow.Schema, visCols []int) (changed bool) {
	changed = schema != inst.tableFitSchema || !slices.Equal(visCols, inst.tableFitCols)
	if changed {
		inst.tableFitSchema = schema
		inst.tableFitCols = append(inst.tableFitCols[:0], visCols...)
	}
	return
}

// ensureColLabels rebuilds the friendly column-label map when the result schema
// changes (cheap pointer compare, same idiom as ensureColWidths). Physical
// leeway column names map to readable section / section:column labels via
// lwsql.BuildLabels; a non-leeway result yields a nil map and the raw physical
// names are shown. This is display-only — the SQL sent to the server keeps
// physical names.
func (inst *PlayApp) ensureColLabels(schema *arrow.Schema) {
	if schema == inst.colLabelsForSchema {
		return
	}
	names := make([]string, 0, schema.NumFields())
	for i := 0; i < schema.NumFields(); i++ {
		names = append(names, schema.Field(i).Name)
	}
	inst.colLabels = lwsql.BuildLabels(names)
	inst.colLabelsForSchema = schema
}

// fitRunes cuts s to at most n runes plus an ellipsis: the text a re-fit
// cell hands egui_table to measure, so a paragraph cannot size its column
// past colMaxWidth.
func fitRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos] + "…"
		}
		i++
	}
	return s
}

func historyLabel(e HistoryEntry) string {
	sql := strings.ReplaceAll(e.SQL, "\n", " ")
	sql = strings.Join(strings.Fields(sql), " ")
	status := fmt.Sprintf("%sr %s",
		humanize.Comma(e.NumRows), e.Elapsed.Round(time.Millisecond))
	if e.ErrorText != "" {
		status = "ERR"
	}
	line := fmt.Sprintf("%s  %s  %s",
		e.Executed.Format("15:04:05"), status, sql)
	return truncateRunes(line, historyLabelChar)
}

// humanBytes retired 2026-08-05 for humanize.IBytes. It divided by 1024 and
// labelled the result "KB", so every byte figure play printed named a unit it
// was not: "39.9 KB" for 40,858 bytes is 39.9 KiB. The replacement spells the
// binary units it actually computes.
