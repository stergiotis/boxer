// Package lwextract builds the ClickHouse expression that locates one
// attribute in a leeway tagged section and extracts its value (ADR-0181
// §SD3).
//
// It is the one place that knows the shape, so the two consumers cannot
// drift: the read-back generator (ADR-0066), which resolves lanes from a
// mappingplan and an IR, and LwExtractExpand, which resolves them from a
// table's physical column names. Both arrive with the same four facts — a
// value lane, a membership identity lane, the cardinality lane, and a
// resolved membership literal — and want the same SQL out.
//
// Nothing here resolves anything. Lanes are physical column names the caller
// has already found and escaped, and Membership is a SQL literal the caller
// has already rendered. That keeps this package free of the schema, the
// registry, and the naming convention, which is what lets both consumers
// import it.
//
// # The two forms
//
// The general form calls the read-back helper family (ADR-0066), which
// handles the doubly-flattened layout: an attribute may carry several
// memberships, so the flattened membership array is longer than the value
// array and a position in one does not index the other.
//
//	LW_VALUE_BY_TAG_EQUAL(val, ident, lit, LW_RAGGED_PARENT_IDS(card))
//
// The fast form is licensed when the section's membership cardinality lane
// does not exist. Its absence is not a missing column — it is the schema
// stating that every attribute carries exactly one membership on this
// channel, so the flattened membership array IS the value array and
// LW_RAGGED_PARENT_IDS would be the identity permutation:
//
//	val[indexOf(ident, lit)]
//
// This is ADR-0066's open fast-path-detection item, closed structurally: the
// decision needs the column list and nothing else, so it cannot disagree
// with the data.
package lwextract

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ShapeE says how the value lane stores one attribute's value.
type ShapeE uint8

const (
	// ShapeScalar is one value per attribute — the value lane is indexed
	// directly by attribute position.
	ShapeScalar ShapeE = iota + 1
	// ShapeList is a homogenous array or a set: the value lane is flattened
	// across attributes and partitioned by Lanes.Length. Both extract
	// identically; what differs is only which support column carries the
	// per-attribute element count, which is the caller's business.
	ShapeList
)

// String renders the shape for error messages.
func (inst ShapeE) String() (s string) {
	switch inst {
	case ShapeScalar:
		s = "scalar"
	case ShapeList:
		s = "list"
	default:
		s = "unknown"
	}
	return
}

// Lanes are the physical columns one extraction reads, already escaped for
// SQL by the caller.
type Lanes struct {
	// Value is the value column: one element per attribute for ShapeScalar,
	// the flattened element array for ShapeList.
	Value string
	// Ident is the membership identity column for the channel being read,
	// flattened across attributes.
	Ident string
	// Card is the per-attribute membership cardinality column. EMPTY IS
	// MEANINGFUL: it says the section has no such column, which proves one
	// membership per attribute and licenses the fast form. Do not pass a
	// placeholder for a column you have not looked for.
	Card string
	// Length is the per-attribute element count (the len or card support
	// column, per shape). Required for ShapeList, ignored for ShapeScalar.
	Length string
}

// Request is one extraction.
type Request struct {
	Lanes Lanes
	Shape ShapeE
	// Membership is the resolved membership literal — an escaped string for
	// a verbatim channel, a decimal id for a ref channel. Rendering it is
	// the caller's job, because only the caller knows the registry.
	Membership string
	// Unit indexes the located list to its single element, for a container
	// column authored as carrying exactly one element per attribute. Ignored
	// for ShapeScalar, where the located value is already that element.
	Unit bool
}

// Value renders the locate-and-extract expression.
//
// Absent membership yields the type default — an empty string, a zero, an
// empty array — never an error and never NULL. A caller that must tell an
// absent attribute from one present with the default wraps this in
// NullWhenAbsent, which is what LW_GET_NULL is.
func Value(req Request) (expr string, err error) {
	err = req.validate()
	if err != nil {
		return
	}
	l := req.Lanes
	switch {
	case req.Shape == ShapeScalar && l.Card == "":
		expr = l.Value + "[indexOf(" + l.Ident + ", " + req.Membership + ")]"
	case req.Shape == ShapeScalar:
		expr = "LW_VALUE_BY_TAG_EQUAL(" + l.Value + ", " + l.Ident + ", " + req.Membership + ", " + parentIds(l.Card) + ")"
	case l.Card == "":
		// One membership per attribute makes the membership index the
		// attribute index, so the flattened value array is sliced by the
		// length lane at that position directly. LW_LIST_BY_TAG_EQUAL is
		// exactly this with the attribute index resolved the general way.
		at := "indexOf(" + l.Ident + ", " + req.Membership + ")"
		expr = "arraySlice(" + l.Value + ", LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL(" + l.Length + ")[" + at + "], " + l.Length + "[" + at + "])"
	default:
		expr = "LW_LIST_BY_TAG_EQUAL(" + l.Value + ", " + l.Length + ", " + l.Ident + ", " + req.Membership + ", " + parentIds(l.Card) + ")"
	}
	if req.Unit && req.Shape == ShapeList {
		expr += "[1]"
	}
	return
}

// Present renders the necessary condition that the attribute exists: the
// membership occurs in the identity lane.
//
// This is the cheap, index-prunable half of the read surface — has() over an
// Array column prunes granules through a bloom_filter skip index, which
// indexOf and countEqual cannot (ADR-0066's 2026-06-09 Update).
func Present(lanes Lanes, membership string) (expr string) {
	expr = "has(" + lanes.Ident + ", " + membership + ")"
	return
}

// CountEqual renders how many times the membership occurs — the exactness
// half, for a validator that pins "exactly once" or "at most once".
func CountEqual(lanes Lanes, membership string) (expr string) {
	expr = "countEqual(" + lanes.Ident + ", " + membership + ")"
	return
}

// NullWhenAbsent wraps a value expression so an absent attribute reads as
// NULL rather than as the type default.
//
// Only legal over a scalar-projecting expression: ClickHouse forbids
// Nullable(Array(...)), so a list keeps the empty-array sentinel — which for
// a container is not a limitation, absent and present-empty being the same
// thing on the write side (ADR-0066 decision 4).
func NullWhenAbsent(expr string, lanes Lanes, membership string) (out string) {
	out = "if(" + Present(lanes, membership) + ", " + expr + ", NULL)"
	return
}

// parentIds is the flattened-membership-position → attribute-index map, the
// pack function the general form threads through every helper call.
func parentIds(card string) (expr string) {
	expr = "LW_RAGGED_PARENT_IDS(" + card + ")"
	return
}

func (inst Request) validate() (err error) {
	if inst.Lanes.Value == "" {
		err = eb.Build().Errorf("no value lane")
		return
	}
	if inst.Lanes.Ident == "" {
		err = eb.Build().Errorf("no membership identity lane")
		return
	}
	if inst.Membership == "" {
		err = eb.Build().Errorf("no membership literal")
		return
	}
	switch inst.Shape {
	case ShapeScalar:
	case ShapeList:
		if inst.Lanes.Length == "" {
			err = eb.Build().Errorf("a list extraction needs the per-attribute element-count lane")
			return
		}
	default:
		err = eb.Build().Stringer("shape", inst.Shape).Errorf("unsupported shape")
		return
	}
	return
}
