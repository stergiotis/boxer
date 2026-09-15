package play

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/analytics/explain"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
)

// play_projection_items.go is the attribute-level reading of the clusters
// (ADR-0235 §SD6): what the entities of a cluster *are*, in terms of the
// card's sections, values and low-cardinality memberships, rather than the
// shape features the clustering was computed from. Items are binary, so
// the readings are the descriptive-rule-discovery ones — per item a share
// in the cluster against the rest with a corrected exact test, per cluster
// the best small conjunction — and a rule spells as SQL over the physical
// columns, which is where it can actually run.

const (
	// projectionItemsMinSupportShare and projectionItemsMinSupportFloor
	// drop items almost no row has; projectionItemsMaxSupportShare drops
	// items almost every row has. Neither says anything about a cluster.
	projectionItemsMinSupportShare = 0.01
	projectionItemsMinSupportFloor = 5
	projectionItemsMaxSupportShare = 0.99
	// projectionItemsMaxItems caps the vocabulary the search runs over.
	projectionItemsMaxItems = 512
	// projectionItemsTopItems is how many contrasts a cluster lists.
	projectionItemsTopItems = 3
)

// projectionItems is the attribute-level half of an explanation.
type projectionItems struct {
	sets      card.ItemSets
	contrast  *explain.ItemContrast
	subgroups []explain.Subgroup
	// sql is each item's predicate over the physical columns, and hasSQL
	// whether it has one.
	sql    []string
	hasSQL []bool
	err    error
	// candidates is the vocabulary size before pruning.
	candidates int
}

// explainItems reads the labels against the item sets in slot order.
func explainItems(ctx context.Context, sets card.ItemSets, candidates int, cl algo.HDBSCANResult, schema *arrow.Schema) (pi projectionItems) {
	pi.candidates = candidates
	if cl.NumClusters == 0 || len(sets.Rows) == 0 {
		return
	}
	n := len(sets.Rows)
	minSupport := int32(max(projectionItemsMinSupportFloor, int(projectionItemsMinSupportShare*float64(n))))
	maxSupport := int32(projectionItemsMaxSupportShare * float64(n))
	pi.sets = sets.Prune(minSupport, maxSupport, projectionItemsMaxItems)
	pi.sql = make([]string, len(pi.sets.Items))
	pi.hasSQL = make([]bool, len(pi.sets.Items))
	for i, it := range pi.sets.Items {
		pi.sql[i], pi.hasSQL[i] = itemPredicate(it, schema)
	}
	contrast, err := explain.ItemContrasts(ctx, pi.sets.Rows, len(pi.sets.Items), cl.Label)
	if err != nil {
		pi.err = err
		return
	}
	pi.contrast = contrast
	pi.subgroups = make([]explain.Subgroup, contrast.NumLabels)
	for lb := range int32(contrast.NumLabels) {
		if ctx.Err() != nil {
			break
		}
		sg, err := explain.FindSubgroup(ctx, pi.sets.Rows, len(pi.sets.Items), cl.Label, lb, explain.SubgroupOptions{
			Negations: true,
			MinRows:   int(minSupport),
		})
		if err != nil {
			continue
		}
		pi.subgroups[lb] = sg
	}
	return
}

// itemPredicate spells an item as a ClickHouse predicate over the
// physical columns of the result, or reports that it has none: a
// co-group has no column of its own, and a membership's column is found by
// its physical name's prefix — the sink does not see that column, only
// the section's physical name.
func itemPredicate(it card.Item, schema *arrow.Schema) (sql string, ok bool) {
	lit := func() string {
		if !it.Quoted {
			return it.Value
		}
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(it.Value) + "'"
	}
	switch it.Kind {
	case card.ItemKindSection:
		if it.Column == "" {
			return
		}
		return "length(`" + it.Column + "`) > 0", true
	case card.ItemKindTaggedValue:
		return "has(`" + it.Column + "`, " + lit() + ")", true
	case card.ItemKindTagRef:
		col, found := columnByPrefix(schema, "tv:"+it.PhysicalSection+":lr:")
		if !found {
			return
		}
		return "has(`" + col + "`, " + strconv.FormatUint(it.Ref, 10) + ")", true
	case card.ItemKindTagVerbatim:
		col, found := columnByPrefix(schema, "tv:"+it.PhysicalSection+":lv:")
		if !found {
			return
		}
		return "has(`" + col + "`, " + lit() + ")", true
	}
	return
}

// columnByPrefix returns the first field of the schema whose name starts
// with prefix.
func columnByPrefix(schema *arrow.Schema, prefix string) (name string, found bool) {
	if schema == nil {
		return
	}
	for _, f := range schema.Fields() {
		if strings.HasPrefix(f.Name, prefix) {
			return f.Name, true
		}
	}
	return
}

// itemsRows reads the attribute-level explanation into one row per
// cluster: the subgroup as SQL with its coverage, and the items most
// over- or under-represented in the cluster that survive the correction.
func itemsRows(pi projectionItems, numLabels int) (rows []explanationRow) {
	if pi.contrast == nil {
		return
	}
	spell := func(item int32) (string, bool) {
		if int(item) < len(pi.sql) && pi.hasSQL[item] {
			return pi.sql[item], true
		}
		return "", false
	}
	for lb := range numLabels {
		row := explanationRow{
			cluster: fmt.Sprintf("cluster %d", lb+1),
			rows:    fmt.Sprintf("%d", pi.contrast.Rows[lb]),
		}
		switch {
		case lb >= len(pi.subgroups):
			row.rule = "not fitted (cut short)"
		case len(pi.subgroups[lb].Literals) == 0:
			row.rule = "no item combination beats the base rate"
		default:
			sg := pi.subgroups[lb]
			sql, ok := sg.SQL(spell, func(item int32) string { return pi.sets.Items[item].Name })
			row.rule = sql
			if !ok {
				row.rule += "  -- an item has no SQL spelling"
			}
			row.fit = fmt.Sprintf("precision %.0f%% · recall %.0f%% · WRAcc %.3f", 100*sg.Precision, 100*sg.Recall, sg.WRAcc)
		}
		var b strings.Builder
		listed := 0
		for _, it := range pi.contrast.Ranked(int32(lb)) {
			cell := pi.contrast.Cell(int32(lb), it)
			if !cell.Significant || listed == projectionItemsTopItems {
				break
			}
			if listed > 0 {
				b.WriteString(" · ")
			}
			arrow := "↑"
			if cell.WRAcc < 0 {
				arrow = "↓"
			}
			fmt.Fprintf(&b, "%s %s (%.0f%% vs %.0f%%; lift %.1f)", pi.sets.Items[it].Name, arrow, 100*cell.In, 100*cell.Rest, cell.Lift)
			listed++
		}
		if listed == 0 {
			b.WriteString("no item stands out after the correction")
		}
		row.features = b.String()
		rows = append(rows, row)
	}
	return
}

// itemsSummary is the line above the attribute table.
func itemsSummary(pi projectionItems) string {
	if pi.err != nil {
		return "attributes: " + pi.err.Error()
	}
	if pi.contrast == nil {
		return "attributes: nothing to read"
	}
	s := fmt.Sprintf("attributes: %d items kept of %d candidates · one subgroup per cluster against the rest, noise included, at most 3 literals · Fisher exact two-sided, Bonferroni over %d tests at α %.2g",
		len(pi.sets.Items), pi.candidates, pi.contrast.Tests, explain.ItemContrastAlpha)
	if pi.contrast.Truncation.Truncated {
		s += " · cut short"
	}
	return s
}
