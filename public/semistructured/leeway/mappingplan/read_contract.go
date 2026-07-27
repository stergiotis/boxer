package mappingplan

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The read contract (ADR-0146 D1): what a row must look like for a decode of
// one kind to be sound, derived from the Plan and nothing else.
//
// The write path already fixes the answer. Every non-tuple field emits AT MOST
// ONE attribute per (section, membership) per row — a container field emits one
// attribute carrying N values, not N attributes — and a tuple section emits one
// attribute per element. What was missing is a statement of that guarantee the
// read paths can enforce, instead of each re-deriving it from Go shape and
// arriving somewhere different. DeriveReadContract is that statement.

// ArityUnbounded is the Slot.MaxAttrs value for a slot with no upper bound —
// a tuple-family section, whose attribute count is the element count.
const ArityUnbounded = -1

// PresenceE is the three-valued detection verdict for one kind on one row
// (ADR-0075's vocabulary, ADR-0146 D2).
//
// The guarantee is one-sided: PresenceExact implies PresenceApproximate would
// also have held, never the reverse. A cheap approximate check can therefore
// prefilter without false negatives, which is the property ADR-0066's
// Presence artefact relies on.
type PresenceE uint8

const (
	// PresenceAbsent — the row does not carry the kind: nothing the kind
	// claims is populated, or a slot the kind requires is missing. Absent is
	// a decision, not a failure; a partially populated row is Absent because
	// no sound decode exists for it.
	PresenceAbsent PresenceE = iota
	// PresenceApproximate — every required slot is populated (ADR-0066's
	// Presence holds), but at least one slot's attribute count falls outside
	// the arity the Plan declares (its Validator does not). The kind is
	// recognisably there and cannot be decoded soundly.
	PresenceApproximate
	// PresenceExact — Presence and Validator both hold. The row conforms;
	// a decode is sound. Decided from attribute counts alone, without
	// reading any value.
	PresenceExact
)

func (p PresenceE) String() string {
	switch p {
	case PresenceAbsent:
		return "absent"
	case PresenceApproximate:
		return "approximate"
	case PresenceExact:
		return "exact"
	}
	return "unknown"
}

// Slot is one (section, membership) attribute slot a kind claims, with the
// number of attributes the writer guarantees for it.
//
// The arity is measured from the write path, not assumed:
//
//	scalar T, `,unit` scalar, const .............. exactly 1
//	option.Option[T] ............................. 0 or 1 (absent splices)
//	[]T, *roaring.Bitmap ......................... 0 or 1 (empty splices)
//	multi-sub-column, ≥1 scalar sub-column ....... exactly 1
//	multi-sub-column, all containers, all empty .. 0 or 1 (the S=0 splice)
//	tuple/nested Many ............................ 0..unbounded
//	tuple/nested One, ≥1 scalar sub-column ....... exactly 1
//	tuple/nested One (all-container) / Optional .. 0 or 1
//
// A container's N values ride inside ONE attribute; N is not an attribute
// count and does not appear here.
type Slot struct {
	// Section is the lw: section name the slot lives in.
	Section string
	// Membership is the lw: membership name that locates the attribute within
	// the section. Empty when OwnsSection is true — a dynamic-membership tuple
	// carries its memberships per element, so no static name identifies them.
	Membership string
	// Channel is the slot's membership channel, uniform per section
	// (ADR-0072's one coupling), so a reader resolves one accessor per section.
	Channel MembershipChannel
	// MinAttrs / MaxAttrs bound the attributes the writer emits for this slot
	// on one row. MaxAttrs is ArityUnbounded for a tuple-family section.
	MinAttrs int
	MaxAttrs int
	// OwnsSection marks a slot whose section belongs to it exclusively — a
	// tuple-family section, which PlanBuilder.Finish already forbids any other
	// field from targeting. Every attribute in the section belongs to this
	// slot, so a reader counts attributes rather than matching memberships.
	OwnsSection bool
	// GoFields names the DTO fields projecting from this slot, in Plan order.
	// Diagnostics only — a slot is identified by (Section, Membership).
	GoFields []string
}

// Required reports whether the slot must be populated for the kind to be
// present on a row. It is the Presence half of the contract; MaxAttrs is the
// Validator half.
func (s Slot) Required() bool { return s.MinAttrs > 0 }

// Admits reports whether n attributes satisfy the slot's arity.
func (s Slot) Admits(n int) bool {
	if n < s.MinAttrs {
		return false
	}
	return s.MaxAttrs == ArityUnbounded || n <= s.MaxAttrs
}

// ReadContract is a kind's slots — the whole of what a reader must check
// before decoding, and the single derivation every back-end shares. The Go
// codecs enforce it directly; the ClickHouse read-back generator's Presence /
// Validator artefacts express the same thing as SQL.
type ReadContract struct {
	Kind  string
	Slots []Slot
}

// Slot returns the slot for a (section, membership) pair. A section owned by a
// tuple matches on section alone, whatever membership is asked for.
func (c ReadContract) Slot(section, membership string) (Slot, bool) {
	for _, s := range c.Slots {
		if s.Section != section {
			continue
		}
		if s.OwnsSection || s.Membership == membership {
			return s, true
		}
	}
	return Slot{}, false
}

// Sections returns the distinct sections the contract claims, in slot order.
func (c ReadContract) Sections() (out []string) {
	seen := map[string]bool{}
	for _, s := range c.Slots {
		if seen[s.Section] {
			continue
		}
		seen[s.Section] = true
		out = append(out, s.Section)
	}
	return
}

// Verdict maps per-slot attribute counts to a PresenceE. counts is keyed by
// the slot index in c.Slots; a missing key counts as zero.
//
// Absent when nothing is populated, or when a required slot is missing —
// ADR-0066's Presence is a necessary condition, so a row failing it does not
// carry the kind. Approximate when Presence holds but some slot's count falls
// outside its arity. Exact when both hold.
func (c ReadContract) Verdict(counts map[int]int) PresenceE {
	populated := false
	presence := true
	validator := true
	for i, s := range c.Slots {
		n := counts[i]
		if n > 0 {
			populated = true
		}
		if s.Required() && n == 0 {
			presence = false
		}
		if !s.Admits(n) {
			validator = false
		}
	}
	if !populated || !presence {
		return PresenceAbsent
	}
	if !validator {
		return PresenceApproximate
	}
	return PresenceExact
}

// DeriveReadContract computes the read contract for a Plan. It reads only what
// the Plan already carries; there is no second source of truth to keep in step.
//
// Plans that PlanBuilder.Finish accepts always derive; the error return covers
// a hand-built Plan whose fields disagree about a section's shape.
func DeriveReadContract(plan *Plan) (c ReadContract, err error) {
	if plan == nil {
		err = eb.Build().Errorf("nil plan")
		return
	}
	c.Kind = plan.KindName

	// Section shape, in first-seen order. A section's arity depends on its
	// sub-column count and on whether any sub-column is scalar-shaped, so both
	// are collected before any slot is emitted.
	type sectionInfo struct {
		columns   map[string]bool
		hasScalar bool
		tuple     bool
		tupleCard AttrCardinalityE
		tupleName string
		dynamic   bool // tuple with per-element memberships
	}
	order := []string{}
	info := map[string]*sectionInfo{}
	for i := range plan.Fields {
		f := &plan.Fields[i]
		si := info[f.LWSection]
		if si == nil {
			si = &sectionInfo{columns: map[string]bool{}}
			info[f.LWSection] = si
			order = append(order, f.LWSection)
		}
		col := f.LWColumn
		if col == "" {
			col = "value"
		}
		si.columns[col] = true
		// Scalar-shaped iff not a container. This mirrors goplan.ClassifyBegin:
		// ShapeScalarBegin and ShapeScalarBeginSingle are both scalar, and only
		// IsMulti() selects ShapeContainer. TestClassifyBeginAgreesWithArity in
		// goplan pins the two together.
		if !f.IsMulti() {
			si.hasScalar = true
		}
		if f.TupleField != "" {
			si.tuple = true
			si.tupleCard = f.TupleCardinality
			si.tupleName = f.TupleField
			if len(f.TupleMemberships) > 0 {
				si.dynamic = true
			}
		}
	}

	for _, section := range order {
		si := info[section]
		var slots []Slot
		slots, err = deriveSectionSlots(plan, section, si.tuple, si.dynamic, si.tupleCard, si.tupleName, len(si.columns) > 1, si.hasScalar)
		if err != nil {
			return
		}
		c.Slots = append(c.Slots, slots...)
	}
	return
}

// deriveSectionSlots emits the slots for one section. A tuple-family section
// yields exactly one slot; a flat section yields one per distinct membership.
func deriveSectionSlots(plan *Plan, section string, isTuple, isDynamic bool, card AttrCardinalityE, tupleName string, multiSubColumn, hasScalar bool) (out []Slot, err error) {
	fieldsOf := func(pred func(*TaggedField) bool) (names []string, ch MembershipChannel, found bool) {
		for i := range plan.Fields {
			f := &plan.Fields[i]
			if f.LWSection != section || !pred(f) {
				continue
			}
			if !found {
				ch = f.Flags.Channel
				found = true
			}
			if f.GoFieldName != "" {
				names = append(names, f.GoFieldName)
			}
		}
		return
	}

	if isTuple {
		_, ch, _ := fieldsOf(func(*TaggedField) bool { return true })
		// The tuple's slot projects through the ONE outer DTO field; the
		// per-element sub-column fields are not row-level projections.
		s := Slot{
			Section:     section,
			Channel:     ch,
			OwnsSection: true,
			GoFields:    []string{tupleName},
		}
		switch card {
		case AttrCardinalityOne:
			// One attribute per row — unless every sub-column is a container
			// and all are empty, which splices to zero (the S=0 rule).
			s.MinAttrs, s.MaxAttrs = 1, 1
			if !hasScalar {
				s.MinAttrs = 0
			}
		case AttrCardinalityOptional:
			s.MinAttrs, s.MaxAttrs = 0, 1
		default: // Many — one attribute per element.
			s.MinAttrs, s.MaxAttrs = 0, ArityUnbounded
		}
		if !isDynamic {
			// A STATIC nested section carries one membership from the section
			// tag, resolved like a flat section; it still owns the section.
			for i := range plan.Fields {
				if plan.Fields[i].LWSection == section {
					s.Membership = plan.Fields[i].LWMembership
					break
				}
			}
		}
		out = append(out, s)
		return
	}

	// Flat section: one slot per distinct membership, in first-seen order.
	seen := map[string]bool{}
	for i := range plan.Fields {
		f := &plan.Fields[i]
		if f.LWSection != section || seen[f.LWMembership] {
			continue
		}
		seen[f.LWMembership] = true
		memb := f.LWMembership
		names, ch, _ := fieldsOf(func(g *TaggedField) bool { return g.LWMembership == memb })
		s := Slot{
			Section:    section,
			Membership: memb,
			Channel:    ch,
			MinAttrs:   1,
			MaxAttrs:   1,
			GoFields:   names,
		}
		if multiSubColumn {
			// One tuple attribute per row. PlanBuilder.Finish already forbids a
			// second membership here, so this slot speaks for the section. With
			// no scalar sub-column an all-empty row splices the attribute away.
			if !hasScalar {
				s.MinAttrs = 0
			}
			out = append(out, s)
			continue
		}
		// Single sub-column: the shape of the fields on this membership decides.
		// A membership carrying only splice-able shapes (Option, container) may
		// legitimately produce zero attributes.
		if !membershipAlwaysEmits(plan, section, memb) {
			s.MinAttrs = 0
		}
		out = append(out, s)
	}
	return
}

// membershipAlwaysEmits reports whether every field on a (section, membership)
// emits an attribute on every row. A mandatory scalar and a const always do; an
// Option with Has=false and an empty container / roaring splice to nothing.
//
// When several fields share one membership they each emit their own attribute,
// so the slot's arity would exceed one — a shape PlanBuilder.Finish does not
// currently produce (its uniqueness key rejects two fields on one membership+
// column). Should it become reachable, the conservative answer here is still
// correct for Presence; ADR-0146 D5 is where the multi-claim case is settled.
func membershipAlwaysEmits(plan *Plan, section, membership string) bool {
	any := false
	for i := range plan.Fields {
		f := &plan.Fields[i]
		if f.LWSection != section || f.LWMembership != membership {
			continue
		}
		any = true
		if f.IsConst {
			continue
		}
		if f.IsOption || f.IsMulti() {
			return false
		}
	}
	return any
}

// String renders the contract as one line per slot, for diagnostics and
// golden tests.
func (c ReadContract) String() string {
	var b strings.Builder
	b.WriteString(c.Kind)
	b.WriteString(":\n")
	for _, s := range c.Slots {
		b.WriteString("  ")
		b.WriteString(s.Section)
		if s.OwnsSection {
			b.WriteString(" [owns]")
		}
		if s.Membership != "" {
			b.WriteString("@")
			b.WriteString(s.Membership)
		}
		b.WriteString(" ")
		b.WriteString(arityString(s))
		if len(s.GoFields) > 0 {
			b.WriteString(" <- ")
			b.WriteString(strings.Join(s.GoFields, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func arityString(s Slot) string {
	upper := "*"
	if s.MaxAttrs != ArityUnbounded {
		upper = strconv.Itoa(s.MaxAttrs)
	}
	return "[" + strconv.Itoa(s.MinAttrs) + ".." + upper + "]"
}
