package lwlens

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/explain"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
)

// RuleTerm is one literal of a cluster's rule: the row has, or lacks, a slot.
type RuleTerm struct {
	Slot int32
	Has  bool
}

// Band is a group of rows drawn together: a cluster, or the rows no cluster
// took.
type Band struct {
	// Cluster is the HDBSCAN label, −1 for the unclustered rows.
	Cluster int32
	Rows    []int32
	// Support is the share of the band's rows carrying each slot.
	Support []float32
	// Rule is the one-vs-rest tree's best rule for the cluster; Precision
	// and Recall are how well it separates the band from the rest.
	Rule      []RuleTerm
	Precision float32
	Recall    float32
}

// SlotStats describes a slot's values across the batch.
type SlotStats struct {
	// Sorted holds the numeric values, ascending.
	Sorted []float64
	// Categories are the distinct texts, most frequent first; Counts is
	// aligned with it.
	Categories []string
	Counts     []int32
	// Width is the widest value text, in runes.
	Width int
	// Prefix is how many leading runes every value of the slot shares, cut
	// back to a separator: the part a glimpse of a value can leave out.
	Prefix int
	// Decimals is how many decimals tell the slot's numbers apart: enough
	// to resolve a fiftieth of their central spread.
	Decimals int
}

// Elide drops the slot's shared prefix from text, marking the cut.
func (inst *SlotStats) Elide(text string) string {
	if inst.Prefix == 0 {
		return text
	}
	rs := []rune(text)
	if len(rs) <= inst.Prefix {
		return text
	}
	return "…" + string(rs[inst.Prefix:])
}

// CommonPrefix is how many leading runes all of texts share, cut back to
// just past the last separator in it; 0 when that is under minPrefix or
// fewer than two texts differ.
func CommonPrefix(texts []string, minPrefix int) int {
	if len(texts) < 2 {
		return 0
	}
	p := []rune(texts[0])
	for _, t := range texts[1:] {
		rs := []rune(t)
		n := 0
		for n < len(p) && n < len(rs) && p[n] == rs[n] {
			n++
		}
		p = p[:n]
	}
	cut := 0
	for i, r := range p {
		if strings.ContainsRune("-_./: ", r) {
			cut = i + 1
		}
	}
	for _, t := range texts {
		if len([]rune(t)) <= cut {
			return 0
		}
	}
	if cut < minPrefix {
		return 0
	}
	return cut
}

// Percentile is v's mid-rank in the slot's values, in [0, 1].
func (inst *SlotStats) Percentile(v float64) float64 {
	n := len(inst.Sorted)
	if n <= 1 {
		return 0.5
	}
	lo, _ := slices.BinarySearch(inst.Sorted, v)
	hi := lo
	for hi < n && inst.Sorted[hi] == v {
		hi++
	}
	return (float64(lo) + float64(hi-lo-1)/2) / float64(n-1)
}

// CategoryRank is text's frequency rank, 0 the most frequent, −1 unseen.
func (inst *SlotStats) CategoryRank(text string) int {
	return slices.Index(inst.Categories, text)
}

// Analysis is what the lens knows about a batch beyond its cells.
type Analysis struct {
	Model *Model
	// Support is the share of rows carrying each slot.
	Support []float32
	Stats   []SlotStats
	// Bands are clusters by size, largest first, then the unclustered rows.
	Bands []Band
	// BandOf is each row's index into Bands.
	BandOf []int32
	// Order is every slot in canonical order: sections kept together,
	// sections and slots ordered by the band they are most typical of, then
	// by support. It is the order a frame shared across rows uses.
	Order []int32
	// Clustered is false when there were too few rows or no structural
	// variation to cluster; every row is then in one band.
	Clustered bool
}

// AnalyzeOptions bounds the analysis.
type AnalyzeOptions struct {
	// MinClusterSize is HDBSCAN's; 0 derives it from the row count.
	MinClusterSize int
	// RuleDepth bounds a cluster rule's terms; 0 takes 3.
	RuleDepth int
}

// minRowsToCluster is the row count below which clustering says nothing a
// reader could not see at a glance.
const minRowsToCluster = 6

// Analyze computes the analysis of m. It is a pure function of m and opts
// and cheap at the row counts the lens draws: the neighbour graph is exact
// and quadratic in the rows.
func Analyze(ctx context.Context, m *Model, opts AnalyzeOptions) (a Analysis, err error) {
	a.Model = m
	n, nSlots := len(m.Rows), len(m.Slots)
	a.Support = make([]float32, nSlots)
	for _, r := range m.Rows {
		for _, c := range r.Cells {
			a.Support[c.Slot]++
		}
	}
	for s := range a.Support {
		a.Support[s] /= float32(max(n, 1))
	}
	a.Stats = slotStats(m)

	// The structural features: presence of each tagged slot that varies.
	// Plain slots are backbone — every row has them — and a slot every row
	// has separates nothing.
	var feats []int32
	for s, sl := range m.Slots {
		if !sl.Plain && a.Support[s] < 1 {
			feats = append(feats, int32(s))
		}
	}
	labels := make([]int32, n)
	if n >= minRowsToCluster && len(feats) > 0 {
		labels, a.Clustered, err = cluster(ctx, m, feats, opts)
		if err != nil {
			return
		}
		if a.Clustered {
			refine(m, labels, feats)
		}
	}
	a.bands(labels, feats, opts)
	a.order()
	return
}

// presence is the row-major n×len(feats) matrix of slot presence.
func presence(m *Model, feats []int32) (x []float64) {
	d := len(feats)
	x = make([]float64, len(m.Rows)*d)
	col := make(map[int32]int, d)
	for j, s := range feats {
		col[s] = j
	}
	for i, r := range m.Rows {
		for _, c := range r.Cells {
			if j, ok := col[c.Slot]; ok {
				x[i*d+j] = 1
			}
		}
	}
	return
}

// cluster runs the projection panel's structural clustering: a cosine
// neighbour graph over slot presence and HDBSCAN over it (ADR-0230,
// ADR-0238). Rows with identical slots sit at distance zero, so a batch of
// a few record kinds clusters into its kinds.
func cluster(ctx context.Context, m *Model, feats []int32, opts AnalyzeOptions) (labels []int32, ok bool, err error) {
	n, d := len(m.Rows), len(feats)
	x64 := presence(m, feats)
	x := make([]float32, len(x64))
	for i, v := range x64 {
		x[i] = float32(v)
	}
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i)
	}
	mcs := opts.MinClusterSize
	if mcs <= 0 {
		mcs = max(3, min(8, n/12))
	}
	// The core distance is read at the K-th neighbour, so K must stay under
	// the smallest cluster: with K past it, every row of a small kind finds
	// its core distance in another kind, and kinds that are cleanly apart
	// merge at one level.
	res, err := knn.Build(ctx, nil, x, d, ids, knn.Options{K: min(mcs-1, n-1), Metric: knn.MetricCosine})
	if err != nil {
		return
	}
	g, err := res.DistanceGraph()
	if err != nil {
		return
	}
	hr, err := algo.HDBSCAN(ctx, g, res.CoreDist, algo.HDBSCANOptions{MinClusterSize: mcs})
	if err != nil {
		return
	}
	labels = make([]int32, n)
	for i := range labels {
		labels[i] = -1
	}
	for slot, row := range res.Rows {
		labels[row] = hr.Label[slot]
	}
	ok = hr.NumClusters > 1
	if !ok {
		// One cluster, or none, is no grouping: every row in one band.
		clear(labels)
	}
	return
}

// bands groups rows by label, orders the groups and reads a rule for each.
func (inst *Analysis) bands(labels []int32, feats []int32, opts AnalyzeOptions) {
	m := inst.Model
	byLabel := map[int32][]int32{}
	for i, lb := range labels {
		byLabel[lb] = append(byLabel[lb], int32(i))
	}
	for lb, rows := range byLabel {
		inst.Bands = append(inst.Bands, Band{Cluster: lb, Rows: rows})
	}
	slices.SortFunc(inst.Bands, func(a, b Band) int {
		if (a.Cluster < 0) != (b.Cluster < 0) {
			if a.Cluster < 0 {
				return 1
			}
			return -1
		}
		if c := cmp.Compare(len(b.Rows), len(a.Rows)); c != 0 {
			return c
		}
		return cmp.Compare(a.Rows[0], b.Rows[0])
	})
	inst.BandOf = make([]int32, len(m.Rows))
	for bi := range inst.Bands {
		b := &inst.Bands[bi]
		slices.SortStableFunc(b.Rows, func(x, y int32) int { return strings.Compare(m.Rows[x].Label, m.Rows[y].Label) })
		b.Support = make([]float32, len(m.Slots))
		for _, r := range b.Rows {
			inst.BandOf[r] = int32(bi)
			for _, c := range m.Rows[r].Cells {
				b.Support[c.Slot]++
			}
		}
		for s := range b.Support {
			b.Support[s] /= float32(len(b.Rows))
		}
	}
	if !inst.Clustered {
		return
	}
	depth := opts.RuleDepth
	if depth <= 0 {
		depth = 3
	}
	x := presence(m, feats)
	for bi := range inst.Bands {
		b := &inst.Bands[bi]
		if b.Cluster < 0 {
			continue
		}
		t, err := explain.FitOneVsRest(context.Background(), x, len(feats), labels, b.Cluster,
			explain.TreeOptions{MaxDepth: depth, MinLeaf: 1})
		if err != nil {
			continue
		}
		var best explain.Rule
		for _, r := range t.RulesFor(depth, 1) {
			if r.Hits > best.Hits || (r.Hits == best.Hits && r.Precision > best.Precision) {
				best = r
			}
		}
		for _, term := range best.Terms {
			b.Rule = append(b.Rule, RuleTerm{Slot: feats[term.Feature], Has: term.Above})
		}
		b.Precision, b.Recall = best.Precision, best.Recall
	}
}

// order computes the canonical slot order.
func (inst *Analysis) order() {
	m := inst.Model
	dominant := make([]int, len(m.Slots))
	for s := range m.Slots {
		best := float32(-1)
		for bi, b := range inst.Bands {
			if b.Support[s] > best+1e-6 {
				best, dominant[s] = b.Support[s], bi
			}
		}
	}
	type secKey struct {
		plain    bool
		dominant int
		support  float32
		first    int
	}
	sec := make(map[string]*secKey, len(m.Sections))
	for i, name := range m.Sections {
		sec[name] = &secKey{dominant: math.MaxInt, first: i}
	}
	for s, sl := range m.Slots {
		k := sec[sl.Section]
		k.plain = sl.Plain
		k.dominant = min(k.dominant, dominant[s])
		k.support = max(k.support, inst.Support[s])
	}
	inst.Order = make([]int32, len(m.Slots))
	for s := range inst.Order {
		inst.Order[s] = int32(s)
	}
	slices.SortStableFunc(inst.Order, func(a, b int32) int {
		sa, sb := m.Slots[a], m.Slots[b]
		ka, kb := sec[sa.Section], sec[sb.Section]
		if ka != kb {
			if ka.plain != kb.plain {
				if ka.plain {
					return -1
				}
				return 1
			}
			if c := cmp.Compare(ka.dominant, kb.dominant); c != 0 {
				return c
			}
			if c := cmp.Compare(kb.support, ka.support); c != 0 {
				return c
			}
			return cmp.Compare(ka.first, kb.first)
		}
		if c := cmp.Compare(dominant[a], dominant[b]); c != 0 {
			return c
		}
		if c := cmp.Compare(inst.Support[b], inst.Support[a]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
}

// slotStats reads each slot's values and decides its kind.
func slotStats(m *Model) (stats []SlotStats) {
	stats = make([]SlotStats, len(m.Slots))
	counts := make([]map[string]int32, len(m.Slots))
	nNum := make([]int, len(m.Slots))
	nAll := make([]int, len(m.Slots))
	for _, r := range m.Rows {
		for _, c := range r.Cells {
			st := &stats[c.Slot]
			nAll[c.Slot]++
			if c.HasNum {
				st.Sorted = append(st.Sorted, c.Num)
				nNum[c.Slot]++
			}
			if counts[c.Slot] == nil {
				counts[c.Slot] = map[string]int32{}
			}
			counts[c.Slot][c.Text]++
			st.Width = max(st.Width, len([]rune(c.Text)))
		}
	}
	for s := range m.Slots {
		st := &stats[s]
		slices.Sort(st.Sorted)
		for t, k := range counts[s] {
			st.Categories = append(st.Categories, t)
			st.Counts = append(st.Counts, k)
		}
		idx := make([]int, len(st.Categories))
		for i := range idx {
			idx[i] = i
		}
		slices.SortFunc(idx, func(a, b int) int {
			if c := cmp.Compare(st.Counts[b], st.Counts[a]); c != 0 {
				return c
			}
			return strings.Compare(st.Categories[a], st.Categories[b])
		})
		cats, cnts := make([]string, len(idx)), make([]int32, len(idx))
		for i, j := range idx {
			cats[i], cnts[i] = st.Categories[j], st.Counts[j]
		}
		st.Categories, st.Counts = cats, cnts
		distinct := len(cats)
		st.Prefix = CommonPrefix(cats, 4)
		st.Decimals = decimals(st.Sorted)
		sl := &m.Slots[s]
		numeric := sl.NumericType && nAll[s] > 0 && nNum[s]*5 >= nAll[s]*4
		switch {
		case numeric && distinct > 3:
			sl.Kind = ValueKindNumeric
		case distinct <= 6 || (nAll[s] >= 4 && distinct*3 <= nAll[s]):
			sl.Kind = ValueKindCategorical
		case numeric:
			sl.Kind = ValueKindNumeric
		default:
			sl.Kind = ValueKindText
		}
	}
	return
}

// minTemplateJaccard is the overlap with its cluster's template under which
// a row is taken out of the cluster.
const minTemplateJaccard = 0.5

// refine takes out of their clusters the rows that share less than half of
// the cluster's template — the slots most of its rows have. HDBSCAN keeps a
// point that falls out of a selected cluster as a member of low
// probability, which over slot presence puts a record of another kind in a
// cluster it shares one section with; drawn there, it reads as a row of that
// kind with a dozen deviations, which it is not.
func refine(m *Model, labels []int32, feats []int32) {
	isFeat := make(map[int32]bool, len(feats))
	for _, s := range feats {
		isFeat[s] = true
	}
	count := map[int32]map[int32]int{}
	size := map[int32]int{}
	for i, lb := range labels {
		if lb < 0 {
			continue
		}
		if count[lb] == nil {
			count[lb] = map[int32]int{}
		}
		size[lb]++
		for _, c := range m.Rows[i].Cells {
			if isFeat[c.Slot] {
				count[lb][c.Slot]++
			}
		}
	}
	for i, lb := range labels {
		if lb < 0 {
			continue
		}
		template := 0
		for _, k := range count[lb] {
			if 2*k >= size[lb] {
				template++
			}
		}
		inter, own := 0, 0
		for _, c := range m.Rows[i].Cells {
			if !isFeat[c.Slot] {
				continue
			}
			own++
			if 2*count[lb][c.Slot] >= size[lb] {
				inter++
			}
		}
		union := own + template - inter
		if union > 0 && float64(inter) < minTemplateJaccard*float64(union) {
			labels[i] = -1
		}
	}
}

// decimals is the number of decimals that resolves a fiftieth of the spread
// between the 10th and 90th percentile of sorted, capped at 4; 0 for whole
// numbers.
func decimals(sorted []float64) int {
	whole := true
	for _, v := range sorted {
		if v != math.Trunc(v) {
			whole = false
			break
		}
	}
	if whole || len(sorted) < 2 {
		return 0
	}
	lo, hi := sorted[len(sorted)/10], sorted[len(sorted)-1-len(sorted)/10]
	spread := hi - lo
	if spread <= 0 {
		spread = sorted[len(sorted)-1] - sorted[0]
	}
	if spread <= 0 {
		return 1
	}
	return max(0, min(4, int(math.Ceil(-math.Log10(spread/50)))))
}
