package lwlens

import (
	"cmp"
	"slices"
)

// ValueKindE is how a slot's values are read: as magnitudes, as a small set
// of labels, or as free text.
type ValueKindE uint8

const (
	ValueKindText ValueKindE = iota
	ValueKindNumeric
	ValueKindCategorical
)

// Slot is one attribute identity: a section and the primary membership of
// the attributes filed under it. A plain column is a slot of the pseudo
// section Plain.
type Slot struct {
	Section string
	Member  string
	Plain   bool
	// NumericType is set when the slot's value column has a numeric scalar
	// canonical type; Kind is decided later from the values seen.
	NumericType bool
	Kind        ValueKindE
}

// Label is the slot as one string: section·member, or the section alone
// when the attributes carry no membership.
func (inst Slot) Label() string {
	if inst.Member == "" {
		return inst.Section
	}
	return inst.Section + "·" + inst.Member
}

// Cell is what one row holds in one slot. Several attributes of a row under
// one slot are one cell of that arity; Text is the first one's value.
type Cell struct {
	Slot   int32
	Text   string
	Num    float64
	HasNum bool
	Arity  int32
	// Series is a numeric array value, at most MaxSeries of its items.
	Series []float64
}

// MaxSeries bounds the items a cell keeps of a numeric array.
const MaxSeries = 256

// Row is one entity.
type Row struct {
	Label string
	// Cells are sorted by Slot.
	Cells []Cell
}

// Cell finds the row's cell in slot s.
func (inst *Row) Cell(s int32) (c *Cell, ok bool) {
	i, found := slices.BinarySearchFunc(inst.Cells, s, func(c Cell, s int32) int { return cmp.Compare(c.Slot, s) })
	if !found {
		return nil, false
	}
	return &inst.Cells[i], true
}

// Model is a batch read as rows of slots.
type Model struct {
	Rows  []Row
	Slots []Slot
	// Sections are the section names in first-seen order; Plain is first
	// when there are plain slots.
	Sections []string
}

// PlainSection is the pseudo section plain columns are filed under.
const PlainSection = "plain"

// SectionOf is the index of slot s's section in Sections.
func (inst *Model) SectionOf(s int32) int {
	return slices.Index(inst.Sections, inst.Slots[s].Section)
}
