package leewaywidgets

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

var _ streamreadaccess.SinkI = (*TopologySink)(nil)

// TopologySink accumulates a batch's shape into a tree a treemap can draw.
//
// It reads every structural callback and discards every value, so what survives
// is the containment hierarchy a leeway entity declares:
//
//	batch → entity → [co-section group] → section → attribute
//
// Each attribute is a unit-sized leaf, so a section's area is its attribute
// count and an entity's area is its total attribute count. Whether an attribute
// carried a value, membership tags, both, or neither is recorded per leaf and
// surfaced through Coloring().
//
// The sink deliberately does not implement `streamreadaccess.MembershipSinkI`:
// it needs to know only *that* an attribute has tags, which `BeginTags` already
// reports. Per ADR-0072 the driver skips membership emission for sinks lacking
// the capability, which is the intended behaviour here.
//
// This is the native successor to the former `card.SvgTopologySink`, which drew
// the same model as a static SVG of dot glyphs. The dot states became the
// AttrState* colours below; drill-down, hover and animation now come from the
// treemap widget rather than being absent.
type TopologySink struct {
	root  *layout.Node
	state map[*layout.Node]AttrStateE

	// Cursor into the tree under construction.
	entity  *layout.Node
	coGroup *layout.Node
	section *layout.Node

	// Per-attribute accumulation, reset at each BeginTaggedValue.
	inPlain   bool
	inTagged  bool
	attrValue bool
	attrTags  bool
	attrName  string
	attrCols  int

	entitySeq int
	attrSeq   int
}

// AttrStateE records what an attribute slot actually carried. The four states
// are the 2×2 grid of (has value) × (has membership tags); the former SVG
// emitter drew them as ● ◐ ○ · respectively.
type AttrStateE uint8

const (
	// AttrStateEmpty is an attribute slot with neither value nor tags.
	AttrStateEmpty AttrStateE = iota
	// AttrStateTagsOnly is an attribute carrying memberships but no value.
	AttrStateTagsOnly
	// AttrStateValueOnly is an attribute carrying a value but no memberships.
	AttrStateValueOnly
	// AttrStateValueAndTags is a fully populated attribute.
	AttrStateValueAndTags
)

// attrStateCount bounds the palette built by topologyPalette.
const attrStateCount = 4

// attrNameMaxCols caps how many column names an attribute leaf's label joins
// before eliding.
const attrNameMaxCols = 2

func (inst AttrStateE) String() (s string) {
	switch inst {
	case AttrStateEmpty:
		s = "empty"
	case AttrStateTagsOnly:
		s = "tags only"
	case AttrStateValueOnly:
		s = "value only"
	case AttrStateValueAndTags:
		s = "value + tags"
	default:
		s = "?"
	}
	return
}

// NewTopologySink returns a sink ready to be driven. Reuse across batches is
// fine — BeginBatch resets the accumulated tree.
func NewTopologySink() (inst *TopologySink) {
	inst = &TopologySink{}
	inst.reset()
	return
}

func (inst *TopologySink) reset() {
	inst.root = &layout.Node{Name: "batch"}
	inst.state = make(map[*layout.Node]AttrStateE, 64)
	inst.entity = nil
	inst.coGroup = nil
	inst.section = nil
	inst.inPlain = false
	inst.inTagged = false
	inst.entitySeq = 0
	inst.attrSeq = 0
}

// Root returns the accumulated topology tree. Valid after EndBatch; before the
// first batch it is an empty "batch" node, which the treemap renders as an
// empty container rather than panicking.
func (inst *TopologySink) Root() (root *layout.Node) { return inst.root }

// StateOf reports what the given attribute leaf carried. Structural nodes
// (batch / entity / co-group / section) report AttrStateEmpty and false.
func (inst *TopologySink) StateOf(node *layout.Node) (state AttrStateE, ok bool) {
	state, ok = inst.state[node]
	return
}

// Coloring encodes attribute state as cell colour, falling through to the
// treemap's depth colouring for structural nodes. A negative categorical index
// is the documented fall-through signal, so entity / co-group / section cells
// keep the depth ramp that communicates nesting.
func (inst *TopologySink) Coloring() (coloring treemap.ColoringI) {
	coloring = treemap.CompositeColoring(
		treemap.DepthColoring(treemap.DefaultDepthColors),
		treemap.CategoricalColoring(topologyPalette(), func(node *layout.Node) (idx int) {
			st, ok := inst.state[node]
			if !ok {
				return -1
			}
			return int(st)
		}),
	)
	return
}

// topologyPalette maps the four attribute states onto IDS tokens. "Empty" is
// drawn from the neutral grey ramp because absence should read as absence; the
// three populated states take distinct qualitative hues so the value/tags
// distinction survives a monochrome-unfriendly readout (ADR-0156).
func topologyPalette() (p []uint32) {
	p = make([]uint32, attrStateCount)
	pack := func(rgba styletokens.RGBA8) (packed uint32) {
		packed = uint32(rgba.R)<<24 | uint32(rgba.G)<<16 | uint32(rgba.B)<<8 | uint32(rgba.A)
		return
	}
	const emptyGreyT = 0.62 // dim enough to recede behind populated cells
	p[AttrStateEmpty] = pack(styletokens.Sequential(styletokens.SequentialGrayC, emptyGreyT))
	p[AttrStateTagsOnly] = pack(styletokens.QualitativeCycle(1))
	p[AttrStateValueOnly] = pack(styletokens.QualitativeCycle(0))
	p[AttrStateValueAndTags] = pack(styletokens.QualitativeCycle(2))
	return
}

// AttrStateColor returns the retained fill the topology view paints for st —
// the same palette entry the treemap's ColoringI uses. A legend needs this:
// the colours carry the value/tags distinction, so a key drawn in the default
// text colour says nothing.
func AttrStateColor(st AttrStateE) (col color.Color) {
	p := topologyPalette()
	if int(st) >= len(p) {
		st = AttrStateEmpty
	}
	col = color.Hex(p[st]).Keep()
	return
}

// NewTopologyTreemap wires an already-driven sink to a treemap widget. The
// caller owns the returned widget and calls Render() each frame; ids must be
// the post-Mount stack so cell ids stay stable.
//
// MaxNestingDepth(0) renders the whole hierarchy at once — the point of the
// view is to see an entity's shape in one glance — while drill-in still works
// on the top-level boxes.
func NewTopologyTreemap(ids *c.WidgetIdStack, scopeKey string, sink *TopologySink, opts ...treemap.Option) (inst *treemap.Treemap) {
	base := []treemap.Option{
		treemap.WithMaxNestingDepth(0),
		treemap.WithColoring(sink.Coloring()),
	}
	inst = treemap.New(ids, scopeKey, sink.Root(), append(base, opts...)...)
	return
}

// --- Batch ---

func (inst *TopologySink) BeginBatch() { inst.reset() }

func (inst *TopologySink) EndBatch() (err error) { return nil }

// --- Entity ---

func (inst *TopologySink) BeginEntity() {
	inst.entity = &layout.Node{Name: "entity " + strconv.Itoa(inst.entitySeq)}
	inst.entitySeq++
	inst.root.Children = append(inst.root.Children, inst.entity)
}

func (inst *TopologySink) EndEntity() (err error) {
	inst.entity = nil
	return nil
}

// --- Plain sections ---

func (inst *TopologySink) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.section = &layout.Node{Name: plainSectionName(itemType)}
	inst.attach(inst.section)
	inst.inPlain = true
}

func (inst *TopologySink) EndPlainSection() (err error) {
	inst.inPlain = false
	inst.section = nil
	return nil
}

func (inst *TopologySink) BeginPlainValue()           {}
func (inst *TopologySink) EndPlainValue() (err error) { return nil }

// --- Tagged sections ---

func (inst *TopologySink) BeginTaggedSections()           {}
func (inst *TopologySink) EndTaggedSections() (err error) { return nil }

// --- Co-section groups ---

func (inst *TopologySink) BeginCoSectionGroup(name naming.Key) {
	inst.coGroup = &layout.Node{Name: "co · " + name.String()}
	if inst.entity != nil {
		inst.entity.Children = append(inst.entity.Children, inst.coGroup)
	}
}

func (inst *TopologySink) EndCoSectionGroup() (err error) {
	inst.coGroup = nil
	return nil
}

// --- Sections ---

func (inst *TopologySink) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, nAttrs int) {
	inst.section = &layout.Node{Name: name.String()}
	inst.attach(inst.section)
}

func (inst *TopologySink) EndSection() (err error) {
	inst.section = nil
	return nil
}

// attach parents a section under the open co-group when there is one, and
// under the entity otherwise.
func (inst *TopologySink) attach(section *layout.Node) {
	parent := inst.coGroup
	if parent == nil {
		parent = inst.entity
	}
	if parent == nil {
		// Defensive: a section outside any entity would otherwise be dropped
		// silently. Park it at the root so the anomaly is visible.
		parent = inst.root
	}
	parent.Children = append(parent.Children, section)
}

// --- Tagged values ---

func (inst *TopologySink) BeginTaggedValue() {
	inst.inTagged = true
	inst.attrValue = false
	inst.attrTags = false
	inst.attrName = ""
	inst.attrCols = 0
}

func (inst *TopologySink) EndTaggedValue() (err error) {
	inst.inTagged = false
	if inst.section == nil {
		return nil
	}
	state := AttrStateEmpty
	switch {
	case inst.attrValue && inst.attrTags:
		state = AttrStateValueAndTags
	case inst.attrValue:
		state = AttrStateValueOnly
	case inst.attrTags:
		state = AttrStateTagsOnly
	}
	inst.addAttr(inst.attrName, state)
	return nil
}

// addAttr appends a unit-sized attribute leaf and records its state. An unnamed
// attribute falls back to a stable ordinal so cells stay individually labelled.
//
// A section may legitimately hold several attributes over the same columns — a
// repeated `metric` is three `value` attributes, not one — so a name that
// collides with an existing sibling gains an ordinal suffix. Without it the
// treemap would show three indistinguishable cells and hover would not say
// which is which.
func (inst *TopologySink) addAttr(name string, state AttrStateE) {
	if name == "" {
		name = "attr " + strconv.Itoa(inst.attrSeq)
	}
	inst.attrSeq++
	name = inst.disambiguate(name)
	leaf := &layout.Node{Name: name, Size: 1}
	inst.section.Children = append(inst.section.Children, leaf)
	inst.state[leaf] = state
}

// disambiguate suffixes name with " #N" when a sibling in the current section
// already carries it. N counts occurrences, so the second `value` reads
// "value #2".
func (inst *TopologySink) disambiguate(name string) (unique string) {
	n := 1
	for _, sib := range inst.section.Children {
		if sib.Name == name || strings.HasPrefix(sib.Name, name+" #") {
			n++
		}
	}
	if n == 1 {
		return name
	}
	return name + " #" + strconv.Itoa(n)
}

// --- Columns ---

func (inst *TopologySink) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	switch {
	case inst.inPlain && inst.section != nil:
		// A plain section has no tagged-value bracket: every column is itself
		// an attribute, and a plain column always carries a value.
		inst.addAttr(name.String(), AttrStateValueOnly)
	case inst.inTagged:
		// Name a multi-column attribute after its columns — a geo point is
		// "lat·lng", not "lat" — so the label says what the cell actually is.
		// Capped at attrNameMaxCols: past that the label stops being readable
		// in a cell and the shape, not the name, is what this view is for.
		inst.attrCols++
		switch {
		case inst.attrCols > attrNameMaxCols:
			if !strings.HasSuffix(inst.attrName, "…") {
				inst.attrName += "…"
			}
		default:
			if inst.attrName != "" {
				inst.attrName += "·"
			}
			inst.attrName += name.String()
		}
	}
}

func (inst *TopologySink) EndColumn() {}

// --- Value shapes ---

func (inst *TopologySink) BeginScalarValue() {
	if inst.inTagged {
		inst.attrValue = true
	}
}

func (inst *TopologySink) EndScalarValue() (err error) { return nil }

func (inst *TopologySink) BeginHomogenousArrayValue(card int) {
	if inst.inTagged && card > 0 {
		inst.attrValue = true
	}
}

func (inst *TopologySink) EndHomogenousArrayValue() {}

func (inst *TopologySink) BeginSetValue(card int) {
	if inst.inTagged && card > 0 {
		inst.attrValue = true
	}
}

func (inst *TopologySink) EndSetValue() {}

func (inst *TopologySink) BeginValueItem(index int) {}
func (inst *TopologySink) EndValueItem()            {}

// --- Value text is structure-irrelevant and discarded ---

func (inst *TopologySink) Write(p []byte) (n int, err error)       { return len(p), nil }
func (inst *TopologySink) WriteString(s string) (n int, err error) { return len(s), nil }

// --- Tags ---

func (inst *TopologySink) BeginTags(nTags int) {
	if inst.inTagged && nTags > 0 {
		inst.attrTags = true
	}
}

func (inst *TopologySink) EndTags() {}

// plainSectionName gives a plain section the short label the former topology
// emitters used, so the two readouts stay comparable.
func plainSectionName(t common.PlainItemTypeE) (s string) {
	switch t {
	case common.PlainItemTypeEntityId:
		s = "id"
	case common.PlainItemTypeEntityTimestamp:
		s = "ts"
	case common.PlainItemTypeEntityRouting:
		s = "routing"
	case common.PlainItemTypeEntityLifecycle:
		s = "lifecycle"
	case common.PlainItemTypeTransaction:
		s = "tx"
	case common.PlainItemTypeOpaque:
		s = "opaque"
	default:
		s = t.String()
	}
	return
}
