package lwlens

import (
	"cmp"
	"encoding/hex"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// maxCellText bounds what a cell keeps of its value: the lens draws a
// glimpse of a value, never the whole of a long one.
const maxCellText = 160

// numericScalar matches a scalar number canonical type: u8…u64, i8…i64,
// f32, f64 — not an array (h) or a set (m).
var numericScalar = regexp.MustCompile(`^[uif][0-9]+$`)

// numericArray matches an array or a set of numbers.
var numericArray = regexp.MustCompile(`^[uif][0-9]+[hm]$`)

// Sink collects a Model from a batch driven through it.
type Sink struct {
	model    Model
	renderer *membership.Renderer
	slotIdx  map[string]int32

	// Entity state.
	row       Row
	rowCells  map[int32]int
	label     string
	labelRank int

	// Section state.
	plain       bool
	plainIdType bool
	section     string
	numericCol0 bool
	arrayCol0   bool

	// Attribute state.
	colIdx   int
	colName  string
	inTags   bool
	member   string
	text     strings.Builder
	value    string
	hasValue bool
	isArray  bool
	// items collects a numeric array's items as they are written.
	items    []float64
	itemText strings.Builder
	inItem   bool
}

var _ streamreadaccess.SinkI = (*Sink)(nil)
var _ streamreadaccess.MembershipSinkI = (*Sink)(nil)

// NewSink returns an empty collector rendering memberships with renderer;
// nil takes membership.DefaultRenderer.
func NewSink(renderer *membership.Renderer) *Sink {
	if renderer == nil {
		renderer = membership.DefaultRenderer()
	}
	return &Sink{renderer: renderer, slotIdx: make(map[string]int32, 32), rowCells: make(map[int32]int, 16)}
}

// Model is what the last batch was read as.
func (inst *Sink) Model() *Model { return &inst.model }

func (inst *Sink) BeginBatch() {
	inst.model = Model{}
	clear(inst.slotIdx)
}
func (inst *Sink) EndBatch() (err error) { return nil }

func (inst *Sink) BeginEntity() {
	inst.row = Row{}
	clear(inst.rowCells)
	inst.label, inst.labelRank = "", 0
}

func (inst *Sink) EndEntity() (err error) {
	label := inst.label
	if label == "" {
		label = "#" + strconv.Itoa(len(inst.model.Rows))
	}
	inst.row.Label = label
	slices.SortFunc(inst.row.Cells, func(a, b Cell) int { return cmp.Compare(a.Slot, b.Slot) })
	inst.model.Rows = append(inst.model.Rows, inst.row)
	return nil
}

func (inst *Sink) BeginPlainSection(itemType common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.plain = true
	inst.plainIdType = itemType == common.PlainItemTypeEntityId
	inst.colIdx = 0
}
func (inst *Sink) EndPlainSection() (err error) {
	inst.plain = false
	return nil
}
func (inst *Sink) BeginPlainValue()           {}
func (inst *Sink) EndPlainValue() (err error) { return nil }
func (inst *Sink) BeginTaggedSections()       {}
func (inst *Sink) EndTaggedSections() (err error) {
	return nil
}
func (inst *Sink) BeginCoSectionGroup(_ naming.Key) {}
func (inst *Sink) EndCoSectionGroup() (err error)   { return nil }

func (inst *Sink) BeginSection(name naming.StylableName, _ []naming.StylableName, types []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, _ int) {
	inst.section = string(name)
	inst.numericCol0 = len(types) > 0 && types[0] != nil && numericScalar.MatchString(types[0].String())
	inst.arrayCol0 = len(types) > 0 && types[0] != nil && numericArray.MatchString(types[0].String())
}
func (inst *Sink) EndSection() (err error) { return nil }

func (inst *Sink) BeginTaggedValue() {
	inst.colIdx, inst.member, inst.hasValue, inst.value = 0, "", false, ""
	inst.items = inst.items[:0]
}

func (inst *Sink) EndTaggedValue() (err error) {
	s := inst.slot(inst.section, inst.member, false, inst.numericCol0)
	inst.addCell(s, inst.value, inst.hasValue && !inst.isArray)
	if inst.arrayCol0 && len(inst.items) > 0 {
		if i, ok := inst.rowCells[s]; ok && inst.row.Cells[i].Series == nil {
			inst.row.Cells[i].Series = slices.Clone(inst.items)
		}
	}
	return nil
}

// slot finds or registers a slot.
func (inst *Sink) slot(section, member string, plain, numeric bool) int32 {
	key := section + "\x00" + member
	if plain {
		key = "\x01" + key
	}
	s, ok := inst.slotIdx[key]
	if !ok {
		s = int32(len(inst.model.Slots))
		inst.slotIdx[key] = s
		inst.model.Slots = append(inst.model.Slots, Slot{Section: section, Member: member, Plain: plain, NumericType: numeric})
		if !slices.Contains(inst.model.Sections, section) {
			if plain {
				inst.model.Sections = slices.Insert(inst.model.Sections, 0, section)
			} else {
				inst.model.Sections = append(inst.model.Sections, section)
			}
		}
	}
	return s
}

func (inst *Sink) addCell(s int32, text string, parse bool) {
	if i, ok := inst.rowCells[s]; ok {
		inst.row.Cells[i].Arity++
		return
	}
	c := Cell{Slot: s, Text: text, Arity: 1}
	if parse {
		if v, err := strconv.ParseFloat(text, 64); err == nil {
			c.Num, c.HasNum = v, true
		}
	}
	inst.rowCells[s] = len(inst.row.Cells)
	inst.row.Cells = append(inst.row.Cells, c)
}

func (inst *Sink) BeginColumn(_ streamreadaccess.PhysicalColumnAddr, name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.colName = string(name)
	inst.text.Reset()
	inst.isArray = false
	if inst.plain {
		inst.numericCol0 = ct != nil && numericScalar.MatchString(ct.String())
	}
}

func (inst *Sink) EndColumn() {
	text := cleanText(inst.text.String())
	switch {
	case inst.plain:
		if !inst.offerLabel(inst.colName, text) && text != "" {
			s := inst.slot(PlainSection, inst.colName, true, inst.numericCol0)
			inst.addCell(s, text, !inst.isArray)
		}
	case inst.colIdx == 0:
		inst.value, inst.hasValue = text, true
	}
	inst.colIdx++
}

// offerLabel keeps the best entity label seen — a natural key over any other
// key-like column over the id — and reports whether the column was taken as
// a label rather than a slot.
func (inst *Sink) offerLabel(col string, text string) (isLabel bool) {
	lc := strings.ToLower(col)
	rank := 0
	switch {
	case strings.Contains(lc, "natural"):
		rank = 3
	case inst.plainIdType && (strings.Contains(lc, "name") || strings.Contains(lc, "key") || strings.Contains(lc, "label")):
		rank = 2
	case lc == "id":
		rank = 1
	}
	if rank == 0 {
		return false
	}
	if text != "" && rank > inst.labelRank {
		inst.label, inst.labelRank = text, rank
	}
	return true
}

// cleanText makes a value drawable on one line: control characters become
// spaces, bytes that are not text become a byte count, and it is cut to
// maxCellText.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return r == utf8.RuneError }) {
		// Bytes that are not text: their hex, which is how an id or a
		// digest is recognised, cut to a prefix that still tells rows apart.
		b := []byte(s)
		if len(b) > 8 {
			return hex.EncodeToString(b[:8]) + "…"
		}
		return hex.EncodeToString(b)
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > maxCellText {
		rs := []rune(s)
		s = string(rs[:maxCellText-1]) + "…"
	}
	return s
}

func (inst *Sink) BeginScalarValue()           {}
func (inst *Sink) EndScalarValue() (err error) { return nil }
func (inst *Sink) BeginHomogenousArrayValue(_ int) {
	inst.isArray = true
}
func (inst *Sink) EndHomogenousArrayValue() {}
func (inst *Sink) BeginSetValue(_ int)      { inst.isArray = true }
func (inst *Sink) EndSetValue()             {}
func (inst *Sink) BeginValueItem(index int) {
	if index > 0 && !inst.inTags {
		inst.text.WriteString(", ")
	}
	inst.inItem = inst.arrayCol0 && inst.colIdx == 0 && !inst.plain
	inst.itemText.Reset()
}
func (inst *Sink) EndValueItem() {
	if inst.inItem && len(inst.items) < MaxSeries {
		v, err := strconv.ParseFloat(strings.TrimSpace(inst.itemText.String()), 64)
		if err != nil {
			v = math.NaN()
		}
		inst.items = append(inst.items, v)
	}
	inst.inItem = false
}
func (inst *Sink) Write(p []byte) (n int, err error) { return inst.WriteString(string(p)) }
func (inst *Sink) WriteString(s string) (n int, err error) {
	if !inst.inTags && inst.text.Len() <= 4*maxCellText {
		inst.text.WriteString(s)
	}
	if inst.inItem {
		inst.itemText.WriteString(s)
	}
	return len(s), nil
}

func (inst *Sink) BeginTags(_ int) { inst.inTags = true }
func (inst *Sink) EndTags()        { inst.inTags = false }

// tag keeps the first membership: the slot's identity. A mixed membership's
// high-cardinality parameter is an instance of the slot, not a new slot.
func (inst *Sink) tag(label string) {
	if inst.member == "" {
		inst.member = label
	}
}
func (inst *Sink) AddMembershipRef(_ bool, ref uint64) { inst.tag(inst.renderer.RenderRef(ref)) }
func (inst *Sink) AddMembershipVerbatim(_ bool, verbatim string) {
	inst.tag(inst.renderer.RenderVerbatim(verbatim))
}
func (inst *Sink) AddMembershipRefParametrized(_ bool, ref uint64, _ string) {
	inst.tag(inst.renderer.RenderRef(ref))
}
func (inst *Sink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, _ string) {
	inst.tag(inst.renderer.RenderRef(ref))
}
func (inst *Sink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, _ string) {
	inst.tag(inst.renderer.RenderVerbatim(verbatim))
}
