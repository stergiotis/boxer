package play

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Minimum number of rows required to compute a meaningful UMAP projection.
// UMAP needs NNeighbors+1 points minimum; below ~3 entities the layout is
// uninformative anyway.
const projectionMinRows = 3

// Maximum rows fed to UMAP in a single run. UMAP is O(n × NNeighbors) in
// memory and roughly O(n × NEpochs × NNeighbors) in time, so the n=2000
// limit that t-SNE forced is now an order of magnitude too pessimistic.
// 10k keeps fits under ~30 s on a modern laptop and the kNN graph well
// under 100 MB; results above are still subsampled uniformly and reported
// as "X of Y entities · sampled".
const projectionMaxRows = 10000

type projectorStatusE uint8

const (
	projectorStatusIdle projectorStatusE = iota
	projectorStatusExtracting
	projectorStatusRunning
	projectorStatusDone
	projectorStatusFailed
	projectorStatusCancelled
	// projectorStatusCancelling is the transient state between Cancel() and
	// the goroutine actually returning. UMAP's FitTransform has no per-epoch
	// hook so cancel-mid-fit takes effect only once FitTransform returns —
	// the UI sits in Cancelling for up to the remaining UMAP wall-clock.
	// Final transition Cancelling → Cancelled happens in markCancelled().
	projectorStatusCancelling
)

func (inst projectorStatusE) String() string {
	switch inst {
	case projectorStatusIdle:
		return "idle"
	case projectorStatusExtracting:
		return "extracting"
	case projectorStatusRunning:
		return "running"
	case projectorStatusDone:
		return "done"
	case projectorStatusFailed:
		return "failed"
	case projectorStatusCancelled:
		return "cancelled"
	case projectorStatusCancelling:
		return "cancelling"
	}
	return "?"
}

// projectorSnapshot is a value-copy of Projector state taken under mutex,
// safe to read on the render goroutine after the lock is released.
//
// coordRow maps each coords index → original record-batch row, so the
// selected-row highlight finds its point even when the projection was run
// over a uniform subsample. totalRows is the full input count (≥ len(coords));
// when they differ, the toolbar reports "X of Y · sampled".
//
// featureColumns is the per-feature value series for the projected sample,
// indexed [featureIdx][coordIdx]. Already log1p-transformed for features
// flagged in card.LogTransformFeature so the colour-bucketing range is
// honest for skewed distributions. Nil while a run is in progress; populated
// at the same time as coords / coordRow.
//
// startedAt is the wall-clock time the run kicked off; used to render
// "elapsed Xs" in the toolbar while UMAP runs (the upstream library has no
// per-epoch callback so a real progress bar / ETA is not exposed).
type projectorSnapshot struct {
	status         projectorStatusE
	coords         [][2]float64
	coordRow       []int64
	featureColumns [card.NumFeatures][]float64
	totalRows      int64
	err            error
	startedAt      time.Time
}

// Projector owns the UMAP projection state for the current result batch.
// A single goroutine runs feature extraction + UMAP in the background; the
// render thread polls Snapshot() each frame to draw progress / scatter.
//
// Lifecycle: Invalidate(schema, executed) is called every frame from the
// renderer; if the underlying result changed it cancels any in-flight run
// and resets to idle. Start(rec, schema, executed) spawns the goroutine
// (no-op if one is already running). Cancel() signals abort.
//
// Concurrency: all mutable fields are guarded by mu. The cancel chan is
// non-nil iff a goroutine is in flight; Start refuses while non-nil. The
// goroutine clears it on exit so the next Start can proceed.
type Projector struct {
	ids   *c.WidgetIdStack
	cards *CardDriver

	mu        sync.Mutex
	forSchema *arrow.Schema
	forExec   time.Time

	status         projectorStatusE
	coords         [][2]float64
	coordRow       []int64
	featureColumns [card.NumFeatures][]float64
	totalRows      int64
	err            error
	startedAt      time.Time

	cancel chan struct{}
}

// NewProjector binds the Projector to the play app's CardDriver. The Projector
// borrows the driver's streamreadaccess.Driver to feed the FeatureExtractor;
// it does not own the CardDriver and must not outlive it.
func NewProjector(ids *c.WidgetIdStack, cards *CardDriver) *Projector {
	return &Projector{
		ids:   ids,
		cards: cards,
	}
}

// Invalidate is called every frame with the current (schema, executed).
// Returns true iff the projection state matches the current result and may
// be displayed. If the result changed since the last call, any in-flight
// computation is cancelled and the state is reset to idle.
//
// Uses detachCurrentRunLocked (not signalCancelLocked) so a goroutine that
// is still winding down for the previous dataset becomes a no-op on its
// terminal status write — the new tab state (Idle) survives instead of
// being overwritten with the stale run's Cancelled / Failed / Done.
func (inst *Projector) Invalidate(schema *arrow.Schema, executed time.Time) (matches bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.forSchema == schema && inst.forExec.Equal(executed) {
		matches = true
		return
	}
	inst.detachCurrentRunLocked()
	inst.forSchema = schema
	inst.forExec = executed
	inst.status = projectorStatusIdle
	inst.coords = nil
	inst.coordRow = nil
	inst.featureColumns = [card.NumFeatures][]float64{}
	inst.totalRows = 0
	inst.err = nil
	inst.startedAt = time.Time{}
	matches = true
	return
}

// Start kicks off a projection run on the given record batch. No-op if a
// run is already in flight (Cancel first if you want to restart). The
// caller must have called Invalidate(schema, executed) earlier this frame
// so the cache key is set.
func (inst *Projector) Start(rec arrow.RecordBatch) {
	inst.mu.Lock()
	if inst.cancel != nil {
		inst.mu.Unlock()
		return
	}
	cancel := make(chan struct{})
	inst.cancel = cancel
	inst.status = projectorStatusExtracting
	inst.coords = nil
	inst.coordRow = nil
	inst.featureColumns = [card.NumFeatures][]float64{}
	inst.totalRows = 0
	inst.err = nil
	inst.startedAt = time.Now()
	inst.mu.Unlock()

	rec.Retain()
	go inst.run(rec, cancel)
}

// Cancel signals the in-flight run to stop. The goroutine sees the closed
// channel on the next stepFunc fire (or the next phase boundary) and exits.
// While the goroutine winds down (UMAP can take seconds with no per-epoch
// hook) the status sits at Cancelling so the UI can show that the click
// was registered. Final transition to Cancelled happens in markCancelled().
// No-op if nothing is running.
func (inst *Projector) Cancel() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.cancel == nil {
		return
	}
	switch inst.status {
	case projectorStatusExtracting, projectorStatusRunning:
		inst.status = projectorStatusCancelling
	}
	inst.signalCancelLocked()
}

// signalCancelLocked closes the cancel chan if it is open. Caller must hold
// mu. Leaves inst.cancel pointing at the chan so the goroutine recognises
// itself as the current run when it publishes terminal status — used by
// user-initiated Cancel(), which wants to see Cancelling → Cancelled.
func (inst *Projector) signalCancelLocked() {
	if inst.cancel == nil {
		return
	}
	select {
	case <-inst.cancel:
	default:
		close(inst.cancel)
	}
}

// detachCurrentRunLocked signals cancellation AND drops inst.cancel so an
// in-flight goroutine sees `inst.cancel != cancel` on its next mu-protected
// write and skips its terminal status update. Caller must hold mu. Used by
// Invalidate() when the underlying (schema, executed) changed: we want the
// new tab state to survive the late goroutine return.
func (inst *Projector) detachCurrentRunLocked() {
	inst.signalCancelLocked()
	inst.cancel = nil
}

// Snapshot returns a value-copy of the current state. Safe to read on the
// render thread without holding the mutex. The coords slice is shared (not
// copied) — the goroutine treats it as immutable once published.
func (inst *Projector) Snapshot() (snap projectorSnapshot) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	snap = projectorSnapshot{
		status:         inst.status,
		coords:         inst.coords,
		coordRow:       inst.coordRow,
		featureColumns: inst.featureColumns,
		totalRows:      inst.totalRows,
		err:            inst.err,
		startedAt:      inst.startedAt,
	}
	return
}

// run is the projection goroutine. Owns the rec.Retain() taken by Start and
// releases it on exit. Drives the FeatureExtractor → builds matrix →
// preprocesses → runs UMAP. UMAP has no per-epoch callback so cancellation
// only takes effect at phase boundaries; a Cancel mid-fit waits for
// FitTransform to return before being honored.
//
// Every publishing step (terminal status writes via fail/markCancelled,
// inline Running/Done writes) guards on `inst.cancel == cancel` so a run
// whose dataset was Invalidated out from under it cannot clobber the new
// tab state. The inline writes additionally guard on `!isClosed(cancel)`
// so a user Cancel between the prior isClosed check and the write doesn't
// clobber the new Cancelling status.
func (inst *Projector) run(rec arrow.RecordBatch, cancel chan struct{}) {
	defer rec.Release()
	defer inst.releaseRunLocked(cancel)

	driver := inst.cards.Driver()
	if driver == nil {
		inst.fail(cancel, eh.Errorf("projection: driver not available (schema not leeway-shaped)"))
		return
	}

	fe, err := card.NewFeatureExtractor()
	if err != nil {
		inst.fail(cancel, eh.Errorf("projection: feature extractor init: %w", err))
		return
	}
	err = driver.DriveRecordBatch(fe, rec)
	if err != nil {
		inst.fail(cancel, eh.Errorf("projection: feature extraction: %w", err))
		return
	}
	if isClosed(cancel) {
		inst.markCancelled(cancel)
		return
	}

	features := fe.Results()
	nRows := len(features)
	if nRows < projectionMinRows {
		inst.fail(cancel, eh.Errorf("projection: too few rows (%d, need ≥%d)", nRows, projectionMinRows))
		return
	}

	// Subsample to keep UMAP's k-NN graph + SGD layout in a sane wall-clock.
	// UMAP itself is O(n × NNeighbors) memory and roughly O(n × NEpochs ×
	// NNeighbors) time, so projectionMaxRows can be much higher than the
	// t-SNE-era cap. The mapping coordRow[i] → original row index is needed
	// by the renderer to highlight the currently-selected table row.
	sampledFeatures, coordRow := subsampleFeatures(features, projectionMaxRows)

	// Per-feature columns (post-log1p where flagged) are kept around so the
	// renderer can colour points by any feature without re-extracting. log1p
	// here mirrors what UMAP preprocessing does (z-score on log-space values
	// for skewed features); without it the colour scale on TotalAttributeCount
	// etc. would be dominated by the few largest values.
	featureColumns := buildFeatureColumns(sampledFeatures)

	inst.mu.Lock()
	if inst.cancel == cancel && !isClosed(cancel) {
		inst.totalRows = int64(nRows)
		inst.featureColumns = featureColumns
		inst.status = projectorStatusRunning
	}
	inst.mu.Unlock()

	m := card.BuildFeatureMatrix(sampledFeatures)
	err = card.PreprocessFeatureMatrix(m)
	if err != nil {
		inst.fail(cancel, eh.Errorf("projection: preprocess: %w", err))
		return
	}
	if isClosed(cancel) {
		inst.markCancelled(cancel)
		return
	}

	// nozzle/umap-go has no per-epoch callback so we cannot interrupt the
	// fit early — a Cancel mid-run lands here, after FitTransform returns,
	// and we discard the result.
	coords, err := card.RunUMAP(m, card.UMAPOptions{})
	if err != nil {
		inst.fail(cancel, eh.Errorf("projection: umap: %w", err))
		return
	}
	if isClosed(cancel) {
		inst.markCancelled(cancel)
		return
	}

	inst.mu.Lock()
	if inst.cancel == cancel && !isClosed(cancel) {
		inst.coords = coords
		inst.coordRow = coordRow
		inst.status = projectorStatusDone
	}
	inst.mu.Unlock()
}

// buildFeatureColumns extracts each EntityFeatures field into its own
// per-coord column, applying log1p to the features flagged in
// card.LogTransformFeature. The latter matches what the UMAP preprocessing
// does internally — colouring on raw values would let one heavy-tailed
// feature smear the whole scale into a single bucket.
func buildFeatureColumns(features []card.EntityFeatures) (cols [card.NumFeatures][]float64) {
	n := len(features)
	for fi := 0; fi < card.NumFeatures; fi++ {
		cols[fi] = make([]float64, n)
	}
	for ri := 0; ri < n; ri++ {
		s := features[ri].AsSlice()
		for fi := 0; fi < card.NumFeatures; fi++ {
			v := s[fi]
			if card.LogTransformFeature[fi] {
				if v < 0 {
					v = 0
				}
				v = math.Log1p(v)
			}
			cols[fi][ri] = v
		}
	}
	return
}

// subsampleFeatures returns up to maxRows uniformly-spaced rows from features
// plus the coordRow mapping (coordRow[i] = original index). When the input
// is already ≤ maxRows the input slice is returned as-is and coordRow is the
// identity mapping. The first and last rows are always retained so the sample
// covers the full input range.
func subsampleFeatures(features []card.EntityFeatures, maxRows int) (sampled []card.EntityFeatures, coordRow []int64) {
	n := len(features)
	if n <= maxRows {
		coordRow = make([]int64, n)
		for i := range features {
			coordRow[i] = int64(i)
		}
		sampled = features
		return
	}
	sampled = make([]card.EntityFeatures, maxRows)
	coordRow = make([]int64, maxRows)
	for i := 0; i < maxRows; i++ {
		idx := int64(i) * int64(n-1) / int64(maxRows-1)
		coordRow[i] = idx
		sampled[i] = features[idx]
	}
	return
}

// fail publishes a Failed terminal status — but only if the run is still
// current (inst.cancel == cancel). A goroutine whose dataset was Invalidated
// out from under it must not clobber the new tab state. The log line still
// fires either way so the failure leaves a trail.
func (inst *Projector) fail(cancel chan struct{}, err error) {
	log.Warn().Err(err).Msg("play: projection failed")
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.cancel != cancel {
		return
	}
	inst.err = err
	inst.status = projectorStatusFailed
}

// markCancelled publishes the Cancelled terminal status — same staleness
// guard as fail(). After Invalidate detaches the run, the goroutine's
// markCancelled is a no-op and the freshly-Idle tab state stays intact.
func (inst *Projector) markCancelled(cancel chan struct{}) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.cancel != cancel {
		return
	}
	inst.status = projectorStatusCancelled
	inst.coords = nil
}

func (inst *Projector) releaseRunLocked(cancel chan struct{}) {
	inst.mu.Lock()
	if inst.cancel == cancel {
		inst.cancel = nil
	}
	inst.mu.Unlock()
}

func isClosed(ch <-chan struct{}) (closed bool) {
	select {
	case <-ch:
		closed = true
	default:
	}
	return
}

// ============================================================================
// Rendering
// ============================================================================

const (
	projectionPointRadius  = 2.5
	projectionSelectRadius = 5.5
)

// projectionColorPoint / projectionColorSelected source from the IDS
// qualitative cycle (batlowS, Crameri MIT). Slot 0 for the default
// point, AccentDefault for selection (ADR-0031 §SD2 reserves accent
// for "selection, focus rings, branded highlights").
var (
	projectionColorPoint    = color.Hex(styletokens.QualitativeCycle(0).AsHex())
	projectionColorSelected = color.Hex(styletokens.AccentDefault.AsHex())
)

// projectionViridisBuckets is the bucket count for the value-by-color
// scatter overlay. 8 matches the original `colormap.Viridis8`
// cardinality; the IDS Sequential(SequentialViridis, t) accessor
// samples the same matplotlib lineage at the same 8 stops.
const projectionViridisBuckets = 8

// renderProjection draws the projection-mode UI. It picks the right state
// (idle / running / done / failed) from the Projector's snapshot. Caller
// is responsible for the surrounding container; this function emits widgets
// directly into the current ui scope.
func (inst *PlayApp) renderProjection(rec arrow.RecordBatch) {
	ids := inst.ids
	snap := inst.projector.Snapshot()
	nRows := rec.NumRows()

	// Mirror the projector's status (mutated under its internal mutex
	// from worker goroutines) into the render-thread-only fsmview.Machine.
	// Rules are pre-declared in newProjectorFSM and drive the drawn graph;
	// Mirror follows an undeclared path (logging it) instead of rejecting,
	// since a memoryless per-frame mirror would otherwise wedge a state
	// behind. The mirror falls one frame behind the projector but that's
	// imperceptible at 60 fps.
	if cur := inst.projFSM.Current(); cur != snap.status {
		if declared := inst.projFSM.Mirror(snap.status); !declared {
			log.Warn().
				Stringer("from", cur).
				Stringer("to", snap.status).
				Msg("play: projector FSM observed an undeclared edge (mirrored)")
		}
	}

	// Toolbar row: Compute / Cancel + status text. While cancelling, the
	// Cancel button is replaced by a muted "Cancelling…" label so the user
	// sees their click was registered — re-clicking would just signal an
	// already-closed channel, but the visual feedback gap (UMAP can take
	// seconds to return) confused users into thinking nothing happened.
	for range c.Horizontal().KeepIter() {
		switch snap.status {
		case projectorStatusExtracting, projectorStatusRunning:
			c.Spinner().Size(14).Send()
			if c.Button(ids.PrepareStr("projectionCancel"),
				c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.projector.Cancel()
			}
		case projectorStatusCancelling:
			c.Spinner().Size(14).Send()
			for rt := range c.RichTextLabel("Cancelling…") {
				rt.Weak()
			}
		default:
			label := "Compute projection"
			if snap.status == projectorStatusDone {
				label = "Recompute"
			}
			if c.Button(ids.PrepareStr("projectionCompute"),
				c.Atoms().Text(label).Keep()).
				SendResp().HasPrimaryClicked() {
				inst.projector.Start(rec)
			}
		}
		c.Separator().Vertical().Send()
		c.Label(formatEntityCountLabel(nRows, snap)).Send()
		c.Separator().Vertical().Send()
		// fsmview chip + popup — clicking the chip pops the full
		// projector lifecycle (table / graph / history) so the operator
		// can see what states are reachable from Here and how often the
		// projector has cycled in this session.
		inst.projFSMWidget.Render()
		if snap.status == projectorStatusDone {
			c.Separator().Vertical().Send()
			inst.renderColorByCombo()
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabel(
				fmt.Sprintf("%d-D → 2-D · UMAP · n_neighbors=%d · min_dist=%.2g",
					card.NumFeatures,
					card.DefaultUMAPNNeighbors,
					card.DefaultUMAPMinDist)) {
				rt.Small().Weak()
			}
		} else if nRows > projectionMaxRows {
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabel(
				fmt.Sprintf("will sample %d of %d (UMAP wall-clock cap)",
					projectionMaxRows, nRows)) {
				rt.Small().Weak()
			}
		}
	}

	switch snap.status {
	case projectorStatusIdle:
		if nRows < projectionMinRows {
			for rt := range c.RichTextLabel(
				fmt.Sprintf("Need ≥%d entities to project (have %d).",
					projectionMinRows, nRows)) {
				rt.Small().Weak()
			}
		} else {
			for rt := range c.RichTextLabel(
				"Click Compute to project the result set into 2-D.") {
				rt.Small().Weak()
			}
		}
	case projectorStatusExtracting, projectorStatusRunning, projectorStatusCancelling:
		for range c.Horizontal().KeepIter() {
			c.Spinner().Size(16).Send()
			c.Label(formatRunningLabel(snap)).Send()
		}
	case projectorStatusFailed:
		c.Label(fmt.Sprintf("Projection failed: %s", snap.err)).Wrap().Send()
	case projectorStatusCancelled:
		for rt := range c.RichTextLabel("Projection cancelled.") {
			rt.Small().Weak()
		}
	}

	if snap.status == projectorStatusDone && len(snap.coords) > 0 {
		if newRow, ok := renderProjectionPlot(ids, snap, inst.selectedRow, inst.colorByFeature); ok {
			inst.selectedRow = newRow
		}
	}
}

// renderColorByCombo emits the "Colour by …" picker into the current
// horizontal layout. "Monochrome" maps to colorByFeature=-1; otherwise the
// selected option indexes card.FeatureNames(). Selection persists on the
// PlayApp across recomputes so the user's chosen colouring sticks.
func (inst *PlayApp) renderColorByCombo() {
	ids := inst.ids
	names := card.FeatureNames()
	curLabel := "monochrome"
	if inst.colorByFeature >= 0 && int(inst.colorByFeature) < len(names) {
		curLabel = names[inst.colorByFeature]
	}
	for range c.ComboBox(ids.PrepareStr("colorBy"),
		c.WidgetText().Text("colour by").Keep(),
		c.WidgetText().Text(curLabel).Keep()).
		KeepIter() {
		if c.Button(ids.PrepareSeq(0x3000),
			c.Atoms().Text("monochrome").Keep()).
			Frame(false).
			Selected(inst.colorByFeature == -1).
			SendResp().HasPrimaryClicked() {
			inst.colorByFeature = -1
		}
		for i, name := range names {
			selected := int(inst.colorByFeature) == i
			if c.Button(ids.PrepareSeq(uint64(0x3001+i)),
				c.Atoms().Text(name).Keep()).
				Frame(false).
				Selected(selected).
				SendResp().HasPrimaryClicked() {
				inst.colorByFeature = int8(i)
			}
		}
	}
}

// formatEntityCountLabel renders the toolbar text describing how many rows
// the run did/will project. When the result was subsampled the label is
// "X of Y · sampled" so the user can tell the projection is partial.
func formatEntityCountLabel(nRows int64, snap projectorSnapshot) (label string) {
	switch snap.status {
	case projectorStatusDone:
		if snap.totalRows > int64(len(snap.coords)) {
			label = fmt.Sprintf("%d of %d entities · sampled",
				len(snap.coords), snap.totalRows)
			return
		}
		label = fmt.Sprintf("%d entities", len(snap.coords))
	default:
		label = fmt.Sprintf("%d entities", nRows)
	}
	return
}

// formatRunningLabel renders the spinner-adjacent label while a projection
// is in flight. UMAP has no per-epoch hook so we cannot render iter/N or
// ETA; wall-clock elapsed since Start is the most we can honestly show.
func formatRunningLabel(snap projectorSnapshot) (label string) {
	elapsed := time.Duration(0)
	if !snap.startedAt.IsZero() {
		elapsed = time.Since(snap.startedAt).Round(100 * time.Millisecond)
	}
	switch snap.status {
	case projectorStatusExtracting:
		label = fmt.Sprintf("extracting features · %s", elapsed)
	case projectorStatusRunning:
		label = fmt.Sprintf("running UMAP · %s", elapsed)
	case projectorStatusCancelling:
		label = fmt.Sprintf("cancelling · waiting for UMAP to return · %s", elapsed)
	default:
		label = snap.status.String()
	}
	return
}

// renderProjectionPlot emits the scatter and the enclosing Plot. When
// colorByFeature is in [0, NumFeatures) points are bucketed into 8 viridis
// bins by min-max-normalised value of that feature; otherwise the whole
// series is a single monochrome scatter. The selected-row marker is drawn
// last (egui_plot z-orders by emit order) so it sits on top of any colour
// bucket.
//
// snap.coordRow maps each coord index → original record-batch row, used
// both to locate the selected row inside a sample and to translate clicks
// back to the row index.
//
// Returns (newSelectedRow, true) iff the user primary-clicked inside the
// plot. The hit is the nearest-by-euclidean-distance point in plot-data
// coordinates; with well-separated clusters this is the obvious cluster
// member, but a click in empty space still snaps to the closest point (no
// max-distance threshold). One-frame lag inherited from FetchR15PlotPointer.
func renderProjectionPlot(ids *c.WidgetIdStack, snap projectorSnapshot, selectedRow int64, colorByFeature int8) (newSelectedRow int64, hit bool) {
	coords := snap.coords
	coordRow := snap.coordRow

	useColor := colorByFeature >= 0 && int(colorByFeature) < card.NumFeatures &&
		len(snap.featureColumns[colorByFeature]) == len(coords)
	if useColor {
		emitBucketedScatters(coords, snap.featureColumns[colorByFeature],
			card.FeatureNames()[colorByFeature])
	} else {
		xs := make([]float64, len(coords))
		ys := make([]float64, len(coords))
		for i, p := range coords {
			xs[i] = p[0]
			ys[i] = p[1]
		}
		c.PlotScatter("entities", xs, ys).
			Color(projectionColorPoint).
			Radius(projectionPointRadius).
			Shape(0).
			Filled(true).
			Send()
	}

	if selectedRow >= 0 {
		for i, row := range coordRow {
			if row == selectedRow {
				sel := coords[i]
				c.PlotScatter("selected",
					[]float64{sel[0]}, []float64{sel[1]}).
					Color(projectionColorSelected).
					Radius(projectionSelectRadius).
					Shape(1).
					Filled(true).
					Send()
				break
			}
		}
	}
	// No Width/Height/ViewAspect/DataAspect: the Rust handler defaults all
	// four to None, which makes egui_plot use ui.available_size() — the
	// plot fills the remaining panel area in both axes. Constraining the
	// aspect (DataAspect(1.0)) would preserve cluster shapes but letterbox
	// one axis; for an exploratory view we prefer the greedy fill.
	resp := c.Plot(ids.PrepareStr("projectionPlot")).
		AllowZoom(true).
		AllowDrag(true).
		AllowBoxedZoom(true).
		ShowGrid(true, true).
		Legend().
		SendResp()
	if !resp.Clicked {
		return
	}
	bestI := 0
	dx0 := coords[0][0] - resp.X
	dy0 := coords[0][1] - resp.Y
	bestD := dx0*dx0 + dy0*dy0
	for i := 1; i < len(coords); i++ {
		dx := coords[i][0] - resp.X
		dy := coords[i][1] - resp.Y
		d := dx*dx + dy*dy
		if d < bestD {
			bestI, bestD = i, d
		}
	}
	newSelectedRow = coordRow[bestI]
	hit = true
	return
}

// emitBucketedScatters partitions points by feature value into len(Viridis8)
// equal-width bins (min-max scale) and emits one PlotScatter per non-empty
// bucket. Each series is named with the bucket's value range so the egui_plot
// legend doubles as a colour-scale legend. Constant columns (max == min)
// fall back to a single bucket — equivalent to monochrome but using the
// palette's lowest stop.
func emitBucketedScatters(coords [][2]float64, values []float64, featureName string) {
	if len(values) == 0 {
		return
	}
	mn, mx := values[0], values[0]
	for _, v := range values[1:] {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	span := mx - mn
	nBuckets := projectionViridisBuckets
	bucketXs := make([][]float64, nBuckets)
	bucketYs := make([][]float64, nBuckets)
	for i, v := range values {
		idx := 0
		if span > 1e-12 {
			idx = int((v - mn) / span * float64(nBuckets-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= nBuckets {
				idx = nBuckets - 1
			}
		}
		bucketXs[idx] = append(bucketXs[idx], coords[i][0])
		bucketYs[idx] = append(bucketYs[idx], coords[i][1])
	}
	for b := 0; b < nBuckets; b++ {
		if len(bucketXs[b]) == 0 {
			continue
		}
		var lo, hi float64
		if span > 1e-12 {
			lo = mn + span*float64(b)/float64(nBuckets-1)
			hi = mn + span*float64(b+1)/float64(nBuckets-1)
		} else {
			lo, hi = mn, mn
		}
		name := fmt.Sprintf("%s [%.2g, %.2g]", featureName, lo, hi)
		bucketT := float32(b) / float32(nBuckets-1)
		bucketColor := color.Hex(styletokens.Sequential(styletokens.SequentialViridis, bucketT).AsHex())
		c.PlotScatter(name, bucketXs[b], bucketYs[b]).
			Color(bucketColor).
			Radius(projectionPointRadius).
			Shape(0).
			Filled(true).
			Send()
	}
}
