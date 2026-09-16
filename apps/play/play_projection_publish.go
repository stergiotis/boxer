package play

import (
	"fmt"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/explain"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_projection_publish.go publishes a projection run as two ad-hoc
// datasets (ADR-0238 update, over ADR-0134's store), so what the tab
// computed becomes something a query can name: one row per projected
// entity with its identity, its features, its cluster and its position,
// and one row per cluster and reading with the rule as SQL. The feature
// rules the section shows then run as written against
// the published rows, and the attribute contrasts are a GROUP BY over the
// items column. Modelled on imzrt's profile publish: off the render
// thread, single-flight, each dataset republished onto its own stable
// handle so its revision bumps, and the query naming the dataset by that
// handle as a literal — no alias binding, so any play instance, this one
// or another, reads it the same way.

const (
	// projectionAlias and projectionRulesAlias label the datasets in the
	// ad-hoc catalogue; a query names them by handle.
	projectionAlias      = "projection"
	projectionRulesAlias = "projection_rules"
)

// projectionPublishState is the publish round's state: the two publishers
// — one handle each across rounds, republished onto rather than replaced —
// and the round's outcome, shared between the render thread and the
// goroutine under mu.
type projectionPublishState struct {
	rows, rules *adhocdata.Publisher

	mu         sync.Mutex
	publishing bool
	err        error
	summary    string
	// generation counts successful publishes, so the render thread offers
	// the scaffold exactly once per publish.
	generation uint64
}

func newProjectionPublishState() *projectionPublishState {
	return &projectionPublishState{
		rows:  adhocdata.NewPublisher(projectionAlias, false),
		rules: adhocdata.NewPublisher(projectionRulesAlias, false),
	}
}

func (inst *projectionPublishState) status() (publishing bool, summary string, gen uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.publishing, inst.summary, inst.generation, inst.err
}

// projectionPublishInput is what one round reads off the run, captured
// on the render thread so the goroutine touches no render state.
type projectionPublishInput struct {
	rec        arrow.RecordBatch
	res        *projectionResult
	depth      int
	perCluster bool
	// x, y are the widget's positions per slot, world units.
	x, y []float32
}

// publishProjection encodes and publishes the current run. A second click
// while one is in flight is dropped rather than queued.
func (inst *PlayApp) publishProjection(in projectionPublishInput) {
	if inst.bus == nil || inst.projPublish == nil {
		return
	}
	st := inst.projPublish
	st.mu.Lock()
	if st.publishing {
		st.mu.Unlock()
		return
	}
	st.publishing = true
	st.err = nil
	st.mu.Unlock()
	in.rec.Retain()
	go func() {
		defer in.rec.Release()
		summary, err := doPublishProjection(inst.bus, in, st)
		st.mu.Lock()
		st.publishing = false
		st.err = err
		if err == nil {
			st.summary = summary
			st.generation++
		}
		st.mu.Unlock()
	}()
}

// doPublishProjection is one round: encode both datasets, publish both onto
// the publishers' held handles.
func doPublishProjection(bus busPublisherI, in projectionPublishInput, st *projectionPublishState) (summary string, err error) {
	alloc := memory.NewGoAllocator()
	rows, err := buildProjectionRows(in, alloc)
	if err != nil {
		return
	}
	defer rows.Release()
	rules := buildProjectionRules(in, alloc)
	defer rules.Release()
	rowsIPC, err := encodeArrowStream(rows)
	if err != nil {
		return
	}
	rulesIPC, err := encodeArrowStream(rules)
	if err != nil {
		return
	}
	rowsRes, err := st.rows.Publish(bus, rowsIPC)
	if err != nil {
		return "", eb.Build().Str("alias", projectionAlias).Errorf("play: projection: publish: %w", err)
	}
	rulesRes, err := st.rules.Publish(bus, rulesIPC)
	if err != nil {
		return "", eb.Build().Str("alias", projectionRulesAlias).Errorf("play: projection: publish: %w", err)
	}
	summary = fmt.Sprintf("keelson('%s'): %d rows (rev %d) · keelson('%s'): %d rules (rev %d)",
		rowsRes.Handle, rowsRes.Rows, rowsRes.Revision, rulesRes.Handle, rulesRes.Rows, rulesRes.Revision)
	return summary, nil
}

// buildProjectionRows is the per-entity dataset: the result's row index
// and its identity columns (every column that is not a tagged-section
// column), the sixteen features in their own units, the cluster and its
// probability, the layout position, the feature set the graph was built
// over, and the item set the attribute reading used.
func buildProjectionRows(in projectionPublishInput, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	res := in.res
	n := len(res.rows)
	if len(res.slotFeatures) != n {
		err = eb.Build().Int("slots", n).Int("features", len(res.slotFeatures)).Errorf("play: projection: the run carries no features to publish")
		return
	}
	fields := []arrow.Field{{Name: "row", Type: arrow.PrimitiveTypes.Int64}}
	cols := []arrow.Array{}
	rowIdx := array.NewInt64Builder(alloc)
	rowIdx.AppendValues(res.rows, nil)
	cols = append(cols, rowIdx.NewArray())
	rowIdx.Release()

	// Identity: the result's plain columns, gathered in slot order.
	schema := in.rec.Schema()
	for i, f := range schema.Fields() {
		if strings.HasPrefix(f.Name, "tv:") {
			continue
		}
		gathered, gErr := gatherRows(in.rec.Column(i), res.rows, alloc)
		if gErr != nil {
			err = eb.Build().Str("column", f.Name).Errorf("play: projection: gather: %w", gErr)
			return
		}
		fields = append(fields, arrow.Field{Name: f.Name, Type: f.Type, Nullable: f.Nullable})
		cols = append(cols, gathered)
	}

	// Features, raw units, under the names the rules use.
	names := card.FeatureNames()
	for fi := range card.NumFeatures {
		b := array.NewFloat64Builder(alloc)
		b.Reserve(n)
		for s := range n {
			b.Append(res.slotFeatures[s].AsSlice()[fi])
		}
		fields = append(fields, arrow.Field{Name: names[fi], Type: arrow.PrimitiveTypes.Float64})
		cols = append(cols, b.NewArray())
		b.Release()
	}

	// The clustering and the picture.
	cl := array.NewInt32Builder(alloc)
	pr := array.NewFloat32Builder(alloc)
	xb := array.NewFloat32Builder(alloc)
	yb := array.NewFloat32Builder(alloc)
	fs := array.NewStringBuilder(alloc)
	for s := range n {
		lb, p := int32(-1), float32(0)
		if s < len(res.clusters.Label) {
			lb = res.clusters.Label[s]
			p = res.clusters.Probability[s]
		}
		// The dataset numbers clusters as the tab does, from one; noise
		// stays at −1 with its meaning from the widget's legend.
		if lb >= 0 {
			lb++
		}
		cl.Append(lb)
		pr.Append(p)
		x, y := float32(0), float32(0)
		if s < len(in.x) {
			x, y = in.x[s], in.y[s]
		}
		xb.Append(x)
		yb.Append(y)
		fs.Append(res.params.FeatureSet.String())
	}
	for _, col := range []struct {
		name string
		b    array.Builder
		t    arrow.DataType
	}{
		{"cluster", cl, arrow.PrimitiveTypes.Int32},
		{"probability", pr, arrow.PrimitiveTypes.Float32},
		{"x", xb, arrow.PrimitiveTypes.Float32},
		{"y", yb, arrow.PrimitiveTypes.Float32},
		{"feature_set", fs, arrow.BinaryTypes.String},
	} {
		fields = append(fields, arrow.Field{Name: col.name, Type: col.t})
		cols = append(cols, col.b.NewArray())
		col.b.Release()
	}

	// The item set per entity, by item name.
	items := array.NewListBuilder(alloc, arrow.BinaryTypes.String)
	vb := items.ValueBuilder().(*array.StringBuilder)
	sets := res.explanation.items.sets
	for s := range n {
		items.Append(true)
		if s < len(sets.Rows) {
			for _, it := range sets.Rows[s] {
				vb.Append(sets.Items[it].Name)
			}
		}
	}
	fields = append(fields, arrow.Field{Name: "items", Type: arrow.ListOf(arrow.BinaryTypes.String)})
	cols = append(cols, items.NewArray())
	items.Release()

	rec = array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(n))
	for _, col := range cols {
		col.Release()
	}
	return
}

// gatherRows is arr at the given rows, in that order: one-row slices
// concatenated, which is generic over the column's type.
func gatherRows(arr arrow.Array, rows []int64, alloc memory.Allocator) (out arrow.Array, err error) {
	parts := make([]arrow.Array, len(rows))
	for i, r := range rows {
		if r < 0 || r >= int64(arr.Len()) {
			err = eb.Build().Int64("row", r).Int("len", arr.Len()).Errorf("row out of range")
			return
		}
		parts[i] = array.NewSlice(arr, r, r+1)
	}
	defer func() {
		for _, p := range parts {
			if p != nil {
				p.Release()
			}
		}
	}()
	if len(parts) == 0 {
		return array.NewSlice(arr, 0, 0), nil
	}
	out, err = array.Concatenate(parts, alloc)
	if err != nil {
		err = eh.Errorf("concatenate: %w", err)
	}
	return
}

// buildProjectionRules is the per-cluster dataset: for each cluster the
// feature rule at the published depth and the attribute rule, each as the
// SQL predicate the section shows, with its coverage.
func buildProjectionRules(in projectionPublishInput, alloc memory.Allocator) arrow.RecordBatch {
	ex := in.res.explanation
	cluster := array.NewInt32Builder(alloc)
	kind := array.NewStringBuilder(alloc)
	rule := array.NewStringBuilder(alloc)
	prec := array.NewFloat32Builder(alloc)
	rec := array.NewFloat32Builder(alloc)
	rows := array.NewInt32Builder(alloc)
	hits := array.NewInt32Builder(alloc)
	n := 0
	add := func(lb int, k, sql string, p, r float32, cover, hit int32) {
		cluster.Append(int32(lb + 1))
		kind.Append(k)
		rule.Append(sql)
		prec.Append(p)
		rec.Append(r)
		rows.Append(cover)
		hits.Append(hit)
		n++
	}
	if ex.tree != nil {
		for lb := range ex.tree.NumLabels {
			var rules []explain.Rule
			if in.perCluster {
				if lb < len(ex.perCluster) && ex.perCluster[lb] != nil {
					rules = ex.perCluster[lb].RulesFor(in.depth, 1)
				}
			} else {
				rules = ex.tree.RulesFor(in.depth, int32(lb))
			}
			if len(rules) == 0 {
				continue
			}
			cover, hit, p, r := explain.Coverage(rules)
			add(lb, "features", rulesSQL(rules, ex.desc), p, r, cover, hit)
		}
	}
	pi := ex.items
	if pi.contrast != nil {
		spell := func(item int32) (string, bool) {
			if int(item) < len(pi.sql) && pi.hasSQL[item] {
				return pi.sql[item], true
			}
			return "", false
		}
		itemName := func(item int32) string { return pi.sets.Items[item].Name }
		for lb, sg := range pi.subgroups {
			if len(sg.Literals) == 0 {
				continue
			}
			sql, _ := sg.SQL(spell, itemName)
			add(lb, "attributes", sql, sg.Precision, sg.Recall, sg.Rows, sg.Hits)
		}
	}
	fields := []arrow.Field{
		{Name: "cluster", Type: arrow.PrimitiveTypes.Int32},
		{Name: "kind", Type: arrow.BinaryTypes.String},
		{Name: "rule", Type: arrow.BinaryTypes.String},
		{Name: "precision", Type: arrow.PrimitiveTypes.Float32},
		{Name: "recall", Type: arrow.PrimitiveTypes.Float32},
		{Name: "rows", Type: arrow.PrimitiveTypes.Int32},
		{Name: "hits", Type: arrow.PrimitiveTypes.Int32},
	}
	cols := []arrow.Array{cluster.NewArray(), kind.NewArray(), rule.NewArray(), prec.NewArray(), rec.NewArray(), rows.NewArray(), hits.NewArray()}
	for _, b := range []array.Builder{cluster, kind, rule, prec, rec, rows, hits} {
		b.Release()
	}
	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(n))
	for _, col := range cols {
		col.Release()
	}
	return out
}

// syncProjectionPublish binds the aliases to the handles the round minted
// and offers the scaffold once per publish. Render thread — BindDataset
// and InsertSqlAtCaret both belong there.
func (inst *PlayApp) syncProjectionPublish() {
	if inst.projPublish == nil {
		return
	}
	_, _, gen, _ := inst.projPublish.status()
	if gen == inst.projPublishSeen {
		return
	}
	inst.projPublishSeen = gen
	rowsHandle, rulesHandle := inst.projPublish.rows.Handle(), inst.projPublish.rules.Handle()
	if rowsHandle == "" || rulesHandle == "" {
		return
	}
	// The scaffold names the aliases; the binding makes them this window's
	// datasets, so the same text reads the same way in a window that
	// follows the aliases by launch config (ADR-0240 §SD7).
	for _, b := range []struct{ alias, handle string }{
		{projectionAlias, rowsHandle},
		{projectionRulesAlias, rulesHandle},
	} {
		if bErr := inst.BindDataset(b.alias, b.handle); bErr != nil {
			return
		}
	}
	inst.InsertSqlAtCaret(projectionScaffold())
}

// projectionScaffold is the query offered after a publish: the clusters
// with their sizes and rules, ready to narrow to one. It names the
// aliases, which this window binds to the handles it minted.
func projectionScaffold() string {
	return fmt.Sprintf(`
-- the projection as data: one row per entity, one per cluster and rule
SELECT p.cluster, count() AS entities, any(r.rule) AS rule
FROM keelson('%s') AS p
LEFT JOIN keelson('%s') AS r ON r.cluster = p.cluster AND r.kind = 'attributes'
GROUP BY p.cluster ORDER BY entities DESC
`, projectionAlias, projectionRulesAlias)
}

// renderProjectionPublish is the toolbar affordance: the button while a
// run is done and a bus is there to publish on, the last summary or error
// beside it.
func (inst *PlayApp) renderProjectionPublish(in func() projectionPublishInput) {
	if inst.bus == nil || inst.projPublish == nil {
		return
	}
	publishing, summary, _, err := inst.projPublish.status()
	label := "publish as dataset"
	if publishing {
		label = "publishing…"
	}
	if c.Button(inst.ids.PrepareStr("projectionPublish"), c.Atoms().Text(label).Keep()).
		SendResp().HasPrimaryClicked() && !publishing {
		inst.publishProjection(in())
	}
	switch {
	case err != nil:
		c.Label(fmt.Sprintf("publish failed: %s", err)).Wrap().Send()
	case summary != "":
		for rt := range c.RichTextLabel(summary) {
			rt.Small().Weak()
		}
	}
}
