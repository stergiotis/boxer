package play

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

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
// the best small conjunction — and a rule spells as SQL over column
// handles, which play resolves before the statement ships.

const (
	// projectionItemsMinSupportShare and projectionItemsMinSupportFloor
	// drop items almost no row has, but never one held by as many rows as
	// the smallest cluster: an item that can cover a cluster must be
	// allowed to describe it. projectionItemsMaxSupportShare drops items
	// almost every row has. Neither end says anything about a cluster.
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
	// sql is each item's predicate over its column handle, and hasSQL
	// whether it has one.
	sql    []string
	hasSQL []bool
	err    error
	// candidates is the vocabulary size before pruning.
	candidates int
}

// explainItems reads the labels against the item sets in slot order.
// minCluster is the clustering's minimum cluster size, which caps the
// support floor.
func explainItems(ctx context.Context, sets card.ItemSets, candidates int, cl algo.HDBSCANResult, minCluster int) (pi projectionItems) {
	pi.candidates = candidates
	if cl.NumClusters == 0 || len(sets.Rows) == 0 {
		return
	}
	n := len(sets.Rows)
	minSupport := int32(max(projectionItemsMinSupportFloor, min(minCluster, int(projectionItemsMinSupportShare*float64(n)))))
	maxSupport := int32(projectionItemsMaxSupportShare * float64(n))
	pi.sets = sets.Prune(minSupport, maxSupport, projectionItemsMaxItems)
	pi.sql = make([]string, len(pi.sets.Items))
	pi.hasSQL = make([]bool, len(pi.sets.Items))
	for i, it := range pi.sets.Items {
		pi.sql[i], pi.hasSQL[i] = itemPredicate(it)
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

// itemPredicate spells an item as a predicate the way the authoring surface
// writes one: over the item's column handle, `section:column` (ADR-0116),
// which play's handle pass resolves to the physical column before the
// statement ships, so no physical name appears; a component as its
// LW_COMPONENT_FILTER, which the component pass expands (ADR-0189). A
// co-group has no column of its own and no spelling.
func itemPredicate(it card.Item) (sql string, ok bool) {
	lit := func() string {
		if !it.Quoted {
			return it.Value
		}
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(it.Value) + "'"
	}
	switch it.Kind {
	case card.ItemKindSection:
		if it.Handle == "" {
			return
		}
		return "length(`" + it.Handle + "`) > 0", true
	case card.ItemKindTaggedValue:
		return "has(`" + it.Handle + "`, " + lit() + ")", true
	case card.ItemKindTagRef:
		return "has(`" + it.Handle + "`, " + strconv.FormatUint(it.Ref, 10) + ")", true
	case card.ItemKindTagVerbatim:
		return "has(`" + it.Handle + "`, " + lit() + ")", true
	case card.ItemKindComponent:
		return "LW_COMPONENT_FILTER('" + it.Value + "')", true
	}
	return
}

// withComponentItems appends one item per registered component kind to the
// sets — `component:<Kind>`, held by the entities that carry the kind — so
// the attribute reading and the rules see components beside sections and
// tags. kinds and rows come from componentDetail.presenceRows over the
// same entities, in the same order.
func withComponentItems(sets card.ItemSets, kinds []string, rows [][]int32) card.ItemSets {
	if len(kinds) == 0 || len(rows) != len(sets.Rows) {
		return sets
	}
	base := int32(len(sets.Items))
	out := card.ItemSets{
		Items:   append(slices.Clone(sets.Items), make([]card.Item, len(kinds))...),
		Support: append(slices.Clone(sets.Support), make([]int32, len(kinds))...),
		Rows:    make([][]int32, len(sets.Rows)),
	}
	for k, kind := range kinds {
		out.Items[int(base)+k] = card.Item{Name: "component:" + kind, Kind: card.ItemKindComponent, Value: kind}
	}
	for r, row := range sets.Rows {
		ids := slices.Clone(row)
		for _, k := range rows[r] {
			ids = append(ids, base+k)
			out.Support[int(base)+int(k)]++
		}
		slices.Sort(ids)
		out.Rows[r] = ids
	}
	return out
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
