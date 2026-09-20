package play

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/sqlfield"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
)

// play_vectorfield.go is the part of the Vector field pane that is not a pane
// (ADR-0250 §SD5, §SD7): the field relation recovered from the split, the
// queryer that runs the source's statements through play's client, and the
// guest that turns the two into a flow layer on somebody's map.

const (
	// vectorFieldNodeID is the CTE the pane binds to,
	// vectorFieldOptsNodeID its optional one-row settings table, and
	// vectorFieldSitesNodeID the optional points the field was measured at.
	vectorFieldNodeID      NodeID = "vector_field"
	vectorFieldOptsNodeID  NodeID = "vector_field_opts"
	vectorFieldSitesNodeID NodeID = "vector_field_sites"

	// vectorFieldFetchTimeout bounds one statement of the source. Generous
	// for the Map's reason: a remote() relation answers in tens of seconds.
	vectorFieldFetchTimeout = 60 * time.Second
)

// errVectorFieldCancelled marks work the reader stopped, which is not a
// failure of the field and is spelled as itself on the status line.
var errVectorFieldCancelled = errors.New("cancelled")

// vectorFieldRelation is the split's `vector_field` node as a field relation:
// the SET prelude, then one WITH list of the node's upstream CTEs with the
// node itself last, which the source's statements read by name. Always the
// wrap form — the body stays inside its parentheses, so a body that opens a
// WITH of its own, or names itself recursively, needs no second shape.
func vectorFieldRelation(res splitResult, nodeID NodeID) (rel sqlfield.Relation, ok bool) {
	node, found := findSplitNode(res, nodeID)
	if !found || node.Kind == splitNodeStatement || node.Client != nil {
		return
	}
	clauseKw := "WITH "
	if node.Recursive {
		clauseKw = "WITH RECURSIVE "
	}
	deps := transitiveDeps(res, nodeID)
	withDefs := make([]string, 0, len(deps)+1)
	for _, d := range deps {
		dn, has := findSplitNode(res, d)
		if !has {
			continue
		}
		if dn.Recursive {
			clauseKw = "WITH RECURSIVE "
		}
		withDefs = append(withDefs, string(d)+" AS (\n"+dn.SQL+"\n)")
	}
	withDefs = append(withDefs, string(node.ID)+" AS (\n"+node.SQL+"\n)")
	parts := make([]string, 0, len(res.Prelude)+1)
	parts = append(parts, res.Prelude...)
	parts = append(parts, clauseKw+strings.Join(withDefs, ",\n"))
	rel = sqlfield.Relation{Head: strings.Join(parts, ";\n"), From: string(node.ID)}
	ok = true
	return
}

// vectorFieldIdentity is what makes two relations the same field: the text
// and the values of the signals it reads. A change of either is another
// source (ADR-0250 §SD5).
func vectorFieldIdentity(rel sqlfield.Relation, params map[string]string) string {
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(rel.Head)
	b.WriteByte(0)
	b.WriteString(rel.From)
	for _, k := range names {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

// vectorFieldQueryLabels is what each purpose's statements are stamped with:
// the executor's query id and label, and the name the pane's progress
// readout follows the phase under. One vocabulary, so a row in the query log
// and a bar in the pane name the same work.
var vectorFieldQueryLabels = [sqlfield.PurposeSummary + 1]string{
	sqlfield.PurposeDescribe: "vectorfield-describe",
	sqlfield.PurposeWindow:   "vectorfield",
	sqlfield.PurposeSummary:  "vectorfield-summary",
}

// vectorFieldQueryProgress says which purposes ask the server for in-band
// progress. A describe is two full scans with nothing yet on screen, and a
// summary one pass over every step, so their counters are worth what they
// cost: the transport that surfaces progress lines mid-run takes one request
// per connection (chserver's progress.go). A window is the opposite trade —
// short, bounded by the view, and fired several at a time when playback looks
// ahead — so it stays on the pooled client, and the readout shows it as
// running without numbers, which is what it has always been able to say.
var vectorFieldQueryProgress = [sqlfield.PurposeSummary + 1]bool{
	sqlfield.PurposeDescribe: true,
	sqlfield.PurposeSummary:  true,
}

// vectorFieldProgress is what the source's statements report while they run,
// kept per purpose. The statements run on goroutines of the guest's and of
// the layer's, and the pane reads one purpose's tick per frame on the render
// thread, so a tick lands here under a lock rather than in a guest field.
//
// running is a count and not a flag: a superseded describe goes on unwinding
// while its replacement starts, and the phase is over only when the last of
// them is off the wire.
//
// A tick outlives the statement that reported it, and is dropped by the
// [vectorFieldProgress.reset] the next PHASE begins with. A describe is three
// statements one after another, several of them short enough to land no tick
// at all, and counters cleared per statement left the readout mostly blank
// and blinking (seen live, 2026-09-20). What the reader is told between two
// of them is what the phase has read so far, which is the last thing known.
type vectorFieldProgress struct {
	mu    sync.Mutex
	lanes [sqlfield.PurposeSummary + 1]struct {
		running int
		p       runstream.Progress
		fresh   bool
	}
}

// reset drops what a purpose reported, for a caller starting a new phase of
// it. The statements of one phase share what they have read.
func (inst *vectorFieldProgress) reset(purpose sqlfield.PurposeE) {
	inst.mu.Lock()
	l := &inst.lanes[purpose]
	l.p, l.fresh = runstream.Progress{}, false
	inst.mu.Unlock()
}

func (inst *vectorFieldProgress) begin(purpose sqlfield.PurposeE) {
	inst.mu.Lock()
	inst.lanes[purpose].running++
	inst.mu.Unlock()
}

func (inst *vectorFieldProgress) end(purpose sqlfield.PurposeE) {
	inst.mu.Lock()
	l := &inst.lanes[purpose]
	if l.running > 0 {
		l.running--
	}
	inst.mu.Unlock()
}

func (inst *vectorFieldProgress) tick(purpose sqlfield.PurposeE, p runstream.Progress) {
	inst.mu.Lock()
	l := &inst.lanes[purpose]
	if l.running > 0 {
		l.p, l.fresh = p, true
	}
	inst.mu.Unlock()
}

// view is one purpose's latest tick, and whether a tick has landed for a
// statement that is still running — the gate every display site checks.
func (inst *vectorFieldProgress) view(purpose sqlfield.PurposeE) (p runstream.Progress, fresh bool) {
	inst.mu.Lock()
	l := &inst.lanes[purpose]
	p, fresh = l.p, l.fresh
	inst.mu.Unlock()
	return
}

// vectorFieldQueryer runs the source's statements through play's client, so
// the pre-execute passes, the dispatch decision and the log_comment stamp
// apply to a window query as to any other (ADR-0250 §SD4). signals are the
// values the relation reads, resolved when the source was built; the
// source's own parameters join them under the param_ prefix.
//
// A lane identity is stable and replaces a running predecessor, which is what
// a superseded window request wants: the layer has one request in flight and
// cancels it before the next. There is one identity per purpose, because a
// summary runs beside the windows and neither may replace the other.
//
// A statement whose purpose [vectorFieldQueryProgress] names carries a
// progress sink, which is what opts the request into the server's in-band
// progress headers (ADR-0115 plane A): without one the two describe scans are
// counted nowhere and the pane can only say that something is running.
type vectorFieldQueryer struct {
	exec     [sqlfield.PurposeSummary + 1]clientExecutor
	signals  map[string]string
	progress *vectorFieldProgress
}

func newVectorFieldQueryer(client *Client, signals map[string]string, progress *vectorFieldProgress) (q vectorFieldQueryer) {
	q.signals, q.progress = signals, progress
	for purpose, label := range vectorFieldQueryLabels {
		q.exec[purpose] = clientExecutor{client: client, opts: newExecOptions(label)}
	}
	return
}

var _ sqlfield.QueryerI = vectorFieldQueryer{}

func (inst vectorFieldQueryer) QueryE(ctx context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	merged := make(map[string]string, len(inst.signals)+len(params))
	for k, v := range inst.signals {
		merged[k] = v
	}
	for k, v := range params {
		merged["param_"+k] = v
	}
	purpose := sqlfield.PurposeOf(ctx)
	ctx, cancel := context.WithTimeout(ctx, vectorFieldFetchTimeout)
	defer cancel()
	alloc := memory.NewGoAllocator()
	var onProgress func(p runstream.Progress)
	if inst.progress != nil && vectorFieldQueryProgress[purpose] {
		inst.progress.begin(purpose)
		defer inst.progress.end(purpose)
		onProgress = func(p runstream.Progress) { inst.progress.tick(purpose, p) }
	}
	rec, schema, _, err := inst.exec[purpose].executeWithProgress(ctx, compiledNode{SQL: statement, Params: merged}, alloc, onProgress)
	if err != nil {
		return
	}
	if rec == nil && schema != nil {
		// No batch is an empty result, which the source reads as one.
		cols := make([]arrow.Array, schema.NumFields())
		for i, f := range schema.Fields() {
			b := array.NewBuilder(alloc, f.Type)
			cols[i] = b.NewArray()
			b.Release()
		}
		rec = array.NewRecordBatch(schema, cols, 0)
		for _, col := range cols {
			col.Release()
		}
	}
	return
}

// vectorFieldBuild is one source being described off the render thread.
type vectorFieldBuild struct {
	identity string
	cancel   context.CancelFunc

	mu   sync.Mutex
	done bool
	src  *sqlfield.Source
	err  error
}

// vectorFieldSummaryJob is one per-step summary being fetched.
type vectorFieldSummaryJob struct {
	key    string
	cancel context.CancelFunc

	mu   sync.Mutex
	done bool
	out  []sqlfield.StepSummary
	err  error
}

// vectorFieldGuest draws a field relation as a flow layer inside a map's
// overlay callback (ADR-0250 §SD7). It owns no map and takes no pointer, so
// what it lies over or under is its host's call order.
//
// Ensure and Draw belong to the frame goroutine. Describing a relation is
// several queries, so a source is built on a goroutine of its own and the
// layer of the previous field stays on screen until the next one is ready.
type vectorFieldGuest struct {
	client *Client
	// Opts is applied to the layer every frame.
	Opts flowoverlay.Options

	identity string
	building *vectorFieldBuild
	src      *sqlfield.Source
	layer    *flowoverlay.Layer
	err      error
	// cancelled says the describe was stopped by hand. The identity stays
	// claimed, so Ensure does not start it again until the relation or its
	// signals change — or until a Run drops the field (forgetLanes).
	cancelled bool

	// progress carries what the statements of each purpose report while
	// they run, for the pane's readout.
	progress vectorFieldProgress

	pos float64 // display time as a fractional step, carried across fields

	// summary is the magnitude of every step inside the view it was last
	// asked for (ADR-0251 §SD4); summaryKey says which view that was.
	summary    []sqlfield.StepSummary
	summaryKey string
	summaryJob *vectorFieldSummaryJob
	summaryErr error
}

func newVectorFieldGuest(client *Client) *vectorFieldGuest {
	return &vectorFieldGuest{client: client}
}

// Ensure makes the guest show the given relation, starting a build when the
// identity is new. params are the relation's resolved signal values, with
// the param_ prefix.
func (inst *vectorFieldGuest) Ensure(rel sqlfield.Relation, params map[string]string, shape sqlfield.Shape) {
	identity := vectorFieldIdentity(rel, params)
	if b := inst.building; b != nil {
		b.mu.Lock()
		done, src, err := b.done, b.src, b.err
		b.mu.Unlock()
		if done {
			// inst.identity is the field wanted; a build that answers an
			// older one was cancelled and is let go.
			inst.building = nil
			if b.identity == inst.identity {
				inst.adopt(src, err)
			}
		}
	}
	if identity == inst.identity {
		return
	}
	if inst.building != nil {
		inst.building.cancel()
		inst.building = nil
	}
	inst.identity = identity
	inst.err, inst.cancelled = nil, false
	inst.progress.reset(sqlfield.PurposeDescribe)
	ctx, cancel := context.WithCancel(context.Background())
	b := &vectorFieldBuild{identity: identity, cancel: cancel}
	inst.building = b
	queryer := newVectorFieldQueryer(inst.client, params, &inst.progress)
	go func() {
		src, err := sqlfield.NewSourceE(ctx, queryer, rel, sqlfield.Options{Shape: &shape})
		b.mu.Lock()
		b.done, b.src, b.err = true, src, err
		b.mu.Unlock()
		cancel()
	}()
}

func (inst *vectorFieldGuest) adopt(src *sqlfield.Source, err error) {
	if inst.layer != nil {
		inst.layer.Close()
		inst.layer = nil
	}
	inst.src, inst.err = src, err
	inst.dropSummary()
	if err != nil || src == nil {
		return
	}
	inst.layer = flowoverlay.New(src, inst.Opts)
	inst.layer.SetStepPosition(inst.pos)
	inst.pos = inst.layer.StepPosition()
}

func (inst *vectorFieldGuest) dropSummary() {
	if inst.summaryJob != nil {
		inst.summaryJob.cancel()
		inst.summaryJob = nil
	}
	inst.summary, inst.summaryKey, inst.summaryErr = nil, "", nil
}

// EnsureSummary asks for the magnitude of every step inside a view, once per
// view: the caller hands it a view that has settled. It is one query over all
// steps, so it runs beside the windows and the strip shows the last answer
// meanwhile. A field of one step has nothing to compare.
func (inst *vectorFieldGuest) EnsureSummary(req vectorfield.Request) {
	if job := inst.summaryJob; job != nil {
		job.mu.Lock()
		done, out, err := job.done, job.out, job.err
		job.mu.Unlock()
		if done {
			inst.summaryJob = nil
			if job.key == inst.summaryKey {
				inst.summaryErr = err
				if err == nil {
					inst.summary = out
				}
			}
		}
	}
	src := inst.src
	if src == nil || len(src.Describe().Steps) < 2 {
		return
	}
	key := fmt.Sprintf("%.4f %.4f %.4f %.4f %d %d", req.West, req.East, req.South, req.North, req.MaxCols, req.MaxRows)
	if key == inst.summaryKey {
		return
	}
	if inst.summaryJob != nil {
		inst.summaryJob.cancel()
	}
	inst.summaryKey = key
	inst.progress.reset(sqlfield.PurposeSummary)
	ctx, cancel := context.WithCancel(context.Background())
	job := &vectorFieldSummaryJob{key: key, cancel: cancel}
	inst.summaryJob = job
	go func() {
		out, err := src.SummarizeE(ctx, req)
		job.mu.Lock()
		job.done, job.out, job.err = true, out, err
		job.mu.Unlock()
		cancel()
	}()
}

// StepState is what the layer has of a step.
func (inst *vectorFieldGuest) StepState(step int) flowoverlay.StepStateE {
	if inst.layer == nil {
		return flowoverlay.StepStateIdle
	}
	return inst.layer.StepState(step)
}

// Forget drops the field, so that the next Ensure describes it again — what a
// fresh Run asks for, since the data behind an unchanged text may have moved.
func (inst *vectorFieldGuest) Forget() {
	if inst.building != nil {
		inst.building.cancel()
		inst.building = nil
	}
	inst.identity, inst.cancelled = "", false
}

// Cancel stops the statements of a phase and leaves that phase alone: a
// describe keeps its identity claimed and a summary its view key, so neither
// is asked for again until the relation, its signals or the view change. A
// Run clears both (VectorFieldDriver.forgetLanes).
//
// A window is not cancellable here. The layer asks for one from the view on
// screen, so the next frame would ask again — a button that undoes itself —
// and a window is bounded by the view where the other two are bounded by the
// relation.
func (inst *vectorFieldGuest) Cancel(purpose sqlfield.PurposeE) {
	switch purpose {
	case sqlfield.PurposeDescribe:
		if inst.building != nil {
			inst.building.cancel()
			inst.building = nil
			inst.cancelled = true
		}
	case sqlfield.PurposeSummary:
		if inst.summaryJob != nil {
			inst.summaryJob.cancel()
			inst.summaryJob = nil
			inst.summaryErr = errVectorFieldCancelled
		}
	}
}

// Phase is the statement purpose the pane should show this frame, and what
// that purpose last reported. Describe comes first — it is the phase nothing
// is on screen for — then a window, then the summary that runs beside them.
//
// The flags it switches on are the frame-visible ones rather than the wire's:
// a describe is three statements one after another, and a phase that ended
// between two of them would blink the readout off and on.
func (inst *vectorFieldGuest) Phase() (purpose sqlfield.PurposeE, running bool, p runstream.Progress, fresh bool) {
	switch {
	case inst.building != nil:
		purpose = sqlfield.PurposeDescribe
	case inst.layer != nil && inst.layer.Stats().InFlight:
		purpose = sqlfield.PurposeWindow
	case inst.summaryJob != nil:
		purpose = sqlfield.PurposeSummary
	default:
		return
	}
	running = true
	p, fresh = inst.progress.view(purpose)
	return
}

// Close ends the guest.
func (inst *vectorFieldGuest) Close() {
	inst.Forget()
	inst.dropSummary()
	if inst.layer != nil {
		inst.layer.Close()
		inst.layer = nil
	}
	inst.src = nil
}

// Loading reports a source being described.
func (inst *vectorFieldGuest) Loading() bool { return inst.building != nil }

// SummaryLoading reports the per-step summary being fetched.
func (inst *vectorFieldGuest) SummaryLoading() bool { return inst.summaryJob != nil }

// Meta is the field's description; ok is false until a source is there.
func (inst *vectorFieldGuest) Meta() (meta vectorfield.Meta, ok bool) {
	if inst.layer == nil {
		return
	}
	return inst.layer.Meta(), true
}

// SetStepPosition sets the display time as a fractional step index.
func (inst *vectorFieldGuest) SetStepPosition(pos float64) {
	inst.pos = pos
	if inst.layer != nil {
		inst.layer.SetStepPosition(pos)
		inst.pos = inst.layer.StepPosition()
	}
}

// SetAhead names the steps the display time will reach next, so the layer
// has them before playback gets there (ADR-0251 §SD9).
func (inst *vectorFieldGuest) SetAhead(steps []int) {
	if inst.layer != nil {
		inst.layer.SetAhead(steps)
	}
}

// SetTime sets the display time, and reports the step position it fell on.
func (inst *vectorFieldGuest) SetTime(t time.Time) (pos float64) {
	if inst.layer != nil {
		inst.layer.SetTime(t)
		inst.pos = inst.layer.StepPosition()
	}
	return inst.pos
}

// Draw paints the layer; it is called inside the host map's overlay callback.
func (inst *vectorFieldGuest) Draw(p portolan.Projector) {
	if inst.building != nil || inst.summaryJob != nil {
		// Nothing else wakes a frame when a build or a summary lands.
		c.RequestRepaintAfter(0.05)
	}
	if inst.layer == nil {
		return
	}
	// The source, the step position and the particles are the layer's own;
	// everything else follows the controls.
	inst.layer.Opts = inst.Opts
	inst.layer.Draw(p)
}
