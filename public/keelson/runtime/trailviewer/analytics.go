package trailviewer

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// The analytic views of ADR-0350 §SD11 (proposed) that M4 builds: the
// actor × object matrix and the dotted chart. Each is one GROUP BY over the
// case, the window and the filter the table last read — the same scope,
// never a wider one — drawn with a widget the tree has, under a text line
// that states what it shows and whether it was cut. Each is read by a
// bgjob worker while its tab is open, again when the table refills, and
// never from a frame.

// ActorE is the matrix's row axis: who acted.
type ActorE uint8

const (
	ActorPrincipal ActorE = iota
	ActorApp
	ActorWindow
)

// AllActors lists the actors in the order the combo shows them.
var AllActors = []ActorE{ActorPrincipal, ActorApp, ActorWindow}

var actorNames = [...]string{"principal", "app", "window"}

func (inst ActorE) String() string {
	if int(inst) < len(actorNames) {
		return actorNames[inst]
	}
	return "unknown"
}

// ObjectE is the matrix's column axis: what was touched.
type ObjectE uint8

const (
	ObjectSubject ObjectE = iota
	ObjectEntity
	ObjectOperation
)

// AllObjects lists the objects in the order the combo shows them.
var AllObjects = []ObjectE{ObjectSubject, ObjectEntity, ObjectOperation}

var objectNames = [...]string{"subject", "entity", "operation"}

func (inst ObjectE) String() string {
	if int(inst) < len(objectNames) {
		return objectNames[inst]
	}
	return "unknown"
}

// DottedRowE is what one row of the dotted chart is.
type DottedRowE uint8

const (
	DottedRun DottedRowE = iota
	DottedWindow
	DottedConversation
	DottedEntity
)

// AllDottedRows lists the row kinds in the order the combo shows them.
var AllDottedRows = []DottedRowE{DottedRun, DottedWindow, DottedConversation, DottedEntity}

var dottedRowNames = [...]string{"run", "window", "conversation", "entity"}

func (inst DottedRowE) String() string {
	if int(inst) < len(dottedRowNames) {
		return dottedRowNames[inst]
	}
	return "unknown"
}

const (
	// matrixPairs bounds the (actor, object) pairs one read returns.
	matrixPairs = 10000
	// matrixShown bounds the actors and the objects the grid draws: the
	// busiest, by row count.
	matrixShown = 50
	// dottedGroups bounds the (row, bucket) groups one read returns.
	dottedGroups = 20000
	// dottedShown bounds the chart's rows: the busiest, by row count.
	dottedShown = 40
	// The marks' radius, in points: the smallest count's and the largest's.
	dottedMinRadius = 2.5
	dottedMaxRadius = 8.0
	// dottedTiers is how many marker sizes the chart draws, one series each.
	dottedTiers = 4
	dottedRowPx = 22.0
)

// auditOnly reports whether an axis is read from the audit-event mart: a
// principal, a data subject and an entity are an audit event's own columns.
func matrixNeedsAudit(a ActorE, o ObjectE) bool {
	return a == ActorPrincipal || o == ObjectSubject || o == ObjectEntity
}

// aggSource is the view an aggregate reads and its conditions: the
// timeline under the table's read conditions, or — when an axis is an audit
// event's own column — the audit-event mart under the same window and case,
// narrowed to the timeline's filtered ids when a filter is set.
func (inst *marts) aggSource(q listQuery, audit bool) (view string, where []string) {
	if !audit {
		return trailviews.ViewTimeline, inst.rowsWhere(q, nil)
	}
	ae, tl := trailviews.ViewAuditEvents, trailviews.ViewTimeline
	where = []string{inst.windowPred(ae, q.From, q.To), inst.casePred(ae, q.Case, q.From, q.To)}
	if len(q.Filter.Terms) > 0 {
		where = append(where, inst.col(ae, "id")+" IN (SELECT "+inst.col(tl, "id")+" FROM "+inst.table(tl)+" WHERE "+
			strings.Join(inst.rowsWhere(listQuery{Case: q.Case, From: q.From, To: q.To, Filter: q.Filter}, nil), " AND ")+")")
	}
	return ae, where
}

// aggExpr is a named axis as an expression over view: an empty string for
// a row that has no value on it.
func (inst *marts) aggExpr(view string, what string) string {
	col := func(n string) string { return inst.col(view, n) }
	audit := view == trailviews.ViewAuditEvents
	switch what {
	case "window":
		return "concat(" + col("run") + ", ':', toString(" + col("instance") + "))"
	case "subject":
		return "if(" + col("subject") + " = 0, '', toString(" + col("subject") + "))"
	case "operation":
		if audit {
			return "concat(" + col("domain") + ", '.', " + col("action") + ")"
		}
		return col("subject")
	case "outcome":
		if audit {
			return col("outcome")
		}
		return col("status")
	}
	return col(what)
}

// MatrixCell is one (actor, object) pair: its row count and its last
// outcome.
type MatrixCell struct {
	Actor  string `ch:"actor"`
	Object string `ch:"object"`
	N      uint64 `ch:"n"`
	Last   string `ch:"last_outcome"`
}

// matrixSQL is the matrix's read: one GROUP BY of the actor and the object
// over the case, window and filter, with each pair's last outcome.
func (inst *marts) matrixSQL(q listQuery, a ActorE, o ObjectE) string {
	view, where := inst.aggSource(q, matrixNeedsAudit(a, o))
	return "SELECT toString(" + inst.aggExpr(view, a.String()) + ") AS actor, toString(" + inst.aggExpr(view, o.String()) + ") AS object, count() AS n, " +
		"toString(argMax(" + inst.aggExpr(view, "outcome") + ", " + inst.col(view, "ts") + ")) AS last_outcome FROM " + inst.table(view) +
		" WHERE " + strings.Join(where, " AND ") + " GROUP BY actor, object HAVING actor != '' AND object != ''" +
		" ORDER BY n DESC, actor, object LIMIT " + strconv.Itoa(matrixPairs)
}

// dottedSQL is the dotted chart's read: rows per (case row, bucket of step)
// over the case, window and filter.
func (inst *marts) dottedSQL(q listQuery, row DottedRowE, step time.Duration) string {
	view, where := inst.aggSource(q, row == DottedEntity)
	return "SELECT toString(" + inst.aggExpr(view, row.String()) + ") AS key, toInt64(toUnixTimestamp(toStartOfInterval(" + inst.col(view, "ts") +
		", INTERVAL " + strconv.FormatInt(max(int64(step/time.Second), 1), 10) + " SECOND))) AS bucket, count() AS n FROM " + inst.table(view) +
		" WHERE " + strings.Join(where, " AND ") + " GROUP BY key, bucket HAVING key != '' ORDER BY key, bucket LIMIT " + strconv.Itoa(dottedGroups)
}

// matrixData is the matrix pivoted: the busiest actors and objects, the
// counts and last outcomes of their pairs, and what the line says.
type matrixData struct {
	actor    ActorE
	object   ObjectE
	actors   []string
	objects  []string
	counts   [][]uint64
	last     [][]string
	allA     int
	allO     int
	pairs    int
	rows     uint64
	busiestA string
	busiestO string
	busiestN uint64
	cut      bool
}

// pivotMatrix folds pairs into the grid, keeping the shown busiest actors
// and objects by row count; cut says the read hit its pair bound.
func pivotMatrix(cells []MatrixCell, a ActorE, o ObjectE, shown int, cut bool) (m matrixData) {
	m.actor, m.object, m.pairs, m.cut = a, o, len(cells), cut
	totA, totO := map[string]uint64{}, map[string]uint64{}
	var orderA, orderO []string
	for _, x := range cells {
		if _, ok := totA[x.Actor]; !ok {
			orderA = append(orderA, x.Actor)
		}
		if _, ok := totO[x.Object]; !ok {
			orderO = append(orderO, x.Object)
		}
		totA[x.Actor] += x.N
		totO[x.Object] += x.N
		m.rows += x.N
		if x.N > m.busiestN || (x.N == m.busiestN && (x.Actor < m.busiestA || (x.Actor == m.busiestA && x.Object < m.busiestO))) {
			m.busiestA, m.busiestO, m.busiestN = x.Actor, x.Object, x.N
		}
	}
	m.allA, m.allO = len(orderA), len(orderO)
	sortKeys(orderA, totA)
	sortKeys(orderO, totO)
	m.actors, m.objects = orderA[:min(len(orderA), shown)], orderO[:min(len(orderO), shown)]
	ai, oi := map[string]int{}, map[string]int{}
	for i, k := range m.actors {
		ai[k] = i
	}
	for i, k := range m.objects {
		oi[k] = i
	}
	m.counts, m.last = make([][]uint64, len(m.actors)), make([][]string, len(m.actors))
	for i := range m.actors {
		m.counts[i], m.last[i] = make([]uint64, len(m.objects)), make([]string, len(m.objects))
	}
	for _, x := range cells {
		i, okA := ai[x.Actor]
		j, okO := oi[x.Object]
		if okA && okO {
			m.counts[i][j], m.last[i][j] = x.N, x.Last
		}
	}
	return
}

// line is the matrix in words: actors, objects, rows, pairs, the busiest
// pair, and what was cut.
func (inst *matrixData) line() string {
	if inst.pairs == 0 {
		return fmt.Sprintf("0 pairs: no row in the case and window has both a %s and a %s", inst.actor, inst.object)
	}
	s := fmt.Sprintf("%d actors × %d objects, %d rows in %d pairs; busiest %s %s → %s %s with %d",
		inst.allA, inst.allO, inst.rows, inst.pairs, inst.actor, inst.busiestA, inst.object, inst.busiestO, inst.busiestN)
	if len(inst.actors) < inst.allA || len(inst.objects) < inst.allO {
		s += fmt.Sprintf("; the grid shows the %d busiest of each", matrixShown)
	}
	if inst.cut {
		s += fmt.Sprintf("; the read stopped at %d pairs, so counts are a lower bound", matrixPairs)
	}
	return s
}

// dottedData is the chart: the busiest rows, a mark per (row, bucket) in
// one of dottedTiers sizes, and what the line says.
type dottedData struct {
	row      DottedRowE
	step     time.Duration
	keys     []string
	allKeys  int
	buckets  int
	rows     uint64
	busiestK string
	busiestN uint64
	maxN     uint64
	tiers    [dottedTiers]dottedTier
	cut      bool
}

// dottedTier is one marker size: its points and the counts it covers.
type dottedTier struct {
	xs, ys     []float64
	radius     float32
	minN, maxN uint64
}

// tierOf is the marker size of a count against the largest: radius by the
// square root, so the mark's area follows the count.
func tierOf(n uint64, maxN uint64) int {
	if maxN == 0 {
		return 0
	}
	t := int(math.Ceil(math.Sqrt(float64(n)/float64(maxN))*dottedTiers)) - 1
	return min(max(t, 0), dottedTiers-1)
}

func tierRadius(t int) float32 {
	return dottedMinRadius + float32(t)*(dottedMaxRadius-dottedMinRadius)/(dottedTiers-1)
}

// buildDotted folds the groups into the chart's busiest rows, the busiest
// on top; cut says the read hit its group bound.
func buildDotted(groups []Bucket, row DottedRowE, step time.Duration, shown int, cut bool) (d dottedData) {
	d.row, d.step, d.cut = row, step, cut
	totals := map[string]uint64{}
	var order []string
	buckets := map[int64]bool{}
	for _, g := range groups {
		if _, ok := totals[g.Key]; !ok {
			order = append(order, g.Key)
		}
		totals[g.Key] += g.N
		d.rows += g.N
		buckets[g.Bucket] = true
	}
	d.allKeys, d.buckets = len(order), len(buckets)
	sortKeys(order, totals)
	if len(order) > 0 {
		d.busiestK, d.busiestN = order[0], totals[order[0]]
	}
	d.keys = order[:min(len(order), shown)]
	idx := map[string]int{}
	for i, k := range d.keys {
		idx[k] = i
	}
	for _, g := range groups {
		if _, ok := idx[g.Key]; ok && g.N > d.maxN {
			d.maxN = g.N
		}
	}
	for t := range d.tiers {
		d.tiers[t].radius = tierRadius(t)
	}
	half := step.Seconds() / 2
	for _, g := range groups {
		i, ok := idx[g.Key]
		if !ok {
			continue
		}
		t := &d.tiers[tierOf(g.N, d.maxN)]
		t.xs = append(t.xs, float64(g.Bucket)+half)
		t.ys = append(t.ys, float64(len(d.keys)-1-i))
		if t.minN == 0 || g.N < t.minN {
			t.minN = g.N
		}
		t.maxN = max(t.maxN, g.N)
	}
	return
}

// line is the chart in words: rows, buckets of what size, the busiest row,
// the largest mark, and what was cut.
func (inst *dottedData) line() string {
	if inst.allKeys == 0 {
		return fmt.Sprintf("0 rows: no row in the case and window has a %s", inst.row)
	}
	s := fmt.Sprintf("%d rows over %d buckets of %s; busiest %s %s with %d; the largest mark is %d in one bucket",
		inst.allKeys, inst.buckets, inst.step, inst.row, inst.busiestK, inst.busiestN, inst.maxN)
	if len(inst.keys) < inst.allKeys {
		s += fmt.Sprintf("; the chart shows the %d busiest", dottedShown)
	}
	if inst.cut {
		s += fmt.Sprintf("; the read stopped at %d marks", dottedGroups)
	}
	return s
}

// aggKey is what one aggregate read is keyed by: the table's fill, the
// pick and the bucket size.
type aggKey struct {
	gen  uint64
	pick uint8
	pk2  uint8
	step time.Duration
}

// aggCache is one analytic view's read, by a bgjob worker.
type aggCache[T any] struct {
	runner bgjob.Runner[T]
	asked  aggKey
	has    bool
	key    aggKey
	data   T
	err    error
	// ver counts the reads adopted, so a view built from the data knows
	// when to rebuild.
	ver uint64
}

// tick adopts a finished read and starts one when the key moved.
func (inst *aggCache[T]) tick(src *Source, key aggKey, kind string, read func(ctx context.Context, p *probe) (T, error), newProbe func() (*probe, error)) {
	if res, _, ok := inst.runner.TakeResult(); ok {
		inst.data, inst.key, inst.has, inst.err = *res, inst.asked, true, nil
		inst.ver++
	}
	if snap := inst.runner.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		inst.err = snap.Err
		inst.runner.Invalidate()
	}
	if !src.persisted() || inst.asked == key && (inst.has || inst.runner.Running() || inst.err != nil) {
		return
	}
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	inst.asked, inst.err = key, nil
	inst.runner.Start(nil, bgjob.Spec{Kind: kind, Title: "Trail viewer " + kind},
		func(ctx context.Context) (out *T, err error) {
			ctx, cancel := context.WithTimeout(ctx, readTimeout)
			defer cancel()
			p, err := newProbe()
			if err != nil {
				return
			}
			v, err := read(ctx, p)
			if err != nil {
				return
			}
			return &v, nil
		})
}

// current is the data when it is of key.
func (inst *aggCache[T]) current(key aggKey) (d *T, stale bool) {
	if !inst.has {
		return nil, false
	}
	return &inst.data, inst.key != key
}

func (inst *aggCache[T]) close() { inst.runner.Invalidate() }

// analytics is the two views' state: the picks, the reads, and the
// matrix's grid.
type analytics struct {
	actor  ActorE
	object ObjectE
	row    DottedRowE
	matrix aggCache[matrixData]
	dotted aggCache[dottedData]
	grid   matrixGrid
	plotW  float32
}

func (inst *analytics) close() {
	inst.matrix.close()
	inst.dotted.close()
	inst.grid.release()
}

// aggQuery is the read the table last made: its case, window and filter.
func (inst *App) aggQuery() listQuery {
	ch := &inst.cache
	return listQuery{Case: ch.scope.Case, From: ch.from, To: ch.to, Filter: parseFilterKeyOrEmpty(ch.scope.Filter)}
}

func (inst *App) matrixKey() aggKey {
	return aggKey{gen: inst.cache.gen, pick: uint8(inst.an.actor), pk2: uint8(inst.an.object)}
}

func (inst *App) dottedKey() aggKey {
	return aggKey{gen: inst.cache.gen, pick: uint8(inst.an.row), step: inst.cache.step}
}

// tickAnalytics reads the open analytic view once the table has a fill.
func (inst *App) tickAnalytics(src *Source) {
	if !inst.cache.filled {
		return
	}
	q := inst.aggQuery()
	switch inst.tab {
	case TabMatrix:
		a, o := inst.an.actor, inst.an.object
		inst.an.matrix.tick(src, inst.matrixKey(), "matrix", func(ctx context.Context, p *probe) (m matrixData, err error) {
			cells, err := queryRows[MatrixCell](ctx, p, p.marts.matrixSQL(q, a, o))
			if err != nil {
				return
			}
			return pivotMatrix(cells, a, o, matrixShown, len(cells) == matrixPairs), nil
		}, inst.probeFunc(src))
	case TabDotted:
		row, step := inst.an.row, inst.cache.step
		if step <= 0 {
			return
		}
		inst.an.dotted.tick(src, inst.dottedKey(), "dotted", func(ctx context.Context, p *probe) (d dottedData, err error) {
			groups, err := queryRows[Bucket](ctx, p, p.marts.dottedSQL(q, row, step))
			if err != nil {
				return
			}
			return buildDotted(groups, row, step, dottedShown, len(groups) == dottedGroups), nil
		}, inst.probeFunc(src))
	}
}

// pickCombo is a combo over names; it returns the picked index.
func (inst *App) pickCombo(key string, label string, names []string, cur int) (picked int) {
	picked = cur
	for range c.IdScope(inst.ids.PrepareStr(key)) {
		for range c.ComboBox(inst.ids.PrepareStr("combo"), c.WidgetText().Text(label).Keep(),
			c.WidgetText().Text(names[cur]).Keep()).KeepIter() {
			for i, n := range names {
				sel := i == cur
				if c.Button(inst.ids.PrepareSeq(uint64(i)), c.Atoms().Text(n).Keep()).Selected(sel).FrameWhenInactive(!sel).Frame(true).
					SendResp().HasPrimaryClicked() {
					picked = i
				}
			}
		}
	}
	return
}

// aggStatus is the line under an analytic view's picks while it reads or
// after it failed; ok says the data is to be drawn.
func aggStatus[T any](ca *aggCache[T], key aggKey) (d *T, ok bool) {
	d, stale := ca.current(key)
	switch {
	case ca.err != nil:
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, ca.err.Error()).Small().End().Keep()).Wrap().Send()
		return nil, false
	case d == nil:
		c.Label("(reading…)").Send()
		return nil, false
	case stale:
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, "reading the new pick or fill…").Small().End().Keep()).Send()
	}
	return d, true
}

// renderMatrix is the actor × object tab: the two picks, the line, and the
// grid with actors as rows and objects as columns.
func (inst *App) renderMatrix() {
	an := &inst.an
	for range c.HorizontalTop().KeepIter() {
		an.actor = ActorE(inst.pickCombo("mx-actor", "actor", actorNames[:], int(an.actor)))
		an.object = ObjectE(inst.pickCombo("mx-object", "object", objectNames[:], int(an.object)))
	}
	if matrixNeedsAudit(an.actor, an.object) {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear,
			"a principal, a data subject and an entity are an audit event's own: the matrix counts the case's audit events").Small().End().Keep()).Wrap().Send()
	}
	m, ok := aggStatus(&an.matrix, inst.matrixKey())
	if !ok {
		return
	}
	c.Label(m.line()).Wrap().Send()
	if len(m.actors) == 0 {
		return
	}
	g := &an.grid
	g.build(m, an.matrix.ver)
	cols := make([]recordtable.Column, 0, len(m.objects)+1)
	cols = append(cols, recordtable.Column{Arrow: 0, Header: m.actor.String(), Width: 220, WidthType: "text", Cell: func(row int64) recordtable.Cell {
		return recordtable.Cell{Text: m.actors[row], LeftAlign: true, Hover: m.actors[row]}
	}})
	for j, o := range m.objects {
		cols = append(cols, recordtable.Column{Arrow: j + 1, Header: appops.CutText(o, 40), Hover: o, Width: 90, WidthType: "number", DescFirst: true,
			Cell: func(row int64) recordtable.Cell {
				n := m.counts[row][j]
				if n == 0 {
					return recordtable.Cell{}
				}
				return recordtable.Cell{Text: strconv.FormatUint(n, 10), Hover: m.actors[row] + " → " + o + ": " + strconv.FormatUint(n, 10) +
					" rows; last outcome " + m.last[row][j]}
			}})
	}
	g.order = g.sort.OrderFor(g.rec, g.rec.NumRows())
	res := recordtable.RenderGrid(recordtable.Input{Ids: inst.ids, ScopeKey: "trail-matrix", Record: g.rec, Columns: cols, Sort: &g.sort,
		Order: g.order, Selected: -1, State: &g.state, RowHeight: 22})
	g.sort.Apply(res)
}

// matrixGrid is the matrix as an Arrow record for the grid: the actor, then
// one count column per object.
type matrixGrid struct {
	rec   arrow.RecordBatch
	ver   uint64
	state recordtable.State
	sort  recordtable.SortState
	order []int64
}

func (inst *matrixGrid) release() {
	if inst.rec != nil {
		inst.rec.Release()
		inst.rec = nil
	}
}

func (inst *matrixGrid) build(m *matrixData, ver uint64) {
	if inst.rec != nil && inst.ver == ver {
		return
	}
	inst.release()
	inst.ver = ver
	mem := memory.DefaultAllocator
	fields := make([]arrow.Field, 0, len(m.objects)+1)
	arrs := make([]arrow.Array, 0, len(m.objects)+1)
	sb := array.NewStringBuilder(mem)
	sb.AppendValues(m.actors, nil)
	arrs = append(arrs, sb.NewArray())
	sb.Release()
	fields = append(fields, arrow.Field{Name: m.actor.String(), Type: arrow.BinaryTypes.String})
	for j, o := range m.objects {
		b := array.NewInt64Builder(mem)
		for i := range m.actors {
			b.Append(int64(m.counts[i][j]))
		}
		arrs = append(arrs, b.NewArray())
		b.Release()
		fields = append(fields, arrow.Field{Name: o, Type: arrow.PrimitiveTypes.Int64})
	}
	inst.rec = array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(len(m.actors)))
	for _, a := range arrs {
		a.Release()
	}
}

// renderDotted is the dotted chart tab: the row pick, the line, and the
// scatter with a row per case row and time on x.
func (inst *App) renderDotted() {
	an := &inst.an
	for range c.HorizontalTop().KeepIter() {
		an.row = DottedRowE(inst.pickCombo("dt-row", "a row per", dottedRowNames[:], int(an.row)))
	}
	if inst.cache.step <= 0 {
		c.Label("(waiting for the table's first read)").Send()
		return
	}
	d, ok := aggStatus(&an.dotted, inst.dottedKey())
	if !ok {
		return
	}
	c.Label(d.line()).Wrap().Send()
	if len(d.keys) == 0 {
		return
	}
	if w, _, ok := c.CapturePaneSize(inst.ids.ProbeSeq("dotted-pane")); ok && w > 100 {
		an.plotW = w
	}
	w := an.plotW
	if w <= 0 {
		w = stripFallbackWidth
	}
	h := float32(max(len(d.keys), 3))*dottedRowPx + 60
	p := implot.Begin(inst.ids, "##trail-dotted", w, h)
	p.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
	p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
	if from, to := inst.cache.from, inst.cache.to; !from.IsZero() {
		p.SetupAxisLimits(implot.AxisX1, float64(from.Unix()), float64(to.Unix()), implot.CondAlways)
	}
	p.SetupAxisLimits(implot.AxisY1, -0.75, float64(len(d.keys))-0.25, implot.CondAlways)
	ticks := make([]float64, len(d.keys))
	labels := make([]string, len(d.keys))
	for i, k := range d.keys {
		ticks[i], labels[i] = float64(len(d.keys)-1-i), appops.CutText(k, 28)
	}
	p.SetupAxisTicks(implot.AxisY1, ticks, labels)
	for t := range d.tiers {
		tier := &d.tiers[t]
		if len(tier.xs) == 0 {
			continue
		}
		label := strconv.FormatUint(tier.minN, 10)
		if tier.maxN != tier.minN {
			label += "–" + strconv.FormatUint(tier.maxN, 10)
		}
		p.SetNextColor(seriesColor(0))
		p.Scatter(label+" rows", tier.xs, tier.ys, implot.MarkerCircle, tier.radius)
	}
	p.End()
}
