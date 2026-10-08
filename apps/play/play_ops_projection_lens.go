package play

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
)

// get_archetypes reads the Projection run's clusters as the lens's archetype
// form (ADR-0289 §SD4): per cluster what it typically holds, its
// extremes and the rows that break it — what the panel's archetypes view
// draws, as data.

const opGetArchetypes = "get_archetypes"

// Bounds on get_archetypes' reading.
const (
	opsArchetypesExceptions = 6
	opsArchetypesRows       = 8
	opsArchetypesMembers    = 20
	opsArchetypesTypical    = 24
)

// ArchetypesArgs is get_archetypes' argument.
type ArchetypesArgs struct {
	Cluster int32 `json:",omitzero" desc:"one cluster, from 1; every cluster and the unclustered rows when left out"`
}

// ArchetypeValue is what a cluster typically holds in one attribute.
type ArchetypeValue struct {
	Attribute string  `desc:"the attribute, as section·name"`
	Typical   string  `desc:"a number's median with its 10th to 90th percentile in brackets; a label with the share of the cluster's values it is; or how many distinct texts there are, from the least to the greatest"`
	Carried   float64 `desc:"the share of the cluster's rows carrying the attribute"`
}

// ArchetypeExtreme names the rows holding a cluster's lowest and highest
// value of one numeric attribute.
type ArchetypeExtreme struct {
	Attribute string `desc:"the attribute"`
	Lowest    string `desc:"the row holding the cluster's lowest value, then the value"`
	Highest   string `desc:"the row holding the cluster's highest value, then the value"`
}

// ArchetypeException is rows breaking their cluster's pattern in the same
// ways.
type ArchetypeException struct {
	Rows       []string `desc:"the rows' labels"`
	RowNumbers []int64  `desc:"the rows of the result, from 0, aligned with rows"`
	More       int32    `json:",omitzero" desc:"further rows departing the same ways, not listed"`
	Departures []string `desc:"how: −attribute lacks what its cluster has; +attribute has what its cluster lacks; attribute value (k of n rows) is a rare label; attribute value ↑ or ↓ is an extreme number"`
}

// ClusterArchetype is one cluster as the archetype form reads it.
type ClusterArchetype struct {
	Cluster   int32    `desc:"the cluster, from 1 as get_projection and explain_clusters number it; -1 for the rows no cluster took"`
	Rows      int32    `desc:"its rows"`
	Rule      string   `json:",omitzero" desc:"which attributes its rows have and lack, as the lens reads them, with the share of the rows matching it that are the cluster's"`
	Constants []string `json:",omitzero" desc:"attributes every row of the cluster holds with one value, as attribute=value"`
	Typical   []ArchetypeValue
	Extremes  []ArchetypeExtreme `json:",omitzero"`
	// Exceptions are most telling first.
	Exceptions     []ArchetypeException `json:",omitzero" desc:"the rows that break the pattern, most telling first: missing and unexpected attributes, then rare labels, then extreme numbers"`
	MoreExceptions int32                `json:",omitzero" desc:"exception lines left out by the bound"`
	Members        []string             `json:",omitzero" desc:"for the unclustered rows: their labels"`
}

// ArchetypesReading is get_archetypes' result.
type ArchetypesReading struct {
	Summary   string             `desc:"the run the reading is of"`
	Clusters  []ClusterArchetype `desc:"the clusters, largest first, then the unclustered rows"`
	Truncated bool               `json:",omitzero" desc:"true when the byte bound left exception lines out"`
	Note      string             `desc:"what the reading leaves out"`
}

const archetypesOpsNote = "values are the driver's text; a row sharing under half of its cluster's attributes is read with the unclustered rows, though the graph keeps it in its cluster"

// archetypesReading is get_archetypes' reading of a snapshot.
func archetypesReading(ps projectionOpsSnap, in ArchetypesArgs) (out ArchetypesReading, err error) {
	res := ps.snap.result
	switch {
	case res == nil:
		return out, app.RefuseOperation("no projection to read: compute_projection runs one, get_projection says when it is done")
	case res.lensErr != nil:
		return out, app.RefuseOperation("the lens could not read this run: " + res.lensErr.Error())
	case res.lens == nil:
		return out, app.RefuseOperation("the run has no lens reading")
	}
	if in.Cluster < 0 || int(in.Cluster) > res.clusters.NumClusters {
		return out, app.RefuseOperation("cluster runs from 1 to " + strconv.Itoa(res.clusters.NumClusters))
	}
	a := res.lens
	p := lwlens.PlanRows(a, lwlens.Intent{Values: projectionLensDefaultValues, Stable: projectionLensDefaultStable})
	out.Note = archetypesOpsNote
	out.Summary = fmt.Sprintf("%d rows projected · %d cluster(s) · features: %s · %d attributes",
		len(res.rows), res.clusters.NumClusters, res.params.FeatureSet, len(a.Model.Slots))
	budget, used := opsSampleMaxBytes, len(out.Summary)+len(out.Note)
	for _, ar := range lwlens.Archetypes(a, &p) {
		b := &a.Bands[ar.Band]
		if in.Cluster != 0 && b.Cluster+1 != in.Cluster {
			continue
		}
		ca := clusterArchetype(a, res.rows, ar)
		used += clusterArchetypeCost(&ca)
		exc := ca.Exceptions
		ca.Exceptions = nil
		for i, e := range exc {
			cost := archetypeExceptionCost(&e)
			if i >= opsArchetypesExceptions || used+cost > budget {
				ca.MoreExceptions = int32(len(exc) - i)
				out.Truncated = out.Truncated || used+cost > budget
				break
			}
			used += cost
			ca.Exceptions = append(ca.Exceptions, e)
		}
		out.Clusters = append(out.Clusters, ca)
	}
	return
}

// clusterArchetype turns one band's archetype into the reading's form.
func clusterArchetype(a *lwlens.Analysis, rows []int64, ar lwlens.Archetype) (ca ClusterArchetype) {
	m := a.Model
	b := &a.Bands[ar.Band]
	ca.Cluster, ca.Rows = -1, int32(len(b.Rows))
	if b.Cluster < 0 {
		for _, r := range b.Rows {
			if len(ca.Members) == opsArchetypesMembers {
				break
			}
			ca.Members = append(ca.Members, m.Rows[r].Label)
		}
		return
	}
	ca.Cluster = b.Cluster + 1
	if rule := a.RuleText(b); rule != "" {
		ca.Rule = fmt.Sprintf("%s (%.0f%% of the rows matching it)", rule, 100*b.Precision)
	}
	for _, s := range ar.Constants {
		if cell, ok := m.Rows[b.Rows[0]].Cell(s); ok {
			ca.Constants = append(ca.Constants, m.Slots[s].Label()+"="+cell.Text)
		}
	}
	for _, t := range ar.Template {
		if len(ca.Typical) == opsArchetypesTypical {
			break
		}
		ca.Typical = append(ca.Typical, ArchetypeValue{Attribute: m.Slots[t.Slot].Label(), Typical: typicalText(t), Carried: float64(t.Support)})
	}
	for _, e := range ar.Extremes {
		ca.Extremes = append(ca.Extremes, ArchetypeExtreme{Attribute: m.Slots[e.Slot].Label(),
			Lowest:  m.Rows[e.LowRow].Label + " " + formatNum(e.LowValue),
			Highest: m.Rows[e.HighRow].Label + " " + formatNum(e.HighValue)})
	}
	for _, e := range ar.Exceptions {
		var x ArchetypeException
		for i, r := range e.Rows {
			if i == opsArchetypesRows {
				x.More = int32(len(e.Rows) - i)
				break
			}
			x.Rows = append(x.Rows, m.Rows[r].Label)
			if int(r) < len(rows) {
				x.RowNumbers = append(x.RowNumbers, rows[r])
			}
		}
		for _, d := range e.Departures {
			x.Departures = append(x.Departures, departureText(m, b, d))
		}
		ca.Exceptions = append(ca.Exceptions, x)
	}
	return
}

// typicalText is a template entry as text.
func typicalText(t lwlens.Typical) string {
	if t.Numeric {
		if t.Count > 2 {
			return formatNum(t.Median) + " (" + formatNum(t.Low) + "–" + formatNum(t.High) + ")"
		}
		return formatNum(t.Median)
	}
	if t.Share < 0.5 && t.Count > 1 {
		return fmt.Sprintf("%d distinct, from %s to %s", t.Distinct, t.First, t.Last)
	}
	return fmt.Sprintf("%s (%.0f%%)", t.Mode, 100*t.Share)
}

// departureText is one departure as text.
func departureText(m *lwlens.Model, b *lwlens.Band, d lwlens.Departure) string {
	name := m.Slots[d.Slot].Label()
	switch d.Kind {
	case lwlens.DepartureKindMissing:
		return "−" + name
	case lwlens.DepartureKindUnexpected:
		return "+" + name
	case lwlens.DepartureKindRareLabel:
		return fmt.Sprintf("%s %s (%d of %d rows)", name, d.Text, d.Count, len(b.Rows))
	}
	dir := "↓"
	if d.High {
		dir = "↑"
	}
	return name + " " + formatNum(d.Value) + dir
}

func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// clusterArchetypeCost estimates a cluster's bytes but for its exceptions.
func clusterArchetypeCost(ca *ClusterArchetype) (cost int) {
	cost = 64 + len(ca.Rule)
	for _, s := range ca.Constants {
		cost += len(s) + 4
	}
	for _, t := range ca.Typical {
		cost += len(t.Attribute) + len(t.Typical) + 48
	}
	for _, e := range ca.Extremes {
		cost += len(e.Attribute) + len(e.Lowest) + len(e.Highest) + 48
	}
	for _, s := range ca.Members {
		cost += len(s) + 4
	}
	return
}

// archetypeExceptionCost estimates an exception's bytes.
func archetypeExceptionCost(e *ArchetypeException) (cost int) {
	cost = 48 + len(strings.Join(e.Rows, ",")) + 12*len(e.RowNumbers)
	for _, d := range e.Departures {
		cost += len(d) + 4
	}
	return
}

func addArchetypesOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Query(s, app.OperationSpec{Name: opGetArchetypes, Version: 1,
		Summary: "read the Projection run's clusters as archetypes: per cluster what its rows typically hold, the rows with its lowest and highest values, and the rows that break its pattern — a missing or extra attribute, a rare label, an extreme number",
		Reads:   []string{opsResProjection}, Agents: true, Untrusted: true},
		func(sn opsSnap, in ArchetypesArgs) (ArchetypesReading, error) {
			if !sn.mounted {
				return ArchetypesReading{}, app.RefuseOperation("the window has not mounted")
			}
			return archetypesReading(sn.projection, in)
		})
}
