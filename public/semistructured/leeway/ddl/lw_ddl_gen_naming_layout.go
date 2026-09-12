package ddl

import (
	"slices"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// NameLayoutE selects one of the two physical column name arities: a
// plain/backbone name, or a tagged-value name. The two carry different
// component sets — only a tagged name has a section, a role, use-aspects and
// a co-section group — which is why a component's field position is a
// question about a layout and not about the convention as a whole.
type NameLayoutE uint8

const (
	NameLayoutPlain NameLayoutE = iota + 1
	NameLayoutTagged
)

var AllNameLayouts = []NameLayoutE{NameLayoutPlain, NameLayoutTagged}

func (inst NameLayoutE) String() (s string) {
	switch inst {
	case NameLayoutPlain:
		s = "plain"
	case NameLayoutTagged:
		s = "tagged"
	default:
		s = common.InvalidEnumValueString
	}
	return
}

func (inst NameLayoutE) IsValid() (valid bool) {
	switch inst {
	case NameLayoutPlain, NameLayoutTagged:
		valid = true
	}
	return
}

// Explanation returns the layout's component list — alternating component
// names and SeparatorExplanation entries, the form ParseColumns reports
// alongside a parsed name.
func (inst NameLayoutE) Explanation() (components []string) {
	switch inst {
	case NameLayoutPlain:
		components = ColumnsComponentsExplanation13
	case NameLayoutTagged:
		components = ColumnsComponentsExplanation21
	}
	return
}

// FieldCount reports how many separator-joined fields a name in this layout
// carries. The explanation lists separators between the components, so the
// field count is half its length rounded up.
func (inst NameLayoutE) FieldCount() (n int) {
	c := inst.Explanation()
	if c == nil {
		return
	}
	n = (len(c) + 1) / 2
	return
}

// FieldIndex reports the 1-based position of a named component within a name
// in this layout — the index arrayElement and splitByChar count in, so a
// generator emitting SQL over a split name can use it directly. present is
// false for a component the layout does not carry.
//
// This is the accessor a code generator reads instead of re-deriving offsets
// from the component list, so that a component added to either arity reaches
// every generator at once (ADR-0226 §SD1).
func (inst NameLayoutE) FieldIndex(component string) (index int, present bool) {
	i := slices.Index(inst.Explanation(), component)
	if i < 0 {
		return
	}
	index = i/2 + 1
	present = true
	return
}

// DefaultSeparator is the physical-name component separator every in-tree
// leeway table is composed with, and the only one the generated SQL surface
// decodes (ADR-0226 §SD5). A name composed with another separator parses
// here — NewHumanReadableNamingConvention takes one — but decodes as foreign
// in SQL.
const DefaultSeparator = ":"

// PlainSectionName is the section a plain/backbone column reports under —
// the handle-side spelling of its item type (ADR-0116). It is what
// BuildLabels renders and what the handle resolver accepts, so the six
// backbone sections have one spelling rather than one per consumer.
//
// The empty string means an item type that names no section: PlainItemTypeNone
// is the tagged case, and an unmapped value is not addressable by handle.
func PlainSectionName(plainItemType common.PlainItemTypeE) (name string) {
	switch plainItemType {
	case common.PlainItemTypeEntityId:
		name = "id"
	case common.PlainItemTypeEntityTimestamp:
		name = "timestamp"
	case common.PlainItemTypeEntityRouting:
		name = "routing"
	case common.PlainItemTypeEntityLifecycle:
		name = "lifecycle"
	case common.PlainItemTypeTransaction:
		name = "transaction"
	case common.PlainItemTypeOpaque:
		name = "opaque"
	}
	return
}

// PlainItemTypePrefix is the total form of the prefix mapping: it reports the
// name prefix an item type composes under, and ok=false for one that composes
// none. PlainItemTypeNone is the tagged case and reports TaggedValuePrefix.
func PlainItemTypePrefix(plainItemType common.PlainItemTypeE) (prefix string, ok bool) {
	switch plainItemType {
	case common.PlainItemTypeNone:
		return TaggedValuePrefix, true
	case common.PlainItemTypeEntityId:
		return IdPrefix, true
	case common.PlainItemTypeEntityTimestamp:
		return TimestampPrefix, true
	case common.PlainItemTypeEntityLifecycle:
		return LifecyclePrefix, true
	case common.PlainItemTypeEntityRouting:
		return RoutingPrefix, true
	case common.PlainItemTypeTransaction:
		return TransactionPrefix, true
	case common.PlainItemTypeOpaque:
		return OpaquePrefix, true
	}
	return
}
