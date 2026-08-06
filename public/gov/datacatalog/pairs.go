package datacatalog

import (
	"slices"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// LeewayTable is one discovered table that classified as leeway, carrying both
// the reconstructed schema and what the analysis derives from it. It is the
// input to [RelatePairs] and the row shape of boxer.tables_leeway.
type LeewayTable struct {
	Ref        TableRef
	Table      *common.TableDesc
	RowConfig  common.TableRowConfigE
	AttrKeys   []string
	SchemaHash uint64
}

// NewLeewayTable derives the attribute keys and schema hash for a classified
// table. It fails only when the table does not normalize, which for a table
// that came out of [Classify] means the naming grammar produced something the
// normalizer rejects — a defect worth surfacing rather than a table worth
// skipping quietly.
func NewLeewayTable(ops *common.TableOperations, ref TableRef, cl Classification) (lt LeewayTable, err error) {
	if cl.Kind != KindLeeway || cl.Table == nil {
		err = eb.Build().Str("table", ref.String()).Errorf("not a leeway classification")
		return
	}
	var keys []string
	keys, err = AttrKeys(ops, cl.Table)
	if err != nil {
		err = eb.Build().Str("table", ref.String()).Errorf("unable to derive attribute keys: %w", err)
		return
	}
	lt = LeewayTable{
		Ref:        ref,
		Table:      cl.Table,
		RowConfig:  cl.RowConfig,
		AttrKeys:   keys,
		SchemaHash: HashAttrKeys(keys),
	}
	return
}

// Pair is one unordered pair of leeway tables and how they relate — a row of
// boxer.tables_leeway_compatibility. A and B are ordered so that A precedes B
// under [TableRef.Compare], and Relation reads "A is a … of B".
type Pair struct {
	A        TableRef
	B        TableRef
	Relation common.TableRelationE
	// ShapeId names the pair's shared schema: [HashAttrKeys] over the
	// intersection of the two key sets, or 0 when the intersection is empty.
	// Zero therefore covers every disjoint pair, and also an overlap pair whose
	// tables share a column name but not its type — they name something in
	// common, but nothing they could both be read through.
	//
	// For an equal or subset pair the intersection is the contained side's
	// whole key set, so ShapeId equals that side's [LeewayTable.SchemaHash].
	ShapeId uint64
	NCommon uint32
	// Jaccard is |A ∩ B| / |A ∪ B| over the attribute keys, 0 when both sides
	// are empty. It is what a book chapter thresholds on to keep a diagram
	// legible, not part of the relation.
	Jaccard float32
}

// RelatePairs classifies every unordered pair of tables. It is brute force by
// decision (ADR-0170 §SD3): ~5,000 calls at a hundred tables, each
// re-normalizing both sides. The loop is single-threaded because
// [common.TableOperations] borrows one manipulator and one normalizer across
// calls and is not safe for concurrent use.
//
// All pairs are returned, disjoint included — a consumer that only wants the
// hierarchy filters, and "these two share nothing" is an answer a reader may be
// looking for.
//
// The input is sorted in place by [TableRef.Compare] so the output ordering is
// the catalog table's ORDER BY.
func RelatePairs(ops *common.TableOperations, tables []LeewayTable) (pairs []Pair, err error) {
	slices.SortFunc(tables, func(a LeewayTable, b LeewayTable) int {
		return a.Ref.Compare(b.Ref)
	})
	n := len(tables)
	pairs = make([]Pair, 0, n*(n-1)/2)
	for i := range n {
		for j := i + 1; j < n; j++ {
			a, b := &tables[i], &tables[j]
			var rel common.TableRelationE
			rel, err = ops.Relate(a.Table, b.Table)
			if err != nil {
				err = eb.Build().Str("tableA", a.Ref.String()).Str("tableB", b.Ref.String()).
					Errorf("unable to relate tables: %w", err)
				pairs = nil
				return
			}
			shared := IntersectKeys(a.AttrKeys, b.AttrKeys)
			shapeId := uint64(0)
			if len(shared) > 0 {
				shapeId = HashAttrKeys(shared)
			}
			pairs = append(pairs, Pair{
				A:        a.Ref,
				B:        b.Ref,
				Relation: rel,
				ShapeId:  shapeId,
				NCommon:  uint32(len(shared)),
				Jaccard:  jaccard(len(shared), len(a.AttrKeys), len(b.AttrKeys)),
			})
		}
	}
	return
}

// IntersectKeys returns the sorted keys present in both lists. Both inputs must
// already be sorted, which [AttrKeys] guarantees; the result is a fresh slice.
func IntersectKeys(a []string, b []string) (out []string) {
	out = make([]string, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return
}

// jaccard is |A ∩ B| / |A ∪ B| with the union computed by inclusion-exclusion.
// Two empty sets score 0 rather than the mathematician's 1: the catalog reads
// the number as "how much of these two tables is the same", and for a table
// with nothing in it that question has no useful answer.
func jaccard(nCommon int, nA int, nB int) (j float32) {
	union := nA + nB - nCommon
	if union <= 0 {
		return 0
	}
	return float32(nCommon) / float32(union)
}

// SortTables orders a slice of snapshots by [TableRef.Compare], the ordering
// every catalog table declares. Exported so a run can put its inventory in the
// same order without restating the comparator.
func SortTables(tables []TableSnapshot) {
	sort.SliceStable(tables, func(i int, j int) bool {
		return tables[i].Ref.Compare(tables[j].Ref) < 0
	})
}
