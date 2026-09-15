package play

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/explain"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_projection_explain.go is the Projection tab's third step (ADR-0235
// §SD3): once HDBSCAN has coloured the neighbour graph, the labels are read
// back off the raw feature matrix as a threshold tree and as per-cluster
// contrasts, and the tab shows, per cluster, the rule that reproduces it
// and the features that set it apart. The fit runs in the projection
// goroutine after the clustering; the depth the rules are read at is a
// view knob, since a greedy tree's shallower cut is its shallower fit.

const (
	// projectionExplainFitDepth is the depth the tree is fitted to; the
	// slider reads it at any cut up to this.
	projectionExplainFitDepth = 6
	// projectionExplainDefaultDepth is the cut shown first: three terms
	// is what a rule can carry and still read as a sentence.
	projectionExplainDefaultDepth = 3
	// projectionExplainMinLeafShare and projectionExplainMinLeafFloor size
	// the smallest leaf as a share of the clustered rows, floored, so a
	// ten-thousand-row picture does not read rules over a handful of rows.
	projectionExplainMinLeafShare = 0.005
	projectionExplainMinLeafFloor = 3
	// projectionExplainTopFeatures is how many contrasts a cluster lists.
	projectionExplainTopFeatures = 3
	// projectionExplainEffectFloor is the least effect (|AUC − ½|) a
	// contrast needs to be listed: under it a feature's members and
	// non-members overlap for the most part.
	projectionExplainEffectFloor = 0.15
	// projectionExplainMaxHeight bounds the cluster table, which scrolls
	// past it, so the graph under the section keeps its canvas.
	projectionExplainMaxHeight = 220
)

// projectionExplanation is the run's explanation, immutable once
// published beside the projectionResult. err is set when the fit failed;
// tree and contrast are nil when there was nothing to explain. perCluster
// holds one tree per label fitted against the rest (ADR-0235 §SD1a), nil
// where the fit was cut short.
type projectionExplanation struct {
	tree       *explain.Tree
	perCluster []*explain.Tree
	contrast   *explain.Contrast
	err        error
	// desc is the matrix the trees and contrasts were read over — the
	// feature set the clustering ran on — and labels the clustering.
	desc   projectionFeatureDesc
	labels []int32
	// items is the attribute-level reading (play_projection_items.go).
	items projectionItems
}

// projectionFeatureDesc is a feature matrix with its spellings: the
// sixteen shape features in their own units, or a binary matrix — one
// column per registered component kind, or per structural item — whose
// columns spell as predicates rather than thresholds. The explanation
// reads the matrix the clustering ran on (ADR-0238), so a rule is a
// criterion of the picture and not a description of another space.
type projectionFeatureDesc struct {
	x     []float64
	d     int
	names []string
	// what names the columns in a summary: "shape features", "component
	// kinds", "structural items".
	what string
	// binary marks 0/1 columns; spell gives such a column's predicate, or
	// false when it has none.
	binary bool
	spell  func(f int32) (pred string, ok bool)
}

// shapeFeatureDesc is the sixteen shape features, raw: a threshold tree and
// a rank statistic are indifferent to the monotone transforms the
// preprocessing applies, and the thresholds then read in the feature's own
// units.
func shapeFeatureDesc(slotFeatures []card.EntityFeatures) projectionFeatureDesc {
	return projectionFeatureDesc{
		x: card.BuildFeatureMatrix(slotFeatures).RawMatrix().Data, d: card.NumFeatures,
		names: card.FeatureNames(), what: "shape features",
	}
}

// binaryFeatureDesc is a 0/1 matrix over n rows: rows[r] lists the
// columns row r holds.
func binaryFeatureDesc(names []string, rows [][]int32, what string, spell func(f int32) (string, bool)) projectionFeatureDesc {
	d := len(names)
	x := make([]float64, len(rows)*d)
	for r, row := range rows {
		for _, c := range row {
			if int(c) < d {
				x[r*d+int(c)] = 1
			}
		}
	}
	return projectionFeatureDesc{x: x, d: d, names: names, what: what, binary: true, spell: spell}
}

// projectionStructureExplainItems caps the structural items a structure
// run's trees read, by support: a tree's cost is linear in the columns.
const projectionStructureExplainItems = 64

// structureFeatureDesc is the pruned item sets' structural items — sections,
// co-groups, memberships, components; never values — as a binary matrix,
// the most frequent first, spelled as the attribute reading spells them.
func structureFeatureDesc(sets card.ItemSets) projectionFeatureDesc {
	keep := make([]int32, 0, len(sets.Items))
	for i, it := range sets.Items {
		switch it.Kind {
		case card.ItemKindTaggedValue:
			continue
		}
		keep = append(keep, int32(i))
	}
	if len(keep) > projectionStructureExplainItems {
		keep = keep[:projectionStructureExplainItems] // Prune orders by support
	}
	col := make(map[int32]int32, len(keep))
	names := make([]string, len(keep))
	for c, i := range keep {
		col[i] = int32(c)
		names[c] = sets.Items[i].Name
	}
	rows := make([][]int32, len(sets.Rows))
	for r, row := range sets.Rows {
		for _, i := range row {
			if c, ok := col[i]; ok {
				rows[r] = append(rows[r], c)
			}
		}
	}
	return binaryFeatureDesc(names, rows, "structural items", func(f int32) (string, bool) {
		return itemPredicate(sets.Items[keep[f]])
	})
}

// componentFeatureDesc is one column per registered component kind.
func componentFeatureDesc(kinds []string, rows [][]int32) projectionFeatureDesc {
	return binaryFeatureDesc(kinds, rows, "component kinds", func(f int32) (string, bool) {
		return "LW_COMPONENT_FILTER('" + kinds[f] + "')", true
	})
}

// explainProjection fits the partition tree, one tree per cluster and the
// contrasts over desc in slot order.
func explainProjection(ctx context.Context, desc projectionFeatureDesc, cl algo.HDBSCANResult) (ex projectionExplanation) {
	ex.desc = desc
	ex.labels = cl.Label
	if cl.NumClusters == 0 || desc.d == 0 || len(desc.x) != desc.d*len(cl.Label) {
		return
	}
	x := desc.x
	labels := cl.Label
	fitted := 0
	for _, lb := range labels {
		if lb >= 0 {
			fitted++
		}
	}
	minLeaf := max(projectionExplainMinLeafFloor, int(math.Round(projectionExplainMinLeafShare*float64(fitted))))
	tree, err := explain.FitTree(ctx, x, desc.d, labels, explain.TreeOptions{
		MaxDepth: projectionExplainFitDepth,
		MinLeaf:  minLeaf,
	})
	if err != nil {
		ex.err = err
		return
	}
	contrast, err := explain.Contrasts(ctx, x, desc.d, labels)
	if err != nil {
		ex.err = err
		return
	}
	ex.tree = tree
	ex.contrast = contrast
	// One tree per cluster against the rest: what is in this cluster. A
	// cancel mid-way leaves the remaining entries nil; the section says so.
	ex.perCluster = make([]*explain.Tree, tree.NumLabels)
	for lb := range int32(tree.NumLabels) {
		if ctx.Err() != nil {
			break
		}
		t, err := explain.FitOneVsRest(ctx, x, desc.d, labels, lb, explain.TreeOptions{
			MaxDepth: projectionExplainFitDepth,
			MinLeaf:  minLeaf,
		})
		if err != nil {
			continue
		}
		ex.perCluster[lb] = t
	}
	return
}

// explanationRow is one cluster's line of the explanation table, text
// only, so the table is unit-tested without the widget.
type explanationRow struct {
	cluster string
	rows    string
	// rule is the cluster's SQL predicate over the feature columns: the
	// disjunction of the leaves that predict it at the cut depth.
	rule     string
	fit      string
	features string
}

// explanationRows reads the explanation at the given rule depth into one
// row per cluster. perCluster reads each cluster's own one-versus-rest
// tree; otherwise the partition tree's leaves for the cluster. Either way
// the rule is the SQL disjunction of those leaves with the coverage it
// earns, and the strongest contrasts with their medians follow.
func explanationRows(ex projectionExplanation, depth int, perCluster bool) (rows []explanationRow) {
	if ex.tree == nil || ex.contrast == nil {
		return
	}
	desc := ex.desc
	name := func(f int32) string {
		if int(f) < len(desc.names) {
			return desc.names[f]
		}
		return fmt.Sprintf("f%d", f)
	}
	for lb := range ex.tree.NumLabels {
		row := explanationRow{
			cluster: fmt.Sprintf("cluster %d", lb+1),
			rows:    fmt.Sprintf("%d", ex.tree.Nodes[0].Counts[lb]),
		}
		var rules []explain.Rule
		fitted := true
		if perCluster {
			if lb < len(ex.perCluster) && ex.perCluster[lb] != nil {
				rules = ex.perCluster[lb].RulesFor(depth, 1)
			} else {
				fitted = false
			}
		} else {
			rules = ex.tree.RulesFor(depth, int32(lb))
		}
		switch {
		case !fitted:
			row.rule = "not fitted (cut short)"
		case len(rules) == 0:
			row.rule = "no leaf at this depth"
		default:
			row.rule = rulesSQL(rules, desc)
			_, _, precision, recall := explain.Coverage(rules)
			row.fit = fmt.Sprintf("precision %.0f%% · recall %.0f%%", 100*precision, 100*recall)
			if len(rules) > 1 {
				row.fit += fmt.Sprintf(" · %d leaves", len(rules))
			}
		}
		var b strings.Builder
		listed := 0
		for _, f := range ex.contrast.Ranked(int32(lb)) {
			cell := ex.contrast.Cell(int32(lb), f)
			if cell.Effect() < projectionExplainEffectFloor || listed == projectionExplainTopFeatures {
				break
			}
			if listed > 0 {
				b.WriteString(" · ")
			}
			arrow := "↑"
			if cell.AUC < 0.5 {
				arrow = "↓"
			}
			if desc.binary {
				in, rest := binaryShares(desc, ex.labels, int32(lb), f)
				fmt.Fprintf(&b, "%s %s (%.0f%% vs %.0f%%)", name(f), arrow, 100*in, 100*rest)
			} else {
				fmt.Fprintf(&b, "%s %s (%.3g vs %.3g; AUC %.2f)", name(f), arrow, cell.Median, cell.MedianRest, cell.AUC)
			}
			listed++
		}
		if listed == 0 {
			b.WriteString("no single feature stands out")
		}
		row.features = b.String()
		rows = append(rows, row)
	}
	return
}

// binaryShares is the share of the label's rows holding column f, and the
// share of every other row holding it.
func binaryShares(desc projectionFeatureDesc, labels []int32, label, f int32) (in, rest float64) {
	var nIn, nRest, hitIn, hitRest float64
	for r, lb := range labels {
		v := desc.x[r*desc.d+int(f)]
		if lb == label {
			nIn++
			hitIn += v
		} else {
			nRest++
			hitRest += v
		}
	}
	if nIn > 0 {
		in = hitIn / nIn
	}
	if nRest > 0 {
		rest = hitRest / nRest
	}
	return
}

// rulesSQL spells a rule set over desc: thresholds over feature names for
// a numeric matrix; for a binary one each term is the column's predicate,
// negated when the term keeps the column at zero.
func rulesSQL(rules []explain.Rule, desc projectionFeatureDesc) string {
	name := func(f int32) string {
		if int(f) < len(desc.names) {
			return desc.names[f]
		}
		return fmt.Sprintf("f%d", f)
	}
	if !desc.binary {
		return explain.SQL(rules, name)
	}
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		if len(r.Terms) == 0 {
			parts = append(parts, "true")
			continue
		}
		var b strings.Builder
		for i, t := range r.Terms {
			if i > 0 {
				b.WriteString(" AND ")
			}
			pred, ok := "", false
			if desc.spell != nil {
				pred, ok = desc.spell(t.Feature)
			}
			if !ok {
				pred = "/* " + name(t.Feature) + " has no SQL spelling */ true"
			}
			if t.Above {
				b.WriteString(pred)
			} else {
				b.WriteString("NOT (" + pred + ")")
			}
		}
		parts = append(parts, b.String())
	}
	switch len(parts) {
	case 0:
		return "false"
	case 1:
		return parts[0]
	}
	return "(" + strings.Join(parts, ") OR (") + ")"
}

// explanationSummary is the line above the table: how much of the
// clustering the rules at this depth reproduce, and the tree's size.
func explanationSummary(ex projectionExplanation, depth int, perCluster bool) string {
	if ex.tree == nil {
		return ""
	}
	var s string
	if perCluster {
		s = fmt.Sprintf("one tree per cluster against the rest, noise included, read at depth %d · smallest leaf %d rows",
			depth, ex.tree.Options.MinLeaf)
	} else {
		agree, fitted := ex.tree.Fidelity(depth)
		pct := 0.0
		if fitted > 0 {
			pct = 100 * float64(agree) / float64(fitted)
		}
		s = fmt.Sprintf("one partition: rules at depth %d reproduce the clustering for %d of %d clustered rows (%.0f%%) · %d leaves · smallest leaf %d rows",
			depth, agree, fitted, pct, len(ex.tree.Rules(depth)), ex.tree.Options.MinLeaf)
	}
	s += fmt.Sprintf(" · over %d %s", ex.desc.d, ex.desc.what)
	if ex.tree.Truncation.Truncated || ex.contrast.Truncation.Truncated {
		s += " · cut short"
	}
	return s
}

// renderExplanation draws the "why these clusters" section under the
// status line: the depth slider, the summary, one row per cluster.
func (inst *Projector) renderExplanation(res *projectionResult) {
	ids := inst.ids
	ex := res.explanation
	for range c.CollapsingHeader(ids.PrepareStr("projectionExplain"),
		c.WidgetText().Text("why these clusters").Keep()).KeepIter() {
		switch {
		case ex.err != nil:
			c.Label(fmt.Sprintf("explanation failed: %s", ex.err)).Wrap().Send()
			continue
		case ex.tree == nil:
			for rt := range c.RichTextLabel("No clusters to explain — lower min cluster and recompute.") {
				rt.Small().Weak()
			}
			continue
		}
		// A ceiling, not a size, set before anything is placed, as the
		// Map's table editor does: the slider row stays, the table under
		// it scrolls past the ceiling, and the graph below keeps its
		// canvas.
		c.UiSetMaxHeight(projectionExplainMaxHeight)
		for range c.Horizontal().KeepIter() {
			c.SliderF64(ids.PrepareStr("projectionExplainDepth"), inst.explainDepthKnob, 1, projectionExplainFitDepth).
				Integer().Text("rule depth").SendRespVal(&inst.explainDepthKnob)
			inst.explainDepth = int(math.Round(inst.explainDepthKnob))
			c.Checkbox(ids.PrepareStr("projectionExplainPerCluster"), inst.explainPerCluster, "one tree per cluster").SendRespVal(&inst.explainPerCluster)
			c.Checkbox(ids.PrepareStr("projectionExplainByItems"), inst.explainByItems, "by attributes").SendRespVal(&inst.explainByItems)
			summary := explanationSummary(ex, inst.explainDepth, inst.explainPerCluster)
			if inst.explainByItems {
				summary = itemsSummary(ex.items)
			}
			for rt := range c.RichTextLabel(summary) {
				rt.Small().Weak()
			}
		}
		var rows []explanationRow
		ruleHeader, apartHeader := "rule (SQL over the "+ex.desc.what+")", "what sets it apart (median in vs rest)"
		if ex.desc.binary {
			apartHeader = "what sets it apart (share in vs rest)"
		}
		if inst.explainByItems {
			rows = itemsRows(ex.items, ex.tree.NumLabels)
			ruleHeader, apartHeader = "what they are (SQL over column handles)", "what sets it apart (share in vs rest)"
		} else {
			rows = explanationRows(ex, inst.explainDepth, inst.explainPerCluster)
		}
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, true).KeepIter() {
			inst.renderExplanationGrid(rows, ruleHeader, apartHeader)
		}
	}
}

// renderExplanationGrid is the cluster table: a header row, then one row
// per cluster.
func (inst *Projector) renderExplanationGrid(rows []explanationRow, ruleHeader, apartHeader string) {
	ids := inst.ids
	for range c.Grid(ids.PrepareStr("projectionExplainGrid")).NumColumns(4).Striped(true).KeepIter() {
		for _, h := range [...]string{"cluster", "rows", ruleHeader, apartHeader} {
			for rt := range c.RichTextLabel(h) {
				rt.Strong()
			}
		}
		c.EndRow()
		for i, r := range rows {
			c.Label(r.cluster).Selectable(false).Send() // designlint:ignore=L1 (a row caption)
			c.Label(r.rows).Send()
			for range c.Vertical().KeepIter() {
				c.Label(r.rule).Wrap().Send()
				if r.fit != "" {
					for range c.Horizontal().KeepIter() {
						for rt := range c.RichTextLabel(r.fit) {
							rt.Small().Weak()
						}
						if inst.copyText != nil {
							if c.Button(ids.PrepareSeq(uint64(0x3100+i)), c.Atoms().Text("copy SQL").Keep()).
								Small().SendResp().HasPrimaryClicked() {
								inst.copyText(r.rule)
							}
						}
					}
				}
			}
			c.Label(r.features).Wrap().Send()
			c.EndRow()
		}
	}
}
