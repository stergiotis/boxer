package lwlens

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// The archetype form reads a cluster as one line — what it typically holds
// in each slot — and under it only the rows that break the pattern, so a
// batch of n rows in k clusters reads in about k lines plus its exceptions.
// What the form says is computed here, as data; leewaywidgets.LensView paints
// it and play's archetypes operation returns it (ADR-0289 §SD4, proposed).

const (
	// TemplateAt is the band support a slot needs to be in its template.
	TemplateAt = 0.5
	// OutlierAt is the value surprise over which a value is an exception:
	// the outer 5% of its slot at either end.
	OutlierAt = 0.9
	// RareShare is the most of a band a label value may hold and still be
	// an exception: rare against a slot whose band has a typical value.
	RareShare = 0.1
	// ExtremesMin is the fewest values a band needs in a slot for its
	// extremes to be named: with two, the lowest and highest are the band.
	ExtremesMin = 3
)

// Typical is what a band typically holds in one slot.
type Typical struct {
	Slot int32
	// Support is the share of the band's rows carrying the slot.
	Support float32
	// Count is how many of the band's values the reading is over: its
	// numbers when Numeric, its texts otherwise.
	Count int
	// Numeric is set for a numeric slot with numbers in the band; Median,
	// Low and High are then their 50th, 10th and 90th percentile.
	Numeric           bool
	Median, Low, High float64
	// Mode is the most frequent text, Share the part of the band's texts it
	// is, Distinct how many texts differ.
	Mode     string
	Share    float64
	Distinct int
	// First and Last are the least and greatest text: the span the texts
	// cover when they are times.
	First, Last string
}

// Extreme names the rows holding a band's lowest and highest value in one
// numeric slot — what a reader asking for a band's maximum needs named,
// whether or not it is the batch's strangest value, which is what exceptions
// rank by (the lens exploration's third judged round found the two differ).
type Extreme struct {
	Slot                int32
	LowRow, HighRow     int32
	LowValue, HighValue float64
}

// DepartureKindE is how a row departs from its band.
type DepartureKindE uint8

const (
	// DepartureKindMissing: the band nearly always has the slot; the row lacks it.
	DepartureKindMissing DepartureKindE = iota
	// DepartureKindUnexpected: the row has a slot the band rarely has.
	DepartureKindUnexpected
	// DepartureKindRareLabel: a label value few of the band's rows hold.
	DepartureKindRareLabel
	// DepartureKindOutlier: a number in the outer ends of its slot.
	DepartureKindOutlier
)

func (inst DepartureKindE) String() string {
	switch inst {
	case DepartureKindMissing:
		return "missing"
	case DepartureKindUnexpected:
		return "unexpected"
	case DepartureKindRareLabel:
		return "rare label"
	}
	return "outlier"
}

// Departure is one way a row breaks its band's pattern.
type Departure struct {
	Kind DepartureKindE
	Slot int32
	// Text is the rare label, or the outlier's text.
	Text string
	// Count is, for a rare label, how many of the band's rows hold it: a
	// reader who sees every one of them needs no "more rows" line to
	// conclude none was cut (the fourth judged round's readers could only
	// infer it).
	Count int
	// Value and High are an outlier's number and whether it sits above the
	// slot's median.
	Value float64
	High  bool
}

// ExceptionClassE ranks what an exception says, most telling first: a
// budget cuts from the end, so a missing slot is never lost to a numeric
// outlier (the first judged round found it was).
type ExceptionClassE uint8

const (
	ExceptionClassStructural ExceptionClassE = iota
	ExceptionClassRareLabel
	ExceptionClassOutlier
)

// Exception is rows departing from their band in the same ways.
type Exception struct {
	// Rows are the rows, the first the one the line is read from.
	Rows       []int32
	Departures []Departure
	Class      ExceptionClassE
	// Surprise is the most extreme value surprise among the outliers, which
	// orders exceptions within a class: a budget cuts the mildest.
	Surprise float64
}

// Archetype is one band of a plan read as the archetype form.
type Archetype struct {
	// Band indexes Analysis.Bands; PlanBand indexes Plan.Bands.
	Band, PlanBand int
	// Constants are the slots every row of the band has with one value.
	Constants []int32
	// Template is what the band typically holds in each slot at least
	// TemplateAt of it carries, in canonical order, its constants left out.
	// Empty for the unclustered rows, which have no pattern to break.
	Template []Typical
	Extremes []Extreme
	// Exceptions are most telling first. Rare labels and outliers are read
	// at the gist detail and above.
	Exceptions []Exception
}

// Archetypes reads every band of p as the archetype form, in plan order.
func Archetypes(a *Analysis, p *Plan) (out []Archetype) {
	for pi := range p.Bands {
		pb := &p.Bands[pi]
		b := &a.Bands[pb.Band]
		ar := Archetype{Band: pb.Band, PlanBand: pi, Constants: pb.Constants}
		if b.Cluster >= 0 {
			for _, s := range a.Order {
				if b.Support[s] >= TemplateAt && !slices.Contains(pb.Constants, s) && !slices.Contains(p.Constants, s) {
					ar.Template = append(ar.Template, BandTypical(a, b, s))
				}
			}
			slots := make([]int32, 0, len(ar.Template))
			for _, t := range ar.Template {
				slots = append(slots, t.Slot)
			}
			ar.Extremes = BandExtremes(a, b, slots)
			ar.Exceptions = BandExceptions(a, p.Detail, pb, b)
		}
		out = append(out, ar)
	}
	return
}

// BandValues is a band's values in slot s: its numbers ascending, its texts
// in row order.
func BandValues(a *Analysis, b *Band, s int32) (nums []float64, texts []string) {
	for _, r := range b.Rows {
		if cell, ok := a.Model.Rows[r].Cell(s); ok {
			if cell.HasNum {
				nums = append(nums, cell.Num)
			}
			texts = append(texts, cell.Text)
		}
	}
	slices.Sort(nums)
	return
}

// mode is the most frequent text, its share and how many texts differ.
func mode(texts []string) (m string, share float64, distinct int) {
	counts := map[string]int{}
	best := 0
	for _, t := range texts {
		counts[t]++
		if k := counts[t]; k > best || (k == best && t < m) {
			m, best = t, k
		}
	}
	if len(texts) > 0 {
		share = float64(best) / float64(len(texts))
	}
	return m, share, len(counts)
}

// quantile reads sorted at q, nearest rank.
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(q*float64(len(sorted)-1)+0.5)]
}

// BandTypical is what band b typically holds in slot s.
func BandTypical(a *Analysis, b *Band, s int32) (t Typical) {
	nums, texts := BandValues(a, b, s)
	t.Slot, t.Support = s, b.Support[s]
	if a.Model.Slots[s].Kind == ValueKindNumeric && len(nums) > 0 {
		t.Numeric, t.Count = true, len(nums)
		t.Median, t.Low, t.High = quantile(nums, 0.5), quantile(nums, 0.1), quantile(nums, 0.9)
		return
	}
	t.Count = len(texts)
	t.Mode, t.Share, t.Distinct = mode(texts)
	if len(texts) > 0 {
		t.First, t.Last = slices.Min(texts), slices.Max(texts)
	}
	return
}

// BandExtremes names, per numeric slot of slots the band mostly has, the rows
// holding the band's lowest and highest value.
func BandExtremes(a *Analysis, b *Band, slots []int32) (out []Extreme) {
	m := a.Model
	for _, s := range slots {
		if m.Slots[s].Kind != ValueKindNumeric || b.Support[s] < TemplateAt {
			continue
		}
		e := Extreme{Slot: s, LowRow: -1, HighRow: -1}
		n := 0
		for _, r := range b.Rows {
			cell, ok := m.Rows[r].Cell(s)
			if !ok || !cell.HasNum {
				continue
			}
			n++
			if e.LowRow < 0 || cell.Num < e.LowValue {
				e.LowRow, e.LowValue = r, cell.Num
			}
			if e.HighRow < 0 || cell.Num > e.HighValue {
				e.HighRow, e.HighValue = r, cell.Num
			}
		}
		if n >= ExtremesMin && e.LowValue != e.HighValue {
			out = append(out, e)
		}
	}
	return
}

// rareLabels finds, per label slot the band mostly has, the values rare
// enough to be exceptions, with how many of the band's rows hold each: the
// slot must have a typical value in the band (one held by half its rows or
// more), and the value is held by at most RareShare of them, one row at
// least.
func rareLabels(a *Analysis, b *Band) (rare map[int32]map[string]int) {
	rare = map[int32]map[string]int{}
	m := a.Model
	for s := range m.Slots {
		sl := int32(s)
		if m.Slots[s].Kind == ValueKindNumeric || b.Support[sl] < TemplateAt {
			continue
		}
		_, texts := BandValues(a, b, sl)
		typical, share, _ := mode(texts)
		if share < 0.5 {
			continue
		}
		counts := map[string]int{}
		for _, t := range texts {
			counts[t]++
		}
		limit := max(1, int(RareShare*float64(len(texts))))
		for t, n := range counts {
			if t != typical && n <= limit {
				if rare[sl] == nil {
					rare[sl] = map[string]int{}
				}
				rare[sl][t] = n
			}
		}
	}
	return rare
}

// BandExceptions lists the band's rows that break its pattern — missing and
// unexpected slots, and from the gist detail up rare labels and extreme
// values — most telling first, and within a class most extreme first. Rows
// that depart in the same ways share an exception.
func BandExceptions(a *Analysis, detail DetailE, pb *PlanBand, b *Band) (out []Exception) {
	idx := map[string]int{}
	m := a.Model
	var rare map[int32]map[string]int
	if detail >= DetailGist {
		rare = rareLabels(a, b)
	}
	var key strings.Builder
	for _, pr := range pb.Rows {
		var deps []Departure
		var surprise float64
		class := ExceptionClassOutlier
		if len(pr.Missing)+len(pr.Unexpected) > 0 {
			class = ExceptionClassStructural
		}
		for _, s := range pr.Missing {
			deps = append(deps, Departure{Kind: DepartureKindMissing, Slot: s})
		}
		for _, s := range pr.Unexpected {
			deps = append(deps, Departure{Kind: DepartureKindUnexpected, Slot: s})
		}
		if detail >= DetailGist {
			row := &m.Rows[pr.Row]
			for _, cell := range row.Cells {
				if n := rare[cell.Slot][cell.Text]; n > 0 {
					deps = append(deps, Departure{Kind: DepartureKindRareLabel, Slot: cell.Slot, Text: cell.Text, Count: n})
					class = min(class, ExceptionClassRareLabel)
				}
			}
			for _, cell := range row.Cells {
				s := cell.Slot
				if m.Slots[s].Kind != ValueKindNumeric || !cell.HasNum || b.Support[s] < TemplateAt {
					continue
				}
				vs := ValueSurprise(a, s, &cell)
				if vs < OutlierAt {
					continue
				}
				surprise = max(surprise, vs)
				deps = append(deps, Departure{Kind: DepartureKindOutlier, Slot: s, Text: cell.Text, Value: cell.Num,
					High: a.Stats[s].Percentile(cell.Num) >= 0.5})
			}
		}
		if len(deps) == 0 {
			continue
		}
		rows := append([]int32{pr.Row}, pr.Also...)
		key.Reset()
		for _, d := range deps {
			key.WriteString(strconv.Itoa(int(d.Kind)))
			key.WriteByte(0)
			key.WriteString(strconv.Itoa(int(d.Slot)))
			key.WriteByte(0)
			key.WriteString(d.Text)
			key.WriteByte(0)
		}
		if i, ok := idx[key.String()]; ok {
			out[i].Rows = append(out[i].Rows, rows...)
			continue
		}
		idx[key.String()] = len(out)
		out = append(out, Exception{Rows: rows, Departures: deps, Class: class, Surprise: surprise})
	}
	slices.SortStableFunc(out, func(x, y Exception) int {
		if x.Class != y.Class {
			return cmp.Compare(x.Class, y.Class)
		}
		return cmp.Compare(y.Surprise, x.Surprise)
	})
	return
}
