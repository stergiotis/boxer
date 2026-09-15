package explain

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// items.go reads a labelling against binary items — facts a row has or
// lacks — rather than against continuous features (ADR-0235 §SD6). The
// two readings are the supervised descriptive rule discovery family
// (Novak, Lavrač & Webb 2009): per item, how over- or under-represented
// it is in a label's rows, with a Fisher exact test corrected for the
// number of tests; and per label, the conjunction of a few literals with
// the best weighted relative accuracy, found by beam search.

// ItemCell is one label's reading of one item.
type ItemCell struct {
	// In is the share of the label's rows holding the item; Rest the share
	// of every other row holding it.
	In, Rest float32
	// Lift is the item's precision for the label over the label's base
	// rate: 1 is no association.
	Lift float32
	// WRAcc is the weighted relative accuracy of "item → label": the
	// item's coverage times the precision gain over the base rate. Its
	// sign is the direction; 0 is no association.
	WRAcc float32
	// P is the two-sided Fisher exact p-value of the 2×2 table; Significant
	// is P under the Bonferroni-corrected level.
	P           float64
	Significant bool
}

// ItemContrast holds an ItemCell per label and item.
type ItemContrast struct {
	NumLabels int
	NumItems  int
	// Cells is indexed [label*NumItems + item].
	Cells []ItemCell
	// Rows is the row count per label.
	Rows []int32
	// Tests is the number of hypotheses the level was corrected for, and
	// Level the per-test level after the correction.
	Tests      int
	Level      float64
	Truncation algo.Truncation
}

// ItemContrastAlpha is the family-wise level the Bonferroni correction
// holds.
const ItemContrastAlpha = 0.01

// Cell returns the reading of item under label.
func (inst *ItemContrast) Cell(label, item int32) ItemCell {
	return inst.Cells[int(label)*inst.NumItems+int(item)]
}

// Ranked returns the items under label by |WRAcc|, the largest first, an
// over-representation before an under-representation of the same size,
// ties by item.
func (inst *ItemContrast) Ranked(label int32) (items []int32) {
	items = make([]int32, inst.NumItems)
	for i := range items {
		items[i] = int32(i)
	}
	cells := inst.Cells[int(label)*inst.NumItems : (int(label)+1)*inst.NumItems]
	slices.SortStableFunc(items, func(a, b int32) int {
		if c := cmp.Compare(math.Abs(float64(cells[b].WRAcc)), math.Abs(float64(cells[a].WRAcc))); c != 0 {
			return c
		}
		return cmp.Compare(cells[b].WRAcc, cells[a].WRAcc)
	})
	return
}

// ItemContrasts reads every label against the rest of the rows over item
// sets: rows[r] lists the items row r holds, ascending, in [0, numItems).
// A negative label belongs to no label and to every label's rest.
func ItemContrasts(ctx context.Context, rows [][]int32, numItems int, labels []int32) (c *ItemContrast, err error) {
	n := len(labels)
	if len(rows) != n {
		err = eb.Build().Int("rows", len(rows)).Int("labels", n).Errorf("explain: rows and labels differ in length")
		return
	}
	k := 0
	for _, lb := range labels {
		k = max(k, int(lb)+1)
	}
	if k == 0 {
		err = eb.Build().Int("rows", n).Errorf("explain: no row carries a label")
		return
	}
	c = &ItemContrast{
		NumLabels: k,
		NumItems:  numItems,
		Cells:     make([]ItemCell, k*numItems),
		Rows:      make([]int32, k),
		Tests:     k * numItems,
	}
	c.Level = ItemContrastAlpha / float64(max(1, c.Tests))
	for _, lb := range labels {
		if lb >= 0 {
			c.Rows[lb]++
		}
	}
	// a[label][item]: the label's rows holding the item; total[item]: all
	// rows holding it.
	a := make([]int32, k*numItems)
	total := make([]int32, numItems)
	for r, row := range rows {
		lb := labels[r]
		for _, it := range row {
			if int(it) >= numItems || it < 0 {
				err = eb.Build().Int("row", r).Int32("item", it).Int("items", numItems).Errorf("explain: item out of range")
				return
			}
			total[it]++
			if lb >= 0 {
				a[int(lb)*numItems+int(it)]++
			}
		}
	}
	lf := newLogFactorials(n)
	for lb := range k {
		if ctx.Err() != nil {
			c.Truncation = algo.Truncation{Truncated: true, By: algo.LimitContext}
			return
		}
		m := c.Rows[lb]
		rest := int32(n) - m
		base := float64(m) / float64(n)
		for it := range numItems {
			cell := &c.Cells[lb*numItems+it]
			hit := a[lb*numItems+it]
			tot := total[it]
			if m > 0 {
				cell.In = float32(hit) / float32(m)
			}
			if rest > 0 {
				cell.Rest = float32(tot-hit) / float32(rest)
			}
			if tot > 0 && base > 0 {
				cell.Lift = float32(float64(hit) / float64(tot) / base)
				cell.WRAcc = float32(float64(tot) / float64(n) * (float64(hit)/float64(tot) - base))
			}
			cell.P = fisherTwoSided(lf, int(hit), int(m), int(tot), n)
			cell.Significant = cell.P < c.Level
		}
	}
	return
}

// logFactorials caches ln(i!) for i in [0, n].
type logFactorials []float64

func newLogFactorials(n int) logFactorials {
	lf := make(logFactorials, n+1)
	for i := 1; i <= n; i++ {
		lf[i] = lf[i-1] + math.Log(float64(i))
	}
	return lf
}

// logHyper is ln P(X = x) for X hypergeometric: x successes in a draw of
// m from n with tot successes.
func (lf logFactorials) logHyper(x, m, tot, n int) float64 {
	if x < 0 || x > m || x > tot || m-x > n-tot {
		return math.Inf(-1)
	}
	return lf[tot] - lf[x] - lf[tot-x] + lf[n-tot] - lf[m-x] - lf[n-tot-m+x] - lf[n] + lf[m] + lf[n-m]
}

// fisherTwoSided is the two-sided Fisher exact p-value: the total
// probability of every table at most as likely as the observed one.
func fisherTwoSided(lf logFactorials, x, m, tot, n int) float64 {
	if m == 0 || tot == 0 || m == n || tot == n {
		return 1
	}
	obs := lf.logHyper(x, m, tot, n)
	lo := max(0, m+tot-n)
	hi := min(m, tot)
	p := 0.0
	for i := lo; i <= hi; i++ {
		if l := lf.logHyper(i, m, tot, n); l <= obs+1e-9 {
			p += math.Exp(l)
		}
	}
	return min(1, p)
}

// Subgroups ------------------------------------------------------------------

// Literal is one condition of a subgroup: the row holds Item, or lacks it
// when Present is false.
type Literal struct {
	Item    int32
	Present bool
}

// Subgroup is a conjunction of literals read against one label: the rows
// it covers, the hits among them, and its quality.
type Subgroup struct {
	Literals []Literal
	Rows     int32
	Hits     int32
	// Precision is Hits over Rows; Recall is Hits over the label's rows;
	// WRAcc the weighted relative accuracy the search maximised.
	Precision float32
	Recall    float32
	WRAcc     float32
}

// SubgroupOptions are the search's knobs.
type SubgroupOptions struct {
	// MaxTerms bounds the conjunction. 0 means the default.
	MaxTerms int
	// Beam is the width of the beam. 0 means the default.
	Beam int
	// MinRows is the least number of rows a subgroup may cover. 0 means
	// the default.
	MinRows int
	// Negations allows "lacks item" literals.
	// MinGain is the least relative rise in WRAcc an added literal must
	// bring, so a rule does not grow by literals that barely matter. 0
	// means the default.
	MinGain   float64
	Negations bool
}

const (
	defaultSubgroupTerms = 3
	defaultSubgroupBeam  = 5
	defaultSubgroupRows  = 5
	defaultSubgroupGain  = 0.05
)

func (inst SubgroupOptions) withDefaults() SubgroupOptions {
	if inst.MaxTerms <= 0 {
		inst.MaxTerms = defaultSubgroupTerms
	}
	if inst.Beam <= 0 {
		inst.Beam = defaultSubgroupBeam
	}
	if inst.MinRows <= 0 {
		inst.MinRows = defaultSubgroupRows
	}
	if inst.MinGain <= 0 {
		inst.MinGain = defaultSubgroupGain
	}
	return inst
}

// FindSubgroup finds, for the rows carrying target against every other
// row, the conjunction of at most MaxTerms literals with the largest
// weighted relative accuracy: a beam search that extends each of the beam's
// conjunctions by every literal, keeps the best Beam, and stops when no
// extension improves the best by MinGain. Ties go to fewer negations, then to the earlier
// literal sequence, so the result is a function of the input. An empty result (no literals) means
// no literal beat the base rate.
func FindSubgroup(ctx context.Context, rows [][]int32, numItems int, labels []int32, target int32, opts SubgroupOptions) (best Subgroup, err error) {
	opts = opts.withDefaults()
	n := len(labels)
	if len(rows) != n {
		err = eb.Build().Int("rows", len(rows)).Int("labels", n).Errorf("explain: rows and labels differ in length")
		return
	}
	targetBM := roaring.New()
	itemBM := make([]*roaring.Bitmap, numItems)
	for i := range itemBM {
		itemBM[i] = roaring.New()
	}
	for r, row := range rows {
		if labels[r] == target {
			targetBM.Add(uint32(r))
		}
		for _, it := range row {
			if it < 0 || int(it) >= numItems {
				err = eb.Build().Int("row", r).Int32("item", it).Int("items", numItems).Errorf("explain: item out of range")
				return
			}
			itemBM[it].Add(uint32(r))
		}
	}
	m := targetBM.GetCardinality()
	if m == 0 {
		err = eb.Build().Int32("label", target).Errorf("explain: no row carries the label")
		return
	}
	base := float64(m) / float64(n)
	literals := make([]Literal, 0, 2*numItems)
	litBM := make([]*roaring.Bitmap, 0, 2*numItems)
	for i := range numItems {
		literals = append(literals, Literal{Item: int32(i), Present: true})
		litBM = append(litBM, itemBM[i])
		if opts.Negations {
			literals = append(literals, Literal{Item: int32(i), Present: false})
			litBM = append(litBM, roaring.FlipInt(itemBM[i], 0, n))
		}
	}
	type cand struct {
		lits []int32 // literal indices, ascending
		rows *roaring.Bitmap
		sg   Subgroup
	}
	score := func(bm *roaring.Bitmap, lits []int32) (sg Subgroup, ok bool) {
		cover := bm.GetCardinality()
		if int(cover) < opts.MinRows {
			return
		}
		hits := roaring.And(bm, targetBM).GetCardinality()
		sg = Subgroup{
			Rows:      int32(cover),
			Hits:      int32(hits),
			Precision: float32(hits) / float32(cover),
			Recall:    float32(hits) / float32(m),
			WRAcc:     float32(float64(cover) / float64(n) * (float64(hits)/float64(cover) - base)),
		}
		sg.Literals = make([]Literal, len(lits))
		for i, l := range lits {
			sg.Literals[i] = literals[l]
		}
		ok = true
		return
	}
	negations := func(c *cand) (n int) {
		for _, l := range c.lits {
			if !literals[l].Present {
				n++
			}
		}
		return
	}
	// a is strictly better than b: by quality, then by fewer negations —
	// "has geo" reads better than "lacks sym" when the two coincide — then
	// by the earlier literal sequence.
	better := func(a, b *cand) bool {
		if a.sg.WRAcc != b.sg.WRAcc {
			return a.sg.WRAcc > b.sg.WRAcc
		}
		if na, nb := negations(a), negations(b); na != nb {
			return na < nb
		}
		return slices.Compare(a.lits, b.lits) < 0
	}
	beam := []*cand{{lits: nil, rows: roaring.FlipInt(roaring.New(), 0, n)}}
	beam[0].sg, _ = score(beam[0].rows, nil)
	best = beam[0].sg
	best.Literals = []Literal{}
	bestC := beam[0]
	for depth := 0; depth < opts.MaxTerms; depth++ {
		if ctx.Err() != nil {
			break
		}
		var next []*cand
		for _, c := range beam {
			for l := range literals {
				if len(c.lits) > 0 && int32(l) <= c.lits[len(c.lits)-1] {
					continue
				}
				bm := roaring.And(c.rows, litBM[l])
				lits := append(slices.Clone(c.lits), int32(l))
				sg, ok := score(bm, lits)
				if !ok {
					continue
				}
				next = append(next, &cand{lits: lits, rows: bm, sg: sg})
			}
		}
		if len(next) == 0 {
			break
		}
		slices.SortFunc(next, func(a, b *cand) int {
			if better(a, b) {
				return -1
			}
			if better(b, a) {
				return 1
			}
			return 0
		})
		if len(next) > opts.Beam {
			next = next[:opts.Beam]
		}
		if !better(next[0], bestC) || float64(next[0].sg.WRAcc) < float64(bestC.sg.WRAcc)*(1+opts.MinGain) {
			break
		}
		bestC = next[0]
		best = bestC.sg
		beam = next
	}
	return
}

// SQL spells the subgroup as a predicate: literals joined by AND, an
// absent item as NOT (…). spell gives an item's predicate, or false when
// the item has no spelling, in which case ok is false and the text names
// the item in a comment — by name(item) when name is given, else by
// index.
func (inst Subgroup) SQL(spell func(item int32) (string, bool), name func(item int32) string) (sql string, ok bool) {
	if len(inst.Literals) == 0 {
		return "true", true
	}
	ok = true
	var b strings.Builder
	for i, l := range inst.Literals {
		if i > 0 {
			b.WriteString(" AND ")
		}
		pred, has := spell(l.Item)
		if !has {
			ok = false
			label := "item " + itoa(l.Item)
			if name != nil {
				label = name(l.Item)
			}
			pred = "/* " + label + " has no SQL spelling */ true"
		}
		if l.Present {
			b.WriteString(pred)
		} else {
			b.WriteString("NOT (")
			b.WriteString(pred)
			b.WriteString(")")
		}
	}
	sql = b.String()
	return
}

func itoa(v int32) string {
	return strconv.FormatInt(int64(v), 10)
}
