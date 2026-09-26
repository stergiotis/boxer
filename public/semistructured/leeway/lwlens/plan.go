package lwlens

import (
	"cmp"
	"encoding/binary"
	"math"
	"slices"
)

// Intent is what the reader wants from the batch, as two positions in
// [0, 1].
type Intent struct {
	// Values runs from structure (0: which slots a row has, and how that
	// sets it apart) to values (1: what the slots hold).
	Values float64
	// Stable runs from local (0: each row drawn on its own terms, its most
	// telling slots first) to stable (1: every row drawn against one frame,
	// so a slot sits in the same place in every row).
	Stable float64
}

// DetailE is how much of a value a cell shows.
type DetailE uint8

const (
	// DetailShape: presence only.
	DetailShape DetailE = iota
	// DetailFingerprint: a value's standing among its slot's values — a
	// percentile bar, a category chip — without its text.
	DetailFingerprint
	// DetailGist: the standing and a short text.
	DetailGist
	// DetailValues: the text.
	DetailValues
)

func (inst DetailE) String() string {
	switch inst {
	case DetailShape:
		return "shape"
	case DetailFingerprint:
		return "fingerprint"
	case DetailGist:
		return "gist"
	}
	return "values"
}

// ScopeE is what a row's frame is shared with.
type ScopeE uint8

const (
	// ScopeRow: no frame; each row lists its own slots.
	ScopeRow ScopeE = iota
	// ScopeBand: one frame per band, from the slots typical of it.
	ScopeBand
	// ScopeGlobal: one frame for the batch.
	ScopeGlobal
)

func (inst ScopeE) String() string {
	switch inst {
	case ScopeRow:
		return "row"
	case ScopeBand:
		return "band"
	}
	return "global"
}

// Thresholds of the two intents. Each intent runs through three regimes;
// within a regime it slides a threshold, so every position of a slider
// changes the picture a little rather than at three points.
const (
	detailFingerprintAt = 0.25
	detailGistAt        = 0.5
	detailValuesAt      = 0.75
	scopeBandAt         = 1.0 / 3
	scopeGlobalAt       = 2.0 / 3
	// salienceOrderBelow: under it a row-scope row is ordered by its own
	// salience; over it, by the canonical order.
	salienceOrderBelow = 1.0 / 6
	// bandFrameMaxTau and globalFrameMaxTau are the support a slot needs to
	// enter a frame at the start of its regime; it falls to any support at
	// the regime's end.
	bandFrameMaxTau   = 0.5
	globalFrameMaxTau = 0.25
	// expectedAt and unexpectedAt are the band supports over which a
	// missing slot is a deviation, and under which a present one is.
	expectedAt   = 0.75
	unexpectedAt = 0.25
	// minBandForDeviation is the band size under which its supports are
	// too coarse to call anything a deviation.
	minBandForDeviation = 4
)

// Plan is what to draw.
type Plan struct {
	Intent Intent
	Detail DetailE
	Scope  ScopeE
	// Frame is the shared frame under ScopeGlobal, nil otherwise.
	Frame []int32
	// Constants are, under ScopeGlobal, the slots every row has with one
	// value.
	Constants []int32
	Bands     []PlanBand
}

// PlanBand is one band's rows and, under ScopeBand, its frame.
type PlanBand struct {
	Band  int
	Frame []int32
	Rows  []PlanRow
	// Constants are slots every row of the band has with one value, taken
	// out of the frame: a fact about the band, said once in its header
	// rather than repeated down a column.
	Constants []int32
}

// PlanRow is one row: the slots it is drawn with besides its frame, and its
// deviations from its band.
type PlanRow struct {
	Row int32
	// Own is the row's slots outside the frame — every slot it has under
	// ScopeRow — in drawing order.
	Own []int32
	// Missing are slots its band nearly always has and it lacks;
	// Unexpected, slots it has that its band rarely does.
	Missing    []int32
	Unexpected []int32
	// Also are the rows this one stands for: under DetailShape, rows with
	// the same slots as it are one line, since they say the same thing.
	Also []int32
}

// DetailOf and ScopeOf read an intent's regimes.
func DetailOf(values float64) DetailE {
	switch {
	case values < detailFingerprintAt:
		return DetailShape
	case values < detailGistAt:
		return DetailFingerprint
	case values < detailValuesAt:
		return DetailGist
	}
	return DetailValues
}

func ScopeOf(stable float64) ScopeE {
	switch {
	case stable < scopeBandAt:
		return ScopeRow
	case stable < scopeGlobalAt:
		return ScopeBand
	}
	return ScopeGlobal
}

// within is how far x is through [lo, hi), in [0, 1].
func within(x, lo, hi float64) float64 {
	return math.Min(1, math.Max(0, (x-lo)/(hi-lo)))
}

// PlanRows plans a drawing of a under intent.
func PlanRows(a *Analysis, intent Intent) (p Plan) {
	m := a.Model
	p.Intent = intent
	p.Detail = DetailOf(intent.Values)
	p.Scope = ScopeOf(intent.Stable)

	// Under DetailShape a slot every row has, or a plain one, says nothing:
	// the picture is of what varies.
	informative := func(s int32) bool {
		if p.Detail != DetailShape {
			return true
		}
		return !m.Slots[s].Plain && a.Support[s] < 1
	}
	frameOf := func(support []float32, tau float32) (frame []int32) {
		for _, s := range a.Order {
			if support[s] > 0 && support[s] >= tau && informative(s) {
				frame = append(frame, s)
			}
		}
		return
	}
	if p.Scope == ScopeGlobal {
		t := within(intent.Stable, scopeGlobalAt, 1)
		p.Frame = frameOf(a.Support, float32(globalFrameMaxTau*(1-t)))
		if p.Detail != DetailShape {
			all := make([]int32, len(m.Rows))
			for i := range all {
				all[i] = int32(i)
			}
			p.Frame, p.Constants = splitConstants(m, p.Frame, all)
		}
	}
	rank := make([]int, len(m.Slots))
	for i, s := range a.Order {
		rank[s] = i
	}
	for bi := range a.Bands {
		b := &a.Bands[bi]
		pb := PlanBand{Band: bi}
		frame := p.Frame
		if p.Scope == ScopeBand {
			t := within(intent.Stable, scopeBandAt, scopeGlobalAt)
			pb.Frame = frameOf(b.Support, float32(bandFrameMaxTau*(1-t)))
			frame = pb.Frame
		}
		if p.Detail != DetailShape && p.Scope != ScopeGlobal && len(b.Rows) > 1 {
			// Under ScopeRow the constants come from the band's typical
			// slots, so a row's own terms are what sets it apart.
			pool := frame
			if p.Scope == ScopeRow {
				pool = frameOf(b.Support, 1)
			}
			rest, constants := splitConstants(m, pool, b.Rows)
			pb.Constants = constants
			if p.Scope == ScopeBand {
				pb.Frame, frame = rest, rest
			}
		}
		covered := func(s int32) bool {
			return slices.Contains(frame, s) || slices.Contains(pb.Constants, s) || slices.Contains(p.Constants, s)
		}
		for _, r := range b.Rows {
			row := &m.Rows[r]
			pr := PlanRow{Row: r}
			for _, c := range row.Cells {
				if informative(c.Slot) && !covered(c.Slot) {
					pr.Own = append(pr.Own, c.Slot)
				}
			}
			if p.Scope == ScopeRow && intent.Stable < salienceOrderBelow {
				orderBySalience(a, b, row, pr.Own, intent.Values, rank)
			} else {
				slices.SortStableFunc(pr.Own, func(x, y int32) int { return cmp.Compare(rank[x], rank[y]) })
			}
			if len(b.Rows) >= minBandForDeviation && a.Clustered && b.Cluster >= 0 {
				for _, s := range a.Order {
					if !informative(s) {
						continue
					}
					_, has := row.Cell(s)
					switch {
					case !has && b.Support[s] >= expectedAt:
						pr.Missing = append(pr.Missing, s)
					case has && b.Support[s] <= unexpectedAt:
						pr.Unexpected = append(pr.Unexpected, s)
					}
				}
			}
			pb.Rows = append(pb.Rows, pr)
		}
		if p.Detail == DetailShape {
			pb.Rows = collapse(a, pb.Rows, informative)
		} else if p.Scope != ScopeRow {
			seriate(a, pb.Rows, intent.Values)
		}
		p.Bands = append(p.Bands, pb)
	}
	return
}

// Salience is how much a row's cell in slot s tells a reader, mixing how
// unusual it is for the row to have the slot at all (structure) with how
// unusual its value is (values), by the values intent.
func Salience(a *Analysis, b *Band, s int32, c *Cell, values float64) float64 {
	structural := 1 - float64(b.Support[s])
	return (1-values)*structural + values*ValueSurprise(a, s, c)
}

// ValueSurprise is how far a cell's value is from its slot's typical one,
// in [0, 1]: distance from the median for a number, rarity for a label.
func ValueSurprise(a *Analysis, s int32, c *Cell) float64 {
	if c == nil {
		return 0
	}
	st := &a.Stats[s]
	switch a.Model.Slots[s].Kind {
	case ValueKindNumeric:
		if !c.HasNum {
			return 0
		}
		return math.Abs(2*st.Percentile(c.Num) - 1)
	case ValueKindCategorical:
		var total int32
		for _, k := range st.Counts {
			total += k
		}
		if i := st.CategoryRank(c.Text); i >= 0 && total > 0 {
			return 1 - float64(st.Counts[i])/float64(total)
		}
	}
	return 0.3
}

// orderBySalience orders a row's slots by what they tell a reader, keeping
// each section's slots together: the sections by their most salient slot,
// the slots within a section by their own. Interleaving sections by salience
// alone scatters a section into runs, and the row stops reading as records.
func orderBySalience(a *Analysis, b *Band, row *Row, slots []int32, values float64, rank []int) {
	m := a.Model
	sal := make(map[int32]float64, len(slots))
	secSal := make(map[string]float64, 4)
	for _, s := range slots {
		c, _ := row.Cell(s)
		v := Salience(a, b, s, c, values)
		sal[s] = v
		sec := m.Slots[s].Section
		secSal[sec] = math.Max(secSal[sec], v)
	}
	slices.SortStableFunc(slots, func(x, y int32) int {
		sx, sy := m.Slots[x].Section, m.Slots[y].Section
		if sx != sy {
			if c := cmp.Compare(secSal[sy], secSal[sx]); c != 0 {
				return c
			}
			return cmp.Compare(m.SectionOf(x), m.SectionOf(y))
		}
		if c := cmp.Compare(sal[y], sal[x]); c != 0 {
			return c
		}
		return cmp.Compare(rank[x], rank[y])
	})
}

// collapse merges rows with the same informative slots into one, the first
// by label standing for the rest; the groups are ordered largest first, so a
// band reads as its pattern and then its exceptions.
func collapse(a *Analysis, rows []PlanRow, informative func(int32) bool) (out []PlanRow) {
	idx := map[string]int{}
	var key []byte
	for _, pr := range rows {
		key = key[:0]
		for _, c := range a.Model.Rows[pr.Row].Cells {
			if informative(c.Slot) {
				key = binary.AppendUvarint(key, uint64(c.Slot))
			}
		}
		if i, ok := idx[string(key)]; ok {
			out[i].Also = append(out[i].Also, pr.Row)
			continue
		}
		idx[string(key)] = len(out)
		out = append(out, pr)
	}
	slices.SortStableFunc(out, func(x, y PlanRow) int { return cmp.Compare(len(y.Also), len(x.Also)) })
	return
}

// splitConstants takes out of frame the slots every one of rows has with the
// same value.
func splitConstants(m *Model, frame []int32, rows []int32) (rest []int32, constants []int32) {
	for _, s := range frame {
		constant := len(rows) > 1
		var first string
		for i, r := range rows {
			c, ok := m.Rows[r].Cell(s)
			if !ok || c.Arity != 1 || (i > 0 && c.Text != first) {
				constant = false
				break
			}
			first = c.Text
		}
		if constant {
			constants = append(constants, s)
		} else {
			rest = append(rest, s)
		}
	}
	return
}

// RowDistance is how far row q is from row r: the Jaccard distance of their
// slots, blended by the values intent with the mean rank distance of the
// numbers they share.
func RowDistance(a *Analysis, r, q int32, values float64) float64 {
	rr, qq := &a.Model.Rows[r], &a.Model.Rows[q]
	inter, union := 0, 0
	var dv float64
	nv := 0
	i, j := 0, 0
	for i < len(rr.Cells) || j < len(qq.Cells) {
		switch {
		case j == len(qq.Cells) || (i < len(rr.Cells) && rr.Cells[i].Slot < qq.Cells[j].Slot):
			union++
			i++
		case i == len(rr.Cells) || qq.Cells[j].Slot < rr.Cells[i].Slot:
			union++
			j++
		default:
			inter++
			union++
			a1, b1 := &rr.Cells[i], &qq.Cells[j]
			s := a1.Slot
			if a1.HasNum && b1.HasNum && a.Model.Slots[s].Kind == ValueKindNumeric {
				dv += math.Abs(a.Stats[s].Percentile(a1.Num) - a.Stats[s].Percentile(b1.Num))
				nv++
			} else if a1.Text != b1.Text {
				dv++
				nv++
			} else {
				nv++
			}
			i++
			j++
		}
	}
	structural := 1.0
	if union > 0 {
		structural = 1 - float64(inter)/float64(union)
	}
	valueDist := 1.0
	if nv > 0 {
		valueDist = dv / float64(nv)
	}
	return (1-values)*structural + values*(0.5*structural+0.5*valueDist)
}

// seriate orders rows so that each is followed by the nearest of those
// left, starting from the first: a greedy chain that puts like rows
// together, so a column of values shows runs rather than noise.
func seriate(a *Analysis, rows []PlanRow, values float64) {
	if len(rows) < 3 {
		return
	}
	left := slices.Clone(rows[1:])
	out := rows[:1]
	for len(left) > 0 {
		last := out[len(out)-1].Row
		best, bestD := 0, math.Inf(1)
		for i, pr := range left {
			if d := RowDistance(a, last, pr.Row, values); d < bestD {
				best, bestD = i, d
			}
		}
		out = append(out, left[best])
		left = slices.Delete(left, best, best+1)
	}
}
