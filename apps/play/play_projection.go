package play

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

// play_projection.go is the Projection tab (ADR-0230 §SD4): the result's
// leeway-card features become a neighbour graph in the analytics engine, the
// graph is laid out live by graphview under the neighbour-embedding force
// model, HDBSCAN over the same graph colours it, and the labels are read back
// off the features as rules (ADR-0235, play_projection_explain.go). The
// background goroutine owns the engine calls; the render thread owns the
// widget, which owns the positions. Nothing here computes a coordinate.

// Minimum number of rows required to compute a meaningful projection: the
// producer wants at least one neighbour per row, and below a handful of
// entities the picture says nothing.
const projectionMinRows = 3

// Maximum rows fed to the producer in a single run. The exact k-NN is
// O(n²·d) and the force step O(n log n) per iteration, both interactive at
// ten thousand rows on one machine (ADR-0230 §SD1); results above are
// subsampled uniformly and reported as "X of Y entities · sampled".
const projectionMaxRows = 10000

// projectionParams are the run's knobs, read by the goroutine at Start.
type projectionParams struct {
	// K is the neighbour count of the graph (ADR-0230 §SD1); the umap-learn
	// n_neighbors is K+1.
	K int
	// MinClusterSize is HDBSCAN's one parameter (ADR-0230 §SD3).
	MinClusterSize int
	// FeatureSet is the matrix the neighbour graph is built over (ADR-0238).
	FeatureSet projectionFeatureSetE
}

// projectionFeatureSetE names a feature set the lane can project.
type projectionFeatureSetE uint8

const (
	// projectionFeatureShape is the sixteen shape features of
	// card.EntityFeatures under Euclidean distance: how big and how skewed
	// a record is.
	projectionFeatureShape projectionFeatureSetE = iota
	// projectionFeatureStructure is the hashed structural identity of
	// card.StructureMatrix under cosine distance: which sections and
	// attributes a record has.
	projectionFeatureStructure
	// projectionFeatureComponents is one column per registered component
	// kind, 1 where the record carries it, under cosine distance: the
	// archetype (ADR-0146 D5) as a vector.
	projectionFeatureComponents
)

func (inst projectionFeatureSetE) String() string {
	switch inst {
	case projectionFeatureShape:
		return "shape"
	case projectionFeatureStructure:
		return "structure"
	case projectionFeatureComponents:
		return "components"
	}
	return "?"
}

// Doc is the one-line reading of the feature set the picker shows.
func (inst projectionFeatureSetE) Doc() string {
	switch inst {
	case projectionFeatureShape:
		return "shape — the sixteen size and skew features, Euclidean"
	case projectionFeatureStructure:
		return "structure — which sections and attributes a record has, hashed, cosine"
	case projectionFeatureComponents:
		return "components — which registered component kinds a record carries, one-hot, cosine"
	}
	return "?"
}

var projectionFeatureSets = [...]projectionFeatureSetE{projectionFeatureShape, projectionFeatureStructure, projectionFeatureComponents}

const (
	projectionDefaultK              = 15
	projectionDefaultMinClusterSize = 10
	// projectionExaggerationStart and projectionExaggerationSteps are the
	// annealing schedule (ADR-0230 §SD2): t-SNE's early exaggeration of 12,
	// lowered geometrically to the slider's value over the first steps
	// after a run.
	projectionExaggerationStart = 12
	projectionExaggerationSteps = 250
	// projectionFastForward is the step backlog run before the first paint
	// of a new graph, so the picture opens past the schedule's noisiest
	// stretch without stalling the frame for the whole schedule.
	projectionFastForward = 100
	// projectionFreezeSteps is the play panel's freeze rule (ADR-0227 play
	// panel §SD10): a layout that has not settled by then is held.
	projectionFreezeSteps = 4000
	// projectionNoiseAuraFloor drops a member from its cluster's aura when
	// HDBSCAN's probability falls under it — the bridge-point reading
	// ADR-0230 §SD3's update records.
	projectionNoiseAuraFloor = 0.1
	projectionNodeRadius     = 3
)

type projectorStatusE uint8

const (
	projectorStatusIdle projectorStatusE = iota
	projectorStatusExtracting
	projectorStatusRunning
	projectorStatusDone
	projectorStatusFailed
	projectorStatusCancelled
	// projectorStatusCancelling is the transient state between Cancel() and
	// the goroutine actually returning. The producer checks its context per
	// chunk of rows, so the wait is short; the state exists so the click is
	// seen to register. Final transition Cancelling → Cancelled happens in
	// markCancelled().
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

// projectionResult is what one run produces, immutable once published.
type projectionResult struct {
	graph    knn.Result
	clusters algo.HDBSCANResult
	// rows maps a graph slot to its original record-batch row, so the
	// selected-row highlight finds its node even when the run was over a
	// uniform subsample, and a node click translates back to the row.
	rows []int64
	// featureColumns is the per-feature value series per slot, indexed
	// [featureIdx][slot], log1p-transformed where card.LogTransformFeature
	// says so, for the colour bucketing.
	featureColumns [card.NumFeatures][]float64
	params         projectionParams
	// explanation is the labels read back off the features (ADR-0235):
	// a threshold tree and per-cluster contrasts, or why there is none.
	explanation projectionExplanation
	// slotFeatures is the raw features per slot, what the explanation was
	// fitted on and what a publish writes out.
	slotFeatures []card.EntityFeatures
}

// projectorSnapshot is a value-copy of Projector state taken under mutex,
// safe to read on the render goroutine after the lock is released. result
// is nil until Done. version counts published results so the render side
// rebuilds its declaration once per run, not per frame.
type projectorSnapshot struct {
	status    projectorStatusE
	result    *projectionResult
	version   uint64
	totalRows int64
	err       error
	startedAt time.Time
}

// Projector owns the projection state for the current result batch. A
// single goroutine runs feature extraction, the neighbour graph and the
// clustering in the background; the render thread polls Snapshot() each
// frame and drives the widget.
//
// Lifecycle: Invalidate(schema, executed) is called every frame from the
// renderer; if the underlying result changed it cancels any in-flight run
// and resets to idle. Start(rec) spawns the goroutine (no-op if one is
// already running). Cancel() signals abort.
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

	status    projectorStatusE
	result    *projectionResult
	version   uint64
	totalRows int64
	err       error
	startedAt time.Time
	params    projectionParams

	cancel chan struct{}

	// Render-thread state: the widget and the declaration cached against
	// the snapshot version and the colouring.
	view         *graphview.View
	nodes        graphview.NodeColumns
	edges        graphview.EdgeColumns
	builtVersion uint64
	builtColorBy int8
	builtAuras   bool
	auras        bool
	showEdges    bool
	exaggeration float64
	// kKnob, mcsKnob and explainDepthKnob back the integer sliders. A
	// slider's value is written at frame-end Sync into the pointer bound
	// at render, so the binding must outlive the frame: a local re-derived
	// from the int each frame is written after it is read and never
	// changes. The ints are read off these after the bind.
	kKnob, mcsKnob   float64
	explainDepthKnob float64
	// explainDepth is the cut the explanation's rules are read at;
	// explainPerCluster reads each cluster's own tree against the rest
	// rather than the one partition.
	explainDepth      int
	explainPerCluster bool
	// explainByItems reads the clusters against the card's attributes
	// rather than the features (play_projection_items.go).
	explainByItems bool
	// copyText hands a rule to the clipboard; nil when the app has none.
	copyText func(string)
	// renderPublish draws the app's publish affordance in the layout row.
	renderPublish func()
	// componentPresence detects the registered component kinds per row of
	// a result (componentDetail.presenceRows); nil when the app has none.
	componentPresence func(rec arrow.RecordBatch) (kinds []string, rows [][]int32, err error)
	paused            bool
	frozen            bool
	paneW, paneH      float32
	lastSelected      int64
	// idSeed keeps two live PlayApps' pane probes apart, as the graph
	// panels' does.
	idSeed uint64
}

// NewProjector binds the Projector to the play app's CardDriver. The Projector
// borrows the driver's streamreadaccess.Driver to feed the FeatureExtractor;
// it does not own the CardDriver and must not outlive it.
func NewProjector(ids *c.WidgetIdStack, cards *CardDriver) *Projector {
	return &Projector{
		ids:   ids,
		cards: cards,
		params: projectionParams{
			K:              projectionDefaultK,
			MinClusterSize: projectionDefaultMinClusterSize,
		},
		view: graphview.New(ids, "play-projection", graphview.Options{
			Layout:        graphview.LayoutForceDirected,
			NodeClicking:  true,
			NodeSelection: true,
		}),
		auras:             true,
		exaggeration:      1,
		kKnob:             projectionDefaultK,
		mcsKnob:           projectionDefaultMinClusterSize,
		explainDepthKnob:  projectionExplainDefaultDepth,
		explainDepth:      projectionExplainDefaultDepth,
		explainPerCluster: true,
		builtColorBy:      -2,
		lastSelected:      -1,
		idSeed:            nextVizSeed(),
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
	inst.result = nil
	inst.totalRows = 0
	inst.err = nil
	inst.startedAt = time.Time{}
	matches = true
	return
}

// Start kicks off a run on the given record batch with the current
// parameters. No-op if a run is already in flight (Cancel first if you want
// to restart). The caller must have called Invalidate(schema, executed)
// earlier this frame so the cache key is set.
func (inst *Projector) Start(rec arrow.RecordBatch) {
	inst.mu.Lock()
	if inst.cancel != nil {
		inst.mu.Unlock()
		return
	}
	cancel := make(chan struct{})
	inst.cancel = cancel
	inst.status = projectorStatusExtracting
	inst.result = nil
	inst.totalRows = 0
	inst.err = nil
	inst.startedAt = time.Now()
	params := inst.params
	inst.mu.Unlock()

	rec.Retain()
	go inst.run(rec, cancel, params)
}

// Cancel signals the in-flight run to stop. The goroutine sees the closed
// channel at its next context check and exits. While it winds down the
// status sits at Cancelling so the UI can show that the click was
// registered. Final transition to Cancelled happens in markCancelled().
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

// Detach cancels any in-flight run and orphans it — Invalidate's semantics
// without installing a new dataset. For app teardown (PlayApp.Close): the
// winding-down goroutine's terminal writes become no-ops and it releases its
// retained record on exit.
func (inst *Projector) Detach() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.detachCurrentRunLocked()
}

// Snapshot returns a value-copy of the current state. Safe to read on the
// render thread without holding the mutex. The result is shared (not
// copied) — the goroutine treats it as immutable once published.
func (inst *Projector) Snapshot() (snap projectorSnapshot) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	snap = projectorSnapshot{
		status:    inst.status,
		result:    inst.result,
		version:   inst.version,
		totalRows: inst.totalRows,
		err:       inst.err,
		startedAt: inst.startedAt,
	}
	return
}

// run is the projection goroutine. Owns the rec.Retain() taken by Start and
// releases it on exit. Drives the FeatureExtractor → builds the matrix →
// preprocesses → neighbour graph → HDBSCAN. The engine calls take a context
// cancelled by the cancel chan, so a Cancel lands within a chunk of rows.
//
// Every publishing step (terminal status writes via fail/markCancelled,
// inline Running/Done writes) guards on `inst.cancel == cancel` so a run
// whose dataset was Invalidated out from under it cannot clobber the new
// tab state. The inline writes additionally guard on `!isClosed(cancel)`
// so a user Cancel between the prior isClosed check and the write doesn't
// clobber the new Cancelling status.
func (inst *Projector) run(rec arrow.RecordBatch, cancel chan struct{}, params projectionParams) {
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

	// The item sets ride on a second pass with their own sink; a failure
	// there costs the attribute reading, not the run.
	ie := card.NewItemExtractor()
	itemErr := driver.DriveRecordBatch(ie, rec)
	if isClosed(cancel) {
		inst.markCancelled(cancel)
		return
	}
	// The registered components each row carries join the item sets, and
	// are the components feature set on their own (ADR-0238 update).
	var compKinds []string
	var compRows [][]int32
	var compErr error
	if inst.componentPresence != nil {
		compKinds, compRows, compErr = inst.componentPresence(rec)
		if compErr != nil {
			log.Debug().Err(compErr).Msg("play: projection: component presence unavailable")
		}
	}
	allItems := withComponentItems(ie.Results(), compKinds, compRows)

	features := fe.Results()
	nRows := len(features)
	if nRows < projectionMinRows {
		inst.fail(cancel, eb.Build().Int("rows", nRows).Int("need", projectionMinRows).Errorf("projection: too few rows"))
		return
	}

	// Subsample to keep the exact k-NN's O(n²) in a sane wall-clock. The
	// mapping sampleRow[i] → original row index is needed by the renderer
	// to highlight the currently-selected table row.
	sampledFeatures, sampleRow := subsampleFeatures(features, projectionMaxRows)
	featureColumns := buildFeatureColumns(sampledFeatures)

	inst.mu.Lock()
	if inst.cancel == cancel && !isClosed(cancel) {
		inst.totalRows = int64(nRows)
		inst.status = projectorStatusRunning
	}
	inst.mu.Unlock()

	// The matrix the graph is built over is the run's choice (ADR-0238):
	// the shape features, preprocessed, under Euclidean distance; or the
	// hashed structural identity of the item sets under cosine.
	var x []float32
	var d int
	metric := knn.MetricEuclidean
	n := len(sampledFeatures)
	switch params.FeatureSet {
	case projectionFeatureStructure:
		if itemErr != nil {
			inst.fail(cancel, eh.Errorf("projection: item extraction: %w", itemErr))
			return
		}
		sampleIdx := make([]int, n)
		for i, r := range sampleRow {
			sampleIdx[i] = int(r)
		}
		d = card.StructureDims
		x = card.StructureMatrix(allItems.Select(sampleIdx), d)
		metric = knn.MetricCosine
	case projectionFeatureComponents:
		if compErr != nil {
			inst.fail(cancel, eh.Errorf("projection: component presence: %w", compErr))
			return
		}
		if len(compKinds) == 0 {
			inst.fail(cancel, eh.Errorf("projection: no registered component reads this result — the components set needs a facts-shaped result"))
			return
		}
		d = len(compKinds)
		x = make([]float32, n*d)
		for i, r := range sampleRow {
			for _, k := range compRows[r] {
				x[i*d+int(k)] = 1
			}
		}
		metric = knn.MetricCosine
	default:
		m := card.BuildFeatureMatrix(sampledFeatures)
		err = card.PreprocessFeatureMatrix(m)
		if err != nil {
			inst.fail(cancel, eh.Errorf("projection: preprocess: %w", err))
			return
		}
		_, d = m.Dims()
		x = make([]float32, n*d)
		raw := m.RawMatrix()
		for i := range n {
			for j := range d {
				x[i*d+j] = float32(raw.Data[i*raw.Stride+j])
			}
		}
	}
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i) + 1 // slot ids; ascending, so slot == sample index
	}

	// The engine calls take a context; the cancel chan feeds it.
	ctx, cancelCtx := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		select {
		case <-cancel:
			cancelCtx()
		case <-done:
		}
	}()
	defer close(done)
	defer cancelCtx()

	g, err := knn.Build(ctx, nil, x, d, ids, knn.Options{K: params.K, Metric: metric})
	if err != nil {
		if isClosed(cancel) {
			inst.markCancelled(cancel)
			return
		}
		inst.fail(cancel, eh.Errorf("projection: neighbour graph: %w", err))
		return
	}
	dg, err := g.DistanceGraph()
	if err != nil {
		inst.fail(cancel, eh.Errorf("projection: distance graph: %w", err))
		return
	}
	cl, err := algo.HDBSCAN(ctx, dg, g.CoreDist, algo.HDBSCANOptions{MinClusterSize: params.MinClusterSize})
	if err != nil {
		inst.fail(cancel, eh.Errorf("projection: clustering: %w", err))
		return
	}
	if isClosed(cancel) || cl.Truncation.Truncated {
		inst.markCancelled(cancel)
		return
	}

	// Slot order is id order is sample order here, so the slot→row mapping
	// composes the producer's Rows with the subsample's.
	res := &projectionResult{graph: g, clusters: cl, params: params}
	nSlots := g.Graph.NumVertices()
	res.rows = make([]int64, nSlots)
	for s := range nSlots {
		res.rows[s] = sampleRow[g.Rows[s]]
	}
	for f := range card.NumFeatures {
		col := make([]float64, nSlots)
		for s := range nSlots {
			col[s] = featureColumns[f][g.Rows[s]]
		}
		res.featureColumns[f] = col
	}
	// The explanation reads the raw features in slot order (ADR-0235 §SD3).
	// A cancel during it truncates rather than fails: the graph and the
	// clusters are already there to show.
	slotFeatures := make([]card.EntityFeatures, nSlots)
	for s := range nSlots {
		slotFeatures[s] = sampledFeatures[g.Rows[s]]
	}
	res.slotFeatures = slotFeatures
	res.explanation = explainProjection(ctx, slotFeatures, cl)
	if itemErr != nil {
		res.explanation.items.err = itemErr
	} else {
		slotIdx := make([]int, nSlots)
		for s := range nSlots {
			slotIdx[s] = int(sampleRow[g.Rows[s]])
		}
		res.explanation.items = explainItems(ctx, allItems.Select(slotIdx), len(allItems.Items), cl, params.MinClusterSize)
	}
	if isClosed(cancel) {
		inst.markCancelled(cancel)
		return
	}

	inst.mu.Lock()
	if inst.cancel == cancel && !isClosed(cancel) {
		inst.result = res
		inst.version++
		inst.status = projectorStatusDone
	}
	inst.mu.Unlock()
}

// buildFeatureColumns extracts each EntityFeatures field into its own
// per-sample column, applying log1p to the features flagged in
// card.LogTransformFeature. The latter matches what the preprocessing does
// internally — colouring on raw values would let one heavy-tailed feature
// smear the whole scale into a single bucket.
func buildFeatureColumns(features []card.EntityFeatures) (cols [card.NumFeatures][]float64) {
	n := len(features)
	for fi := range card.NumFeatures {
		cols[fi] = make([]float64, n)
	}
	for ri := range n {
		s := features[ri].AsSlice()
		for fi := range card.NumFeatures {
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
// plus the sampleRow mapping (sampleRow[i] = original index). When the input
// is already ≤ maxRows the input slice is returned as-is and sampleRow is the
// identity mapping. The first and last rows are always retained so the sample
// covers the full input range.
func subsampleFeatures(features []card.EntityFeatures, maxRows int) (sampled []card.EntityFeatures, sampleRow []int64) {
	n := len(features)
	if n <= maxRows {
		sampleRow = make([]int64, n)
		for i := range features {
			sampleRow[i] = int64(i)
		}
		sampled = features
		return
	}
	sampled = make([]card.EntityFeatures, maxRows)
	sampleRow = make([]int64, maxRows)
	for i := range maxRows {
		idx := int64(i) * int64(n-1) / int64(maxRows-1)
		sampleRow[i] = idx
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
	inst.result = nil
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

// projectionColorPoint / projectionColorSelected source from the IDS
// qualitative cycle (Okabe-Ito, ADR-0156). Slot 0 for the default node
// fill; selection is the widget's own highlight.
var projectionColorPoint = color.Hex(styletokens.QualitativeCycle(0).AsHex())

// projectionViridisBuckets is the bucket count for the colour-by-feature
// fill: the IDS Sequential(SequentialViridis, t) accessor sampled at 8
// stops, the `colormap.Viridis8` cardinality the former scatter used.
const projectionViridisBuckets = 8

// projectionIDSalt namespaces the pane probe — distinct from the graph
// panels' so the drawings never collide.
const projectionIDSalt uint64 = 0x9401ec7104e9a11e

// renderProjection draws the Projection tab. It picks the right state
// (idle / running / done / failed) from the Projector's snapshot; when done
// it declares the neighbour graph into the widget. Caller is responsible for
// the surrounding container; this function emits widgets directly into the
// current ui scope.
func (inst *PlayApp) renderProjection(rec arrow.RecordBatch, selectedRow int64, emit SignalEmitterI) {
	ids := inst.ids
	p := inst.projector
	snap := p.Snapshot()
	nRows := rec.NumRows()

	// Mirror the projector's status (mutated under its internal mutex
	// from worker goroutines) into the render-thread-only fsmview.Machine.
	// Rules are pre-declared in newProjectorFSM and drive the drawn graph;
	// mirrorObservedFSM (play_querystate.go) follows an undeclared path
	// instead of rejecting, since a memoryless per-frame mirror would
	// otherwise wedge a state behind, and grades it for the log — a stage
	// that finished inside one frame is a skip, not a surprise. The mirror
	// falls one frame behind the projector but that's imperceptible at 60 fps.
	mirrorObservedFSM(inst.projFSM, snap.status, "projector")

	// Toolbar row: Compute / Cancel + status text. While cancelling, the
	// Cancel button is replaced by a muted "Cancelling…" label so the user
	// sees their click was registered.
	for range c.Horizontal().KeepIter() {
		switch snap.status {
		case projectorStatusExtracting, projectorStatusRunning:
			c.Spinner().Size(14).Send()
			if c.Button(ids.PrepareStr("projectionCancel"),
				c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				p.Cancel()
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
				p.Start(rec)
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
		c.Separator().Vertical().Send()
		// The run's knobs apply on the next Compute; the layout's apply live.
		c.SliderF64(ids.PrepareStr("projectionK"), p.kKnob, 2, 50).Integer().Text("neighbours").SendRespVal(&p.kKnob)
		p.params.K = int(math.Round(p.kKnob))
		c.SliderF64(ids.PrepareStr("projectionMCS"), p.mcsKnob, 2, 100).Integer().Text("min cluster").SendRespVal(&p.mcsKnob)
		p.params.MinClusterSize = int(math.Round(p.mcsKnob))
		p.renderFeatureSetCombo()
		if snap.status == projectorStatusDone {
			c.Separator().Vertical().Send()
			inst.renderColorByCombo()
		} else if nRows > projectionMaxRows {
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabel(
				fmt.Sprintf("will sample %s of %s (exact k-NN cap)",
					humanize.Comma(projectionMaxRows), humanize.Comma(nRows))) {
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
				"Click Compute to build the result's neighbour graph and lay it out.") {
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

	inst.syncProjectionPublish()
	if snap.status == projectorStatusDone && snap.result != nil {
		p.copyText = nil
		if inst.CanCopy() {
			p.copyText = inst.copyToClipboard
		}
		// The publish affordance sits at the end of the layout controls,
		// the row with room; it needs the app's bus, so the app renders it.
		res := snap.result
		p.renderPublish = func() {
			inst.renderProjectionPublish(func() projectionPublishInput {
				_, x, y := p.view.PositionColumns(nil, nil, nil)
				return projectionPublishInput{
					rec: rec, res: res, depth: p.explainDepth, perCluster: p.explainPerCluster, x: x, y: y,
				}
			})
		}
		p.renderGraph(snap, selectedRow, inst.colorByFeature, emit)
	}
}

// renderGraph declares the run's neighbour graph into the widget and draws
// it: the layout controls, the status line, the canvas last. The
// declaration is rebuilt once per run and once per colouring change; the
// widget keeps the positions across frames.
func (inst *Projector) renderGraph(snap projectorSnapshot, selectedRow int64, colorBy int8, emit SignalEmitterI) {
	ids := inst.ids
	res := snap.result
	if inst.builtVersion != snap.version || inst.builtColorBy != colorBy || inst.builtAuras != inst.auras {
		fresh := inst.builtVersion != snap.version
		buildProjectionDeclaration(res, colorBy, inst.auras, &inst.nodes, &inst.edges)
		inst.builtVersion, inst.builtColorBy, inst.builtAuras = snap.version, colorBy, inst.auras
		if fresh {
			// A new graph: re-place, restart the schedule, re-arm the fit
			// and run the noisiest stretch before the first paint.
			inst.view.ResetLayout()
			inst.view.FitNow()
			inst.view.FastForward(projectionFastForward)
			inst.frozen = false
			inst.lastSelected = -1
		}
	}

	// Layout controls: the exaggeration slider is the one knob of the
	// model (ADR-0230 §SD2); its value has published meanings.
	for range c.Horizontal().KeepIter() {
		c.SliderF64(ids.PrepareStr("projectionExag"), inst.exaggeration, 1, 30).
			Text("exaggeration (1 t-SNE · 4 UMAP · 30 ForceAtlas2)").SendRespVal(&inst.exaggeration)
		if c.Button(ids.PrepareStr("projectionFit"), c.Atoms().Text("fit").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FitNow()
		}
		if c.Button(ids.PrepareStr("projectionReset"), c.Atoms().Text("re-lay-out").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.ResetLayout()
			inst.view.FastForward(projectionFastForward)
			inst.frozen = false
		}
		if c.Button(ids.PrepareStr("projectionSettle"), c.Atoms().Text("settle").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FastForward(projectionExaggerationSteps)
			inst.frozen = false
		}
		c.Checkbox(ids.PrepareStr("projectionPaused"), inst.paused, "paused").SendRespVal(&inst.paused)
		// The neighbour edges are the layout's input, not a reading: at
		// fifteen per node they cover the picture, so they are off by default.
		c.Checkbox(ids.PrepareStr("projectionEdges"), inst.showEdges, "edges").SendRespVal(&inst.showEdges)
		if res.clusters.NumClusters > 0 {
			c.Checkbox(ids.PrepareStr("projectionAuras"), inst.auras, "auras by cluster").SendRespVal(&inst.auras)
		}
		if inst.renderPublish != nil {
			c.Separator().Vertical().Send()
			inst.renderPublish()
		}
	}
	c.Label(inst.statusLine(res)).Send()
	if res.clusters.NumClusters > 0 {
		inst.renderExplanation(res)
	}

	for rt := range c.RichTextLabel("drag pans and moves a node, ctrl+scroll zooms; click a node to select its row") {
		rt.Small().Weak()
	}
	c.Separator().Horizontal().Send()
	if availW, availH, ok := c.CapturePaneSize(projectionIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := graphviewPaneFill.box(inst.paneW, inst.paneH)

	// An external selection (a Table click) selects its node; the widget's
	// own clicks are read back below.
	if selectedRow != inst.lastSelected {
		inst.lastSelected = selectedRow
		inst.view.ClearSelection()
		if selectedRow >= 0 {
			for s, row := range res.rows {
				if row == selectedRow {
					inst.view.SelectNode(uint64(s) + 1)
					break
				}
			}
		}
	}

	o := &inst.view.Opts
	o.Force.Model = graphview.ForceModelNeighborEmbedding
	o.Force.Exaggeration = float32(inst.exaggeration)
	o.Force.ExaggerationStart = projectionExaggerationStart
	o.Force.ExaggerationSteps = projectionExaggerationSteps
	o.Force.PauseOnSettle = true
	o.Force.Paused = inst.paused || inst.frozen
	o.HideEdges = !inst.showEdges
	o.Auras = graphview.AuraParams{Enabled: inst.auras && res.clusters.NumClusters > 0, Legend: graphview.AuraLegendInside}

	if err := inst.view.RenderColumns(&inst.nodes, &inst.edges, w, h); err != nil {
		log.Error().Err(err).Msg("play: projection declaration rejected")
	}

	if graphviewFrozen(inst.view, projectionFreezeSteps) {
		inst.frozen = true
	}

	// A node select publishes its row; a deselect of the published row
	// clears it to -1, the Table's own "no row". Events arrive in order, so
	// replaying them leaves the right value.
	for _, ev := range inst.view.Events() {
		s := int(ev.Node) - 1
		if s < 0 || s >= len(res.rows) {
			continue
		}
		switch ev.Kind {
		case graphview.EventKindNodeSelect:
			inst.lastSelected = res.rows[s]
		case graphview.EventKindNodeDeselect:
			if res.rows[s] != inst.lastSelected {
				continue
			}
			inst.lastSelected = -1
		default:
			continue
		}
		if emit != nil {
			emit.Emit(signalSelection, inst.lastSelected)
		}
	}
}

// buildProjectionDeclaration turns a run into the widget's columnar
// declaration (ADR-0232 §SD2), over the columns' previous backing slices:
// one node per slot, filled by the colour-by feature's viridis bucket or the
// default; the cluster as an aura id when the member's probability clears
// the floor; one edge per neighbour-graph arc pair with the membership
// weight as its strength (ADR-0224 §SD13). The columns go to the widget as
// they are, so a frame does not pay the row form's rewrite.
func buildProjectionDeclaration(res *projectionResult, colorBy int8, auras bool, nodes *graphview.NodeColumns, edges *graphview.EdgeColumns) {
	g := res.graph.Graph
	n := g.NumVertices()
	nodes.Ids = projectionColumn(nodes.Ids, n)
	nodes.Radius = projectionColumn(nodes.Radius, n)
	nodes.Color = projectionColumn(nodes.Color, n)
	nodes.AuraOffsets, nodes.AuraIds = nil, nodes.AuraIds[:0]
	if auras {
		nodes.AuraOffsets = projectionColumn(nodes.AuraOffsets, n+1)
	}
	useColor := colorBy >= 0 && int(colorBy) < card.NumFeatures && len(res.featureColumns[colorBy]) == n
	var mn, mx float64
	if useColor {
		col := res.featureColumns[colorBy]
		mn, mx = col[0], col[0]
		for _, v := range col[1:] {
			mn = min(mn, v)
			mx = max(mx, v)
		}
	}
	for s := range n {
		nodes.Ids[s] = uint64(s) + 1
		nodes.Radius[s] = projectionNodeRadius
		nodes.Color[s] = projectionColorPoint
		if useColor {
			b := bucketIndex(res.featureColumns[colorBy][s], mn, mx, projectionViridisBuckets)
			t := float32(b) / float32(projectionViridisBuckets-1)
			nodes.Color[s] = color.Hex(styletokens.Sequential(styletokens.SequentialViridis, t).AsHex())
		}
		if auras {
			nodes.AuraOffsets[s] = int32(len(nodes.AuraIds))
			if s < len(res.clusters.Label) {
				if lb := res.clusters.Label[s]; lb >= 0 && res.clusters.Probability[s] >= projectionNoiseAuraFloor {
					nodes.AuraIds = append(nodes.AuraIds, fmt.Sprintf("cluster %d", lb+1))
				}
			}
		}
	}
	if auras {
		nodes.AuraOffsets[n] = int32(len(nodes.AuraIds))
	}
	m := int(g.NumArcs() / 2)
	edges.From = projectionColumn(edges.From, m)[:0]
	edges.To = projectionColumn(edges.To, m)[:0]
	edges.Strength = projectionColumn(edges.Strength, m)[:0]
	for s := range n {
		out := g.Out(int32(s))
		w := g.OutWeights(int32(s))
		for a, d := range out {
			if d > int32(s) {
				edges.From = append(edges.From, uint64(s)+1)
				edges.To = append(edges.To, uint64(d)+1)
				// A weight that is not positive is undeclared, as the row
				// form reads it: the widget's default strength.
				st := w[a]
				if !(st > 0) {
					st = float32(math.NaN())
				}
				edges.Strength = append(edges.Strength, st)
			}
		}
	}
	edges.Width = projectionColumn(edges.Width, len(edges.From))
	for i := range edges.Width {
		edges.Width[i] = 0.5
	}
}

// projectionColumn is s resized to n rows over its own backing array when
// that holds them.
func projectionColumn[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]T, n)
}

// statusLine reports the graph, the clustering and how far the layout has
// got — the settle state and the schedule's current exaggeration being the
// readouts a live neighbour embedding has.
func (inst *Projector) statusLine(res *projectionResult) string {
	var b strings.Builder
	g := res.graph.Graph
	fmt.Fprintf(&b, "%s nodes · %s neighbour edges · k=%d · features: %s", humanize.Comma(int64(g.NumVertices())), humanize.Comma(g.NumEdges()), res.graph.K, res.params.FeatureSet)
	noise := 0
	for _, lb := range res.clusters.Label {
		if lb < 0 {
			noise++
		}
	}
	fmt.Fprintf(&b, " · %d cluster(s), %s noise (min cluster %d)", res.clusters.NumClusters, humanize.Comma(int64(noise)), res.params.MinClusterSize)
	m := inst.view.Metrics()
	switch {
	case inst.paused:
		b.WriteString(" · paused")
	case inst.frozen:
		fmt.Fprintf(&b, " · frozen after %d steps, still moving (%.3f) — settle or re-lay-out", m.Steps, m.LastDisplacement)
	case inst.view.IsSettled():
		fmt.Fprintf(&b, " · settled at exaggeration %.3g", m.Exaggeration)
	case m.Steps > 0:
		fmt.Fprintf(&b, " · settling (%.3f) at exaggeration %.3g", m.LastDisplacement, m.Exaggeration)
	}
	return b.String()
}

// renderFeatureSetCombo emits the "features" picker: which matrix the
// next Compute builds the neighbour graph over (ADR-0238). A run knob,
// like the neighbour count, so it applies on the next Compute.
func (inst *Projector) renderFeatureSetCombo() {
	ids := inst.ids
	for range c.ComboBox(ids.PrepareStr("projectionFeatureSet"),
		c.WidgetText().Text("features").Keep(),
		c.WidgetText().Text(inst.params.FeatureSet.String()).Keep()).
		KeepIter() {
		for i, fs := range projectionFeatureSets {
			if c.Button(ids.PrepareSeq(uint64(0x3200+i)),
				c.Atoms().Text(fs.Doc()).Keep()).
				Frame(false).
				Selected(inst.params.FeatureSet == fs).
				SendResp().HasPrimaryClicked() {
				inst.params.FeatureSet = fs
			}
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
	switch {
	case snap.status == projectorStatusDone && snap.result != nil:
		got := int64(len(snap.result.rows))
		if snap.totalRows > got {
			label = fmt.Sprintf("%s of %s entities · sampled",
				humanize.Comma(got), humanize.Comma(snap.totalRows))
			return
		}
		label = fmt.Sprintf("%s entities", humanize.Comma(got))
	default:
		label = fmt.Sprintf("%s entities", humanize.Comma(nRows))
	}
	return
}

// formatRunningLabel renders the spinner-adjacent label while a run is in
// flight, with the wall-clock elapsed since Start.
func formatRunningLabel(snap projectorSnapshot) (label string) {
	elapsed := time.Duration(0)
	if !snap.startedAt.IsZero() {
		elapsed = time.Since(snap.startedAt).Round(100 * time.Millisecond)
	}
	switch snap.status {
	case projectorStatusExtracting:
		label = fmt.Sprintf("extracting features · %s", elapsed)
	case projectorStatusRunning:
		label = fmt.Sprintf("building the neighbour graph and clusters · %s", elapsed)
	case projectorStatusCancelling:
		label = fmt.Sprintf("cancelling · %s", elapsed)
	default:
		label = snap.status.String()
	}
	return
}

// bucketIndex maps value v in [mn, mx] to one of n equal-width buckets in
// [0, n). Values at mx land in the top bucket (n-1); a degenerate range
// (mx <= mn) or n <= 1 maps everything to bucket 0. Pure — unit-tested in
// play_projection_test.go. Note the divisor is n (not n-1): with n-1 the top
// bucket would catch only the exact maximum and one colour would go unused.
func bucketIndex(v, mn, mx float64, n int) int {
	span := mx - mn
	if span <= 1e-12 || n <= 1 {
		return 0
	}
	idx := int((v - mn) / span * float64(n))
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}
