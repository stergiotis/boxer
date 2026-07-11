package play

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pager"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
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
	// Column-width heuristic bounds (px).
	colMinWidth      = 100.0
	colMaxWidth      = 420.0
	colCharPx        = 7.0 // approx monospace-ish character advance
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
	dockTabEditor     uint64 = 1
	dockTabHistory    uint64 = 2
	dockTabTable      uint64 = 3
	dockTabProjection uint64 = 4
	dockTabDetail     uint64 = 5
	dockTabPreview    uint64 = 6
	dockTabTimeline   uint64 = 7
	dockTabSnippets   uint64 = 8
	dockTabMap        uint64 = 9
	dockTabGraph      uint64 = 10
	dockTabSchema     uint64 = 11
	dockTabWorld      uint64 = 12
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
	// launchURL is the original target restored by "External (reset)". See
	// renderEndpointSwitcher and Client.SetURL (ADR-0094 §SD6).
	endpointDraft string
	launchURL     string

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at NewPlayApp.
	density styletokens.DensityE

	sql         string
	lastSentSql string
	// Slice-5a signal-store state. frameSig is the per-frame immutable
	// snapshot of the graph's signal store, taken at Render top so every
	// consumer in a frame sees a single revision (glitch-freedom as frame
	// semantics); an emit lands next frame. lastSentSigParams /
	// lastRunBound record what the last Run resolved (URL-keyed) and which
	// names its prelude bound — the signal half of the staleness witness
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
	// pendingSnippetInsert / pendingSnippetReplace hold a snippet-library
	// click until the editor consumes it on the next frame: Insert splices
	// the snippet at the caret via TextEditFluid.InsertAtCursor (ADR-0063);
	// Replace swaps the whole buffer (a plain Go assignment — no FFI). Set by
	// renderSnippetsTab, captured-and-cleared by renderSqlEditor.
	pendingSnippetInsert  string
	pendingSnippetReplace string
	requestRun            bool
	cards                 *CardDriver
	projector             *Projector

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
	queryFSM               *fsmview.Machine[queryStateE]
	queryFSMWidget         *fsmview.Widget[queryStateE]
	timeline               *TimelineDriver
	timelineBandsSql       string
	timelineNowLineEnabled bool

	// mapDriver is the ADR-0096 geo-raster map panel (Map dock tab): a walkers
	// map whose viewport drives an in-DB-rendered raster on a panel-local lane.
	mapDriver *MapDriver

	// worldDriver is the ADR-0114 schematic world-choropleth panel (World dock
	// tab): a plain observer of the active result — no lane, nothing to Close.
	worldDriver *WorldDriver

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

	// "As sent" preview toggle (ADR-0108): when on, the Preview tab shows
	// the statement Client.BuildStatement would ship — params harvested,
	// pre-execute passes applied, FORMAT rewritten — instead of the
	// canonical form. wireFor keys the debounced cache like formattedFor;
	// wireParams holds the harvested URL params for the caption line.
	previewAsSent bool
	wireFor       string
	wireBody      string
	wireParams    map[string]string

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
	paramSlots             []paramSlot
	paramDrafts            map[string]*string
	paramSyncedValues      map[string]string
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
// inst.sql stays. The caller (PlayLauncher.Mount) decides precedence:
// today it lets SPINNAKER_PLAY_SQL win over persist, persist win over
// the literal default.
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
}

// PersistSql writes inst.sql under persistKeyLastSql when storage is
// wired. Called on Run + Unmount; errors are logged at debug level
// (audit-trail concern, not a user-visible failure).
func (inst *PlayApp) PersistSql() {
	if inst.storage == nil {
		return
	}
	err := inst.storage.Set(persistKeyLastSql, []byte(inst.sql))
	if err != nil {
		inst.logger.Debug().Err(err).Msg("play: persist save failed")
	}
}

// RestorePersistedTimelineBandsSql loads the bands-SQL editor buffer
// from the persist cap. Same best-effort semantics as RestorePersistedSql.
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

// PersistTimelineBandsSql writes the current bands-SQL editor buffer
// to the persist cap. Called from Unmount so the user's bands query
// survives session restart; the value-write happens unconditionally
// so an empty buffer also persists (and overrides a previous value).
func (inst *PlayApp) PersistTimelineBandsSql() {
	if inst.storage == nil {
		return
	}
	err := inst.storage.Set(persistKeyTimelineBandsSql, []byte(inst.timelineBandsSql))
	if err != nil {
		inst.logger.Debug().Err(err).Msg("play: persist save (bands) failed")
	}
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

	rawReply, rerr := inst.bus.Request(fsbroker.SubjectDialogRead, nil)
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
	body, rerr := inst.bus.Request(dr.HandleSubjectPrefix+".read", nil)
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

func NewPlayApp(client *Client, graph *queryGraph, initialSQL string) *PlayApp {
	cardIds := c.NewWidgetIdStack()
	pagerIds := c.NewWidgetIdStack()
	projectorIds := c.NewWidgetIdStack()
	projFSMIds := c.NewWidgetIdStack()
	queryFSMIds := c.NewWidgetIdStack()
	timelineIds := c.NewWidgetIdStack()
	cards := NewCardDriver(cardIds, nil)
	projFSM := newProjectorFSM()
	queryFSM := newQueryFSM()
	// client may be nil in tests and the legacy CLI path; the endpoint switcher
	// is guarded behind a non-nil client in renderTopBar, so an empty launch
	// URL is harmless here.
	launchURL := ""
	if client != nil {
		launchURL = client.URL()
	}
	inst := &PlayApp{
		ids:              c.NewWidgetIdStack(),
		graph:            graph,
		client:           client,
		intermediateLane: newNodeLane(clientExecutor{client: client, opts: newExecOptions("intermediate")}, memory.NewGoAllocator(), 0),
		endpointDraft:    launchURL,
		launchURL:        launchURL,
		density:          styletokens.DensityFromEnv(),
		sql:              initialSQL,
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
		// Range widget first so the Grafana-style picker (when the
		// host has wired an evaluator via SetCapabilities) folds the
		// from/to pair; otherwise its Matches returns ok=false and
		// the simpler dateTimePairWidget claims the slots. Scalar
		// text widget is the tail catch-all — one TextEdit per
		// remaining slot.
		paramWidgets: []paramWidgetI{
			newDateTimeRangeWidget(),
			newDateTimePairWidget(),
			newScalarTextWidget(),
		},
	}
	inst.timeline = NewTimelineDriver(timelineIds, client, &inst.timelineBandsSql, &inst.timelineNowLineEnabled)
	inst.mapDriver = NewMapDriver(c.NewWidgetIdStack(), client)
	inst.worldDriver = NewWorldDriver(c.NewWidgetIdStack())
	inst.affordanceEval = newAffordanceEvaluator(&inst.observations)
	return inst
}

// Close tears down the app's async machinery (Unmount): cancels in-flight
// work, releases held results, and closes every lane. Late completions from
// still-running goroutines hit their generation/closed guards and are
// dropped. Idempotent; the app is unusable afterwards.
func (inst *PlayApp) Close() {
	if inst.projector != nil {
		inst.projector.Detach()
	}
	if inst.intermediateLane != nil {
		inst.intermediateLane.close()
	}
	if inst.mapDriver != nil && inst.mapDriver.lane != nil {
		inst.mapDriver.lane.close()
	}
	if inst.timeline != nil && inst.timeline.bandsLane != nil {
		inst.timeline.bandsLane.close()
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
func (inst *PlayApp) activeSnapshot() (rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	split := inst.currentSplit
	if inst.observedNode != "" && inst.observedNode != split.Sink && len(split.Nodes) > 0 {
		if node, ok := findSplitNode(split, inst.observedNode); ok {
			// The intermediate's signal values resolve from its Reads (the
			// split's signal edges) against the frame snapshot; names the
			// last Run's prelude bound are constants and travel inside the
			// fused SQL's SET prelude instead (slice 5a).
			view := inst.intermediateLane.demand(compiledNode{
				SQL:    fuseNode(split, inst.observedNode),
				Params: resolveSignalNames(node.Reads, inst.lastRunBound, inst.frameSig),
			})
			if view.rec != nil {
				numRows = view.rec.NumRows()
			}
			return view.rec, view.schema, numRows, view.loading, view.elapsed, view.summary, view.executedAt, view.err
		}
	}
	return inst.graph.MainSnapshot()
}

func (inst *PlayApp) Render() error {
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
	rec, schema, numRows, loading, elapsed, summary, executed, err := inst.activeSnapshot()
	if rec != nil {
		defer rec.Release()
		inst.syncSelectionClamp(rec)
		if executed != inst.pagerSeenExecuted {
			inst.pagerSeenExecuted = executed
			inst.pager.Reset()
		}
		inst.pager.Configure(rec.NumRows())
		inst.projector.Invalidate(schema, executed)
		inst.syncSchemaModel(schema)
	}

	// Mirror the result↔input lifecycle into the query FSM every frame —
	// runs outside the rec!=nil guard so idle / empty / failed are observed
	// too. The status bar and its chip both read inst.queryFSM.
	inst.syncQueryFSM(loading, numRows, executed, err)

	// Run the canonical-form pipeline once per frame regardless of which
	// tab is active. The pipeline is debounced internally (previewDebounce),
	// so most frames are a no-op; running it here keeps the Preview tab's
	// output fresh even when the user has the Editor tab hidden.
	inst.updatePreview()

	// Layout inside the runtime-created window scope (ADR-0026
	// Amendment 2026-05-12). Mirrors imztop's shape: a pinned topbar
	// with controls, a single DockArea hosting the body panes as
	// drag-rearrangeable tabs, and a non-resizable status bar for
	// per-result metrics. The DockArea's initial split lives in the
	// InitRoot/Split block; once the user drags, the persistent
	// dock_state on the Rust side wins.
	for range c.PanelTopInside(ids.PrepareStr("topbar")).Resizable(false).KeepIter() {
		inst.renderTopBar()
	}
	for range c.PanelBottomInside(ids.PrepareStr("status")).Resizable(false).KeepIter() {
		inst.renderStatus(numRows, elapsed, summary, executed, err)
	}
	for range c.PanelCentralInside().KeepIter() {
		for dock := range c.DockArea(ids.PrepareStr("play-dock")) {
			editLeaf := dock.InitRoot(dockTabEditor, dockTabHistory)
			bodyTabs := []uint64{dockTabTable, dockTabProjection, dockTabTimeline, dockTabSnippets, dockTabMap, dockTabWorld, dockTabGraph, dockTabSchema}
			if FocusMap.Get() != "" {
				// Scripted-screenshot focus: make Map the default-active body tab.
				bodyTabs = []uint64{dockTabMap, dockTabTable, dockTabProjection, dockTabTimeline, dockTabSnippets, dockTabWorld, dockTabSchema}
			}
			if FocusGraph.Get() != "" {
				// Scripted-screenshot focus: make Graph the default-active body tab.
				bodyTabs = []uint64{dockTabGraph, dockTabTable, dockTabProjection, dockTabTimeline, dockTabSnippets, dockTabMap, dockTabWorld, dockTabSchema}
			}
			if FocusTimeline.Get() != "" {
				// Scripted-screenshot focus: make Timeline the default-active body tab.
				bodyTabs = []uint64{dockTabTimeline, dockTabTable, dockTabProjection, dockTabSnippets, dockTabMap, dockTabWorld, dockTabGraph, dockTabSchema}
			}
			if FocusSchema.Get() != "" {
				// Scripted-screenshot focus: make Schema the default-active body tab.
				bodyTabs = []uint64{dockTabSchema, dockTabTable, dockTabProjection, dockTabTimeline, dockTabSnippets, dockTabMap, dockTabWorld, dockTabGraph}
			}
			if FocusWorld.Get() != "" {
				// Scripted-screenshot focus: make World the default-active body tab.
				bodyTabs = []uint64{dockTabWorld, dockTabTable, dockTabProjection, dockTabTimeline, dockTabSnippets, dockTabMap, dockTabGraph, dockTabSchema}
			}
			bodyLeaf := dock.Split(editLeaf, c.DockBelow, 0.45, bodyTabs...)
			_ = dock.Split(bodyLeaf, c.DockRight, 0.70, dockTabDetail)
			_ = dock.Split(editLeaf, c.DockRight, 0.55, dockTabPreview)

			for range dock.Tab(dockTabEditor, "Editor") {
				inst.renderEditorTab()
			}
			for range dock.Tab(dockTabPreview, "Preview") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderPreviewTab()
				}
			}
			for range dock.Tab(dockTabHistory, "History") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderHistoryTab()
				}
			}
			for range dock.Tab(dockTabTable, "Table") {
				inst.renderTableTab(rec, schema, numRows, loading, err)
			}
			for range dock.Tab(dockTabProjection, "Projection") {
				inst.renderProjectionTab(rec, loading, err)
			}
			for range dock.Tab(dockTabTimeline, "Timeline") {
				inst.renderTimelineTab(rec, schema, loading, err)
			}
			for range dock.Tab(dockTabSnippets, "Snippets") {
				inst.renderSnippetsTab()
			}
			for range dock.Tab(dockTabDetail, "Detail") {
				inst.renderDetailTab(rec, schema)
			}
			// TabNoScroll: the walkers map reads wheel/zoom input globally
			// (no consumption), so the dock's default body ScrollArea would
			// scroll the panel in the same gesture that pans/zooms the map —
			// the map jitters while zooming. Overflow clips instead.
			for range dock.TabNoScroll(dockTabMap, "Map") {
				inst.mapDriver.Render(inst.frameSig, inst.sigEmit)
			}
			for range dock.Tab(dockTabWorld, "World") {
				inst.renderWorldTab(rec, schema, loading, err, executed)
			}
			for range dock.Tab(dockTabGraph, "Graph") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderGraphTab()
				}
			}
			for range dock.Tab(dockTabSchema, "Schema") {
				inst.renderSchemaTab(rec, schema, loading, err)
			}
		}
	}

	// Execute after rendering — keeps the UI responsive on the submit frame.
	if inst.requestRun && !inst.graph.MainLoading() {
		inst.requestRun = false
		sql := strings.TrimSpace(inst.sql)
		if sql != "" {
			// ADR-0097 3c: split the buffer into the node graph and fuse to the
			// sink for execution. For a single statement the fused SQL is the
			// original (the client re-lifts the SET prelude either way), so this
			// is behaviour-identical. On a split/parse failure, fall back to the
			// raw buffer so ClickHouse reports the error exactly as before.
			executable, split, fErr := fuseToSink(sql)
			if fErr != nil {
				executable = sql
				split = splitResult{}
			}
			inst.currentSplit = split
			inst.splitErr = fErr
			// A fresh run resets the observed node to the new sink and forgets
			// the intermediate lane's memo (3d): re-observing an intermediate
			// after a Run re-executes against the possibly-changed data.
			inst.observedNode = split.Sink
			inst.intermediateLane.forget()
			// Scripted-screenshot affordance: observe a named node on run so a
			// capture can show the panels rendering an intermediate (mirrors
			// SPINNAKER_PLAY_FOCUS_*). Ignored when the node is absent.
			if obs := ObserveNode.Get(); obs != "" {
				if _, ok := findSplitNode(split, NodeID(obs)); ok {
					inst.observedNode = NodeID(obs)
				}
			}
			// Resolve the buffer's unbound param slots against the frame's
			// signal snapshot (slice 5a): the values ride the request URL and
			// the run's history entry snapshots them (D4). The resolution and
			// the bound-name set also feed the staleness witness and the
			// observed intermediates.
			sigParams, boundNames := inst.resolveRunSignals(sql)
			inst.lastSentSql = sql
			inst.lastSentSigParams = sigParams
			inst.lastRunBound = boundNames
			inst.graph.RunMain(executable, sigParams)
			// Persist on Run: the user's intent is "this is the SQL I
			// want to keep around". Save-on-Unmount is the fallback
			// for sessions that never Run; doing both keeps the
			// persistence point user-intent-anchored.
			inst.PersistSql()
		}
	}

	inst.frame++
	inst.autoShotTick()
	c.RequestRepaint()
	return nil
}

// syncSelectionClamp keeps the selection signal valid for the active result
// (slice 5b, replacing the selectedRow field clamp): an absent or
// out-of-range selection resets to row 0, so a fresh result auto-selects its
// first row exactly as before. The write lands in the store immediately and
// is visible from the NEXT frame's snapshot; this frame's panels guard
// out-of-range rows themselves (the Detail empty-state), so the one-frame
// window is benign. An in-range selection writes nothing (and a repeated "0"
// write does not bump the store revision).
func (inst *PlayApp) syncSelectionClamp(rec arrow.RecordBatch) {
	if rec == nil {
		return
	}
	row, found := readSelection(inst.frameSig)
	if !found || row < 0 || row >= rec.NumRows() {
		inst.graph.setSignalRaw(signalSelection, "0")
	}
}

// resolveRunSignals resolves a Run buffer's unbound param slots against the
// frame's signal snapshot (slice 5a): a fresh parse (the debounced caches may
// lag the buffer) yields the slot list and the SET-bound names; unbound names
// with a store value become URL params. Also returns the bound-name set (the
// prelude constants — D1: a SET pins, so those names never consult the
// store). On a parse failure — the raw-fallback Run path — nothing resolves
// and the server reports the real problem, exactly as for the SQL itself.
func (inst *PlayApp) resolveRunSignals(sql string) (sigParams map[string]string, bound map[string]bool) {
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
	sigParams = resolveSignalNames(names, bound, inst.frameSig)
	return
}

// restoreHistoryEntry restores a past run: the buffer, plus the signal values
// the run shipped seeded back into the store (slice-5 D4), so re-running
// reproduces the same inputs. A SET-bound name still shadows a seeded signal
// at execution (D1).
func (inst *PlayApp) restoreHistoryEntry(entry HistoryEntry) {
	inst.sql = entry.SQL
	for urlKey, raw := range entry.SigParams {
		inst.graph.setSignalRaw(strings.TrimPrefix(urlKey, "param_"), raw)
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
		// Wait until a query has completed with results.
		if inst.didAutoRun && !inst.graph.MainLoading() {
			rec, _, _, _, _, _, _, _ := inst.graph.MainSnapshot()
			if rec != nil {
				rec.Release()
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
		// SPINNAKER_PLAY_SHOT_SETTLE bumps the settle window so scripted
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
func (inst *PlayApp) renderTopBar() {
	ids := inst.ids
	for range c.Horizontal().KeepIter() {
		if inst.graph.MainLoading() {
			c.Spinner().Size(16).Send()
			if c.Button(ids.PrepareStr("cancel"), c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.graph.CancelMain()
			}
		} else {
			if c.Button(ids.PrepareStr("run"), c.Atoms().Text("Run").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.requestRun = true
			}
		}

		// Load .sql via fs Powerbox — only when the runtime wired a
		// bus client. The picker overlay lives at the host level
		// (carousel renders pickerbridge between Frame and metrics);
		// this button only kicks the fs.dialog.read request that puts
		// a pending entry on the broker's queue.
		var pickErr string
		if inst.bus != nil {
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

		if inst.client != nil {
			c.Separator().Vertical().Send()
			inst.renderEndpointSwitcher()
		}

		// Hide-prelude toggle (visible only when there's at least one
		// param slot — no point in offering it for queries with no
		// placeholders). Mutates the canonical state on the next
		// frame; the editor binding flips at the start of the next
		// renderEditorTab.
		if len(inst.paramSlots) > 0 {
			c.Separator().Vertical().Send()
			c.Checkbox(ids.PrepareStr("hidePrelude"), inst.paramHidePrelude, "Hide prelude").
				SendRespVal(&inst.paramHidePrelude)
		}
	}
}

// renderEndpointSwitcher is the toolbar control for the query target. It shows
// the current endpoint read-only beside a fixed-label "Endpoint" menu (a
// dynamic MenuButton label would shift its derived id and drop menu state). The
// menu offers a manual URL plus two presets: the in-process keelson
// introspection /query endpoint (shown only when a co-resident host published
// one via introspecthost.Start → introspect.LocalQueryEndpoint, ADR-0094 §SD6)
// and the launch URL ("External"). Every widget uses an explicit stable id, so
// conditionally showing the keelson preset never drifts the others' ids.
func (inst *PlayApp) renderEndpointSwitcher() {
	ids := inst.ids
	c.Label(fmt.Sprintf("%s  as %s", truncateRunes(inst.client.URL(), 40), inst.client.cfg.User)).
		Truncate().Send()
	for range c.MenuButton(c.Atoms().Text("Endpoint").Keep()).KeepIter() {
		c.TextEdit(ids.PrepareStr("endpointDraft"), inst.endpointDraft, false).
			SendRespVal(&inst.endpointDraft)
		if c.Button(ids.PrepareStr("endpointApply"), c.Atoms().Text("Apply").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.client.SetURL(strings.TrimSpace(inst.endpointDraft))
		}
		c.Separator().Send()
		if ep := introspect.LocalQueryEndpoint(); ep != "" {
			if c.Button(ids.PrepareStr("endpointKeelson"),
				c.Atoms().Text("Keelson introspection").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.setEndpoint(ep)
			}
		}
		if c.Button(ids.PrepareStr("endpointExternal"),
			c.Atoms().Text("External (reset)").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.setEndpoint(inst.launchURL)
		}
	}
}

// setEndpoint repoints the client and syncs the draft TextEdit, telling the
// frontend to drop its cached buffer so the new URL shows (the "Stubborn Text"
// override — a programmatic write to an interactive-widget binding).
func (inst *PlayApp) setEndpoint(u string) {
	inst.client.SetURL(u)
	inst.endpointDraft = u
	c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.endpointDraft)
}

// renderEditorTab is the Editor dock tab body: multi-line SQL editor
// followed by the SQL function affordances. The syntax-highlighted
// canonical form lives in its own Preview tab (split to the right by
// default); the toolbar lives in the topbar.
//
// The TextEdit's desired_rows is computed from the previous frame's
// captured ui.available_size (R18) so the editor fills the dock pane
// vertically. egui's TextEdit otherwise allocates a fixed
// desired_rows × row_height and leaves the rest of the pane blank.
// First frame falls back to editorDesiredRows; the editor's own
// internal scroll handles content overflow.
func (inst *PlayApp) renderEditorTab() {
	// Approximate row height for Monospace at default text-style size
	// (TextStyle::Monospace ≈ 14 px + ~2 px line spacing). The reserve
	// covers chrome below the editor: a thin bottom margin always, plus
	// room for the affordances block when at least one observation was
	// captured by the most recent updatePreview run. The parameter block
	// now renders ABOVE the editor, so it is deliberately absent from
	// this reserve: CaptureAvailableSize runs after renderParamSlots, so
	// avail.H already has the param block's height subtracted.
	const editorRowPx float32 = 16.0
	const editorBaseReservePx float32 = 8.0
	const editorAffordanceReservePx float32 = 120.0

	rows := uint32(editorDesiredRows)
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	if !math.IsNaN(float64(avail.H)) && avail.H > 0 {
		reserve := editorBaseReservePx
		if len(inst.observations) > 0 {
			reserve += editorAffordanceReservePx
		}
		usable := avail.H - reserve
		if usable > 0 {
			if r := uint32(usable / editorRowPx); r > rows {
				rows = r
			}
		}
	}

	for range c.Vertical().KeepIter() {
		// Param-slot widgets render above the editor; they author the
		// leading SET prelude. Rendered first so the editor below claims
		// the remaining vertical space.
		inst.renderParamSlots()

		// Capture the height left below the param block for next frame's
		// editor sizing. Runs AFTER renderParamSlots so the captured
		// value already excludes the param block, but BEFORE the editor:
		// the param block is fixed-height for a given slot count, so
		// capturing here is stable, whereas capturing after the
		// variable-height editor would ratchet the size down each frame.
		c.CaptureAvailableSize()

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

// sqlTextEditField is the shared multi-line CodeEditor TextEdit
// builder for the SQL editor surface — three variants reuse this
// single chain (canonical, fallback when slicing fails, residual
// mirror in hide mode). idSlot keeps each instance's stable widget
// id distinct; valuePtr is the bound buffer (both displayed value
// and SendRespVal target); hint is the empty-buffer placeholder.
func (inst *PlayApp) sqlTextEditField(idSlot string, valuePtr *string, hint string, rows uint32, pendingInsert string) {
	b := c.TextEdit(inst.ids.PrepareStr(idSlot), *valuePtr, true).
		CodeEditor().
		DesiredRows(rows).
		DesiredWidth(float32(math.Inf(1))).
		HintText(hint)
	// Snippet-library Insert: hand the pending snippet to the editor so the
	// Rust side splices it at the caret next frame (TextEditFluid.InsertAtCursor,
	// ADR-0063). Only the visible editor is given the text, so it lands where
	// the user is looking; empty means no insert this frame.
	if pendingInsert != "" {
		b = b.InsertAtCursor(pendingInsert)
	}
	b.SendRespVal(valuePtr)
}

// renderSqlEditor wires the main SQL TextEdit and the show/hide
// parameter-prelude toggle. Default mode binds the editor to
// inst.sql verbatim — the user sees and can hand-edit the SET
// prelude. Hide mode delegates the canonical/mirror state machine
// to recomposeMirror (see play_param_inject.go) and renders the
// sliced-off prelude as a read-only label above the residual editor.
func (inst *PlayApp) renderSqlEditor(rows uint32) {
	const mainHint = "-- type SQL, press Run"
	// Consume any pending snippet-library actions once per frame. Replace
	// swaps the whole buffer — assign inst.sql before the mode branch so it
	// works in both: non-hide binds inst.sql directly, and hide-mode
	// recomposeMirror re-derives the residual from the new canonical. Insert
	// is handed to whichever editor renders below (exactly one does); Replace
	// supersedes a same-frame Insert. Cleared eagerly so each click applies
	// exactly once.
	pending := inst.pendingSnippetInsert
	inst.pendingSnippetInsert = ""
	if replace := inst.pendingSnippetReplace; replace != "" {
		inst.pendingSnippetReplace = ""
		inst.sql = replace
		pending = ""
	}
	if !inst.paramHidePrelude {
		inst.sqlTextEditField("sqlEditor", &inst.sql, mainHint, rows, pending)
		return
	}

	pre := recomposeMirror(inst.sql, inst.paramSqlEdit, inst.paramSqlEditSyncedFrom)
	if !pre.OK {
		// Parse broken — fall back to the unsliced editor so the
		// user can fix the syntax. Don't try to slice a buffer we
		// don't understand.
		inst.sqlTextEditField("sqlEditor", &inst.sql, mainHint, rows, pending)
		return
	}
	inst.sql = pre.Canonical
	inst.paramSqlEdit = pre.Mirror
	inst.paramSqlEditSyncedFrom = pre.SyncedFrom

	if pre.Prelude != "" {
		for rt := range c.RichTextLabel(strings.TrimRight(pre.Prelude, "\n")) {
			rt.Small().Weak().Monospace()
		}
	}
	inst.sqlTextEditField("sqlEditorResidual", &inst.paramSqlEdit,
		"-- type SQL (prelude hidden)", rows, pending)
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
		for rt := range c.RichTextLabel("parse: " + inst.formattedErr.Error()) {
			rt.Small().Weak()
		}
	case inst.formatted != "":
		c.CodeView(ids.PrepareStr("sqlPreview"),
			codeview.BuildSql(inst.formatted)).
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
			// Signal values the store would supply at Run for the buffer's
			// unbound slots (slice 5a) — name=value, values truncated.
			pairs := make([]string, 0, len(inst.wireSignals))
			for k, v := range inst.wireSignals {
				pairs = append(pairs, k+"="+truncateRunes(v, 24))
			}
			sort.Strings(pairs)
			for rt := range c.RichTextLabel("signals on URL: " + strings.Join(pairs, ", ")) {
				rt.Small().Weak()
			}
		}
		c.CodeView(ids.PrepareStr("sqlWire"),
			codeview.BuildSql(inst.wireBody)).
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
	raw := strings.TrimSpace(inst.sql)
	if raw == "" {
		inst.formatted = ""
		inst.formattedErr = nil
		inst.refreshParamSlotsFromParse(nil, nil)
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
	// does not capture it.
	if err := formatSyntaxError(raw); err != nil {
		inst.formatted = ""
		inst.formattedErr = err
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
	if err != nil {
		inst.formatted = ""
		inst.formattedErr = err
		return
	}
	inst.formatted = out
	inst.formattedErr = nil
}

// updateWirePreview keeps the "as sent" cache in sync with inst.sql on the
// same debounce as the canonical preview. Computed only while the toggle
// is on: BuildStatement re-parses per pass, and paying that per edit for a
// hidden view would be waste. Toggling the checkbox on picks the current
// buffer up on the next frame (wireFor is stale and the debounce window
// has long elapsed). The signal caption additionally refreshes when the
// store revision moves (a signal can change without a buffer edit).
func (inst *PlayApp) updateWirePreview() {
	if !inst.previewAsSent || inst.client == nil {
		return
	}
	sigRev := uint64(0)
	if inst.frameSig != nil {
		sigRev = inst.frameSig.Revision()
	}
	if inst.sql == inst.wireFor && sigRev == inst.wireSigRev {
		return
	}
	if inst.sql != inst.wireFor && time.Since(inst.lastEditAt) < previewDebounce {
		return
	}
	inst.wireFor = inst.sql
	inst.wireSigRev = sigRev
	raw := strings.TrimSpace(inst.sql)
	if raw == "" {
		inst.wireBody = ""
		inst.wireParams = nil
		inst.wireSignals = nil
		return
	}
	inst.wireBody, inst.wireParams = inst.client.BuildStatement(raw)
	inst.wireSignals, _ = inst.resolveRunSignals(raw)
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
func (inst *PlayApp) renderStatus(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	inst.queryFSMWidget.
		Provenance(inspector.Provenance{
			Subject:   "app.play.query.result-state",
			SourceApp: "github.com/stergiotis/boxer/apps/play",
			SampledAt: executed,
		}).
		Summary(func() { inst.renderQuerySummary(numRows, elapsed, summary, executed, err) }).
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
}

// renderTableTab is the Table dock tab body: pager strip atop the master
// table, with a centred empty-state when there is no result yet. loading is
// the ACTIVE snapshot's flag (activeSnapshot), not MainLoading(): an observed
// intermediate loads on its own lane, and gating the spinner on the main lane
// showed "0 rows" during its first fetch (review finding). Same for the
// Projection/Timeline/Schema tabs below.
func (inst *PlayApp) renderTableTab(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, err error) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			c.Label("Query failed:").Send()
			// Selectable so the ClickHouse diagnostic (folded into err.Error()
			// by ExecuteArrowStream) can be copied out to search or a bug report.
			c.Label(err.Error()).Wrap().Selectable(true).Send()
		}
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
	// Give the pager strip vertical breathing room off the tab bar and rule it
	// off from the grid, so the toolbar reads as its own band rather than being
	// jammed against the table's first header row.
	pad := styletokens.PaddingTight(inst.density)
	c.AddSpace(pad)
	inst.pager.Render()
	c.AddSpace(pad)
	c.Separator().Send()
	dispatchPanel(tablePanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.activeNodeID(), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
}

// renderProjectionTab is the Projection dock tab body: the UMAP scatter
// with its own toolbar/status. Same empty/error guards as the Table tab.
func (inst *PlayApp) renderProjectionTab(rec arrow.RecordBatch, loading bool, err error) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		c.Label("Query failed.").Send()
		return
	}
	if rec == nil {
		inst.renderResultsEmpty()
		return
	}
	dispatchPanel(projectionPanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.activeNodeID(), rec: rec, schema: rec.Schema(), sig: inst.frameSig},
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
		c.Label("Query failed.").Send()
		return
	}
	if rec == nil {
		// Timeline-specific empty state: pair the generic "run a query"
		// hint with the column contract so first-time users see what
		// shape of SELECT the panel expects without leaving the tab.
		for range c.Vertical().KeepIter() {
			c.Label("Run a query to see the timeline.").Send()
			c.AddSpace(8)
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
		chEvents: {node: inst.activeNodeID(), rec: rec, schema: schema, sig: inst.frameSig},
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
		c.AddSpace(8)
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
		c.Label("Query failed.").Send()
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
		chMain: {node: inst.activeNodeID(), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// renderDetailTab is the Detail dock tab body: the leeway card stack for the
// currently selected row. Detail is an ADR-0097 PanelI observer of the `main`
// node and the consumer of the `selection` signal the Timeline/Table/Projection
// publish — this method runs the panel's Accept (which reads the selection from
// the signal env) and renders its reject reason or the card body. renderDetailPane
// scrolls its own content (the leeway card table owns its scroll; the ad-hoc
// fallback adds one), so the dock tab must NOT add an outer ScrollArea —
// wrapping the self-scrolling card table hands it unbounded height and crops its
// tail (tagged) sections.
func (inst *PlayApp) renderDetailTab(rec arrow.RecordBatch, schema *arrow.Schema) {
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query, then select a row to see its detail.") {
			rt.Small().Weak()
		}
		return
	}
	reject := dispatchPanel(detailPanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.activeNodeID(), rec: rec, schema: schema, sig: inst.frameSig},
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

func (inst *PlayApp) renderMasterTable(rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, selectedRow int64, emit SignalEmitterI) {
	ids := inst.ids
	ncols := int(rec.NumCols())
	totalRows := rec.NumRows()

	// egui_table draws cell content flush to the cell edge ("Does not add any
	// margins to cells" — egui_table's own docs say to add them yourself). We
	// lead every header and body cell with a horizontal AddSpace so content
	// isn't jammed against the gridline or the neighbouring column's header
	// type string. The cell ui is laid out left-to-right, so AddSpace advances
	// the cursor along the row → a left inset. ensureColWidths reserves the
	// same amount so the inset doesn't eat into a column's fitted width.
	cellPadX := styletokens.PaddingTight(inst.density)
	if totalRows > numRows {
		totalRows = numRows
	}

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

	// Leading "#" selector column (click to select row) + the data columns.
	c.EtColumn(44.0).Resizable(false).Send()

	// Emit per-column widths from the schema-keyed cache. Resampling every
	// frame from the current page's content would reflow the table each
	// time the pager advances, since different pages have different string
	// lengths. The cache is invalidated when the Arrow *Schema pointer
	// changes, i.e. on a new query.
	inst.ensureColWidths(rec, schema, pageStart, pageEnd)
	for col := 0; col < ncols; col++ {
		c.EtColumn(inst.colWidths[col]).Resizable(true).Send()
	}

	et := c.EndETable(ids.PrepareStr("results"),
		uint64(displayRows),
		18.0, 1, 1).
		Striped(true)
	// Selection is stored as an absolute row index; translate to the
	// page-local row when highlighting so the stripe lands on the right line.
	if selectedRow >= pageStart && selectedRow < pageEnd {
		et = et.SelectedRow(uint64(selectedRow - pageStart))
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
			c.AddSpace(cellPadX)
			for rt := range c.RichTextLabel("#") {
				rt.Weak().Monospace()
			}
		}
	}
	for col := 0; col < ncols; col++ {
		if vis, _ := et.ColVisible(uint32(col + 1)); !vis {
			continue
		}
		for range et.Headers(0, uint32(col+1)) {
			c.AddSpace(cellPadX)
			field := schema.Field(col)
			for rt := range c.RichTextLabel(field.Name) {
				rt.Strong().Monospace()
			}
			for rt := range c.RichTextLabel(field.Type.String()) {
				rt.Small().Weak().Monospace()
			}
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
		absRow := pageStart + int64(local)
		selected := absRow == selectedRow
		rowBase := cellIdBase + uint64(absRow)*cellColStride

		if vis, _ := et.ColVisible(0); vis {
			for range et.Cells(local, 0) {
				c.AddSpace(cellPadX)
				marker := fmt.Sprintf("%d", absRow+1)
				if c.Button(ids.PrepareSeq(rowBase),
					c.Atoms().BeginRichText(marker).Monospace().End().Keep()).
					Frame(false).
					Selected(selected).
					Truncate().
					SendResp().HasPrimaryClicked() {
					emit.Emit(signalSelection, absRow)
				}
			}
		}
		for col := 0; col < ncols; col++ {
			colPlus1 := uint32(col + 1)
			if vis, _ := et.ColVisible(colPlus1); !vis {
				continue
			}
			for range et.Cells(local, colPlus1) {
				c.AddSpace(cellPadX)
				text := formatCell(rec, col, absRow)
				if c.Button(ids.PrepareSeq(rowBase+uint64(col)+1),
					c.Atoms().BeginRichText(text).Monospace().End().Keep()).
					Frame(false).
					Selected(selected).
					Truncate().
					SendResp().HasPrimaryClicked() {
					emit.Emit(signalSelection, absRow)
				}
			}
		}
	}
	et.Send()
}

// ensureColWidths samples per-column widths the first time a given schema
// is seen and caches them. Subsequent calls with the same schema are a
// cheap pointer compare. The sample window is the first colSampleRows rows
// of whichever page happens to be active when the cache gets populated —
// good enough for initial sizing; user resizes via drag persist separately
// in egui_table's own state.
func (inst *PlayApp) ensureColWidths(rec arrow.RecordBatch, schema *arrow.Schema, pageStart, pageEnd int64) {
	if schema == inst.colWidthsForSchema && len(inst.colWidths) == schema.NumFields() {
		return
	}
	ncols := schema.NumFields()
	widths := make([]float32, ncols)
	sampleN := pageEnd - pageStart
	if sampleN > colSampleRows {
		sampleN = colSampleRows
	}
	// Reserve the same left inset renderMasterTable leads each cell with, so a
	// padded cell doesn't truncate content that would otherwise fit.
	cellPadX := styletokens.PaddingTight(inst.density)
	for col := 0; col < ncols; col++ {
		maxChars := len(schema.Field(col).Name)
		for r := int64(0); r < sampleN; r++ {
			if n := len(formatCell(rec, col, pageStart+r)); n > maxChars {
				maxChars = n
			}
		}
		w := float32(maxChars)*colCharPx + 16.0 + cellPadX
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

func historyLabel(e HistoryEntry) string {
	sql := strings.ReplaceAll(e.SQL, "\n", " ")
	sql = strings.Join(strings.Fields(sql), " ")
	status := fmt.Sprintf("%dr %s",
		e.NumRows, e.Elapsed.Round(time.Millisecond))
	if e.ErrorText != "" {
		status = "ERR"
	}
	line := fmt.Sprintf("%s  %s  %s",
		e.Executed.Format("15:04:05"), status, sql)
	return truncateRunes(line, historyLabelChar)
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
