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
	// items is the attribute-level reading (play_projection_items.go).
	items projectionItems
}

// explainProjection fits the tree and the contrasts over the raw feature
// values in slot order. Raw rather than the preprocessed matrix: a
// threshold tree and a rank statistic are indifferent to the monotone
// transforms the preprocessing applies, and the thresholds then read in
// the feature's own units.
func explainProjection(ctx context.Context, slotFeatures []card.EntityFeatures, cl algo.HDBSCANResult) (ex projectionExplanation) {
	if cl.NumClusters == 0 || len(slotFeatures) == 0 {
		return
	}
	x := card.BuildFeatureMatrix(slotFeatures).RawMatrix().Data
	labels := cl.Label
	fitted := 0
	for _, lb := range labels {
		if lb >= 0 {
			fitted++
		}
	}
	minLeaf := max(projectionExplainMinLeafFloor, int(math.Round(projectionExplainMinLeafShare*float64(fitted))))
	tree, err := explain.FitTree(ctx, x, card.NumFeatures, labels, explain.TreeOptions{
		MaxDepth: projectionExplainFitDepth,
		MinLeaf:  minLeaf,
	})
	if err != nil {
		ex.err = err
		return
	}
	contrast, err := explain.Contrasts(ctx, x, card.NumFeatures, labels)
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
		t, err := explain.FitOneVsRest(ctx, x, card.NumFeatures, labels, lb, explain.TreeOptions{
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
func explanationRows(ex projectionExplanation, depth int, names []string, perCluster bool) (rows []explanationRow) {
	if ex.tree == nil || ex.contrast == nil {
		return
	}
	name := func(f int32) string {
		if int(f) < len(names) {
			return names[f]
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
			row.rule = explain.SQL(rules, name)
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
			fmt.Fprintf(&b, "%s %s (%.3g vs %.3g; AUC %.2f)", name(f), arrow, cell.Median, cell.MedianRest, cell.AUC)
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
			} else if res.params.FeatureSet != projectionFeatureShape {
				// The clustering ran on structure or components, so these
				// rules describe each cluster's shape; the attribute rules
				// are its criterion.
				summary = "clustered by " + res.params.FeatureSet.String() + " — shape rules are a description here, the attribute rules the criterion · " + summary
			}
			for rt := range c.RichTextLabel(summary) {
				rt.Small().Weak()
			}
		}
		var rows []explanationRow
		ruleHeader, apartHeader := "rule (SQL over the feature columns)", "what sets it apart (median in vs rest)"
		if inst.explainByItems {
			rows = itemsRows(ex.items, ex.tree.NumLabels)
			ruleHeader, apartHeader = "what they are (SQL over the physical columns)", "what sets it apart (share in vs rest)"
		} else {
			rows = explanationRows(ex, inst.explainDepth, card.FeatureNames(), inst.explainPerCluster)
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
