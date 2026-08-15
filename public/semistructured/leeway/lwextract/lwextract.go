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
//
// # Mixed channels
//
// A mixed channel spends a second lane on the high-cardinality half of the
// locator: Lanes.Param, co-indexed 1:1 with Lanes.Ident and counted by the
// same Lanes.Card. Locating then matches the PAIR, which ClickHouse does
// with a two-array arrayFirstIndex where a simple channel uses indexOf.
//
// On such a channel the identity alone is not an identifier — the parameter
// exists precisely because many attributes share the identity — so Value and
// ValueCount REQUIRE ParamsGiven and refuse without it. The plural question
// ("every attribute carrying this identity") is well posed without a
// parameter and is what Selection answers.
//
// # Selection
//
// Selection and SelectionAttrs return index selectors rather than values,
// which is the "argwhere + gather" plan the array-idioms how-to describes:
// select positions once, then project any number of lanes through the same
// selector with LW_CO_GATHER. The two are co-indexed with each other by
// construction, so they pass as two arguments to one lambda and stay
// aligned.
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
	// Param is the high-cardinality parameter column of a MIXED channel,
	// flattened alongside Ident and counted by the same Card. Empty on the
	// simple channels, which have no such lane.
	//
	// Its presence is what makes the locator a pair rather than a single
	// value, so it also decides which form every builder here renders.
	Param string
}

// Request is one extraction.
type Request struct {
	Lanes Lanes
	Shape ShapeE
	// Membership is the resolved membership literal — an escaped string for
	// a verbatim channel, a decimal id for a ref channel. Rendering it is
	// the caller's job, because only the caller knows the registry.
	Membership string
	// Params is the resolved parameter literal for a mixed channel — an
	// escaped string, because both parameter lanes are byte columns
	// whatever their identity lane carries.
	Params string
	// ParamsGiven says whether Params was supplied. It is NOT derivable from
	// Params being empty: the empty blob is a real parameter value — what an
	// attribute carries when no array index was elided into the channel — so
	// conflating the two would silently turn "I did not say" into "I said
	// the empty tuple".
	ParamsGiven bool
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
	case l.Param != "":
		// A mixed channel locates by the PAIR, which no helper in the
		// read-back family takes — its signatures predate the second lane.
		// The attribute index is resolved here and the value read off it,
		// which is what LW_VALUE_BY_TAG_EQUAL and LW_LIST_BY_TAG_EQUAL do
		// internally for the single-lane case.
		at := attrOf(req)
		if req.Shape == ShapeScalar {
			expr = l.Value + "[" + at + "]"
			break
		}
		expr = "arraySlice(" + l.Value + ", LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL(" + l.Length + ")[" + at + "], " + l.Length + "[" + at + "])"
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

// Selection renders the membership-lane positions the membership occupies —
// the "argwhere" half of the argwhere-and-gather plan, whose "gather" half
// is the pack's LW_CO_GATHER.
//
// It answers the PLURAL question, so a mixed channel's parameter is optional
// here where Value requires it: "every position carrying this identity" is
// well posed without one, and supplying one narrows the selection to the
// pair. An absent membership selects nothing, and every consumer of an empty
// selector — arrayMap, arrayFilter, LW_CO_GATHER, length — answers empty
// without a special case.
//
// Only the identity lane is read, so this needs neither a value lane nor the
// cardinality lane, and Request.Shape, Value, Length and Unit are ignored.
func Selection(req Request) (expr string, err error) {
	err = req.validateLocator()
	if err != nil {
		return
	}
	l := req.Lanes
	if l.Param != "" && req.ParamsGiven {
		expr = "arrayFilter((i, m, p) -> m = " + req.Membership + " AND p = " + req.Params +
			", arrayEnumerate(" + l.Ident + "), " + l.Ident + ", " + l.Param + ")"
		return
	}
	expr = "arrayFilter((i, m) -> m = " + req.Membership + ", arrayEnumerate(" + l.Ident + "), " + l.Ident + ")"
	return
}

// SelectionAttrs renders the ATTRIBUTE indices the membership occupies, in
// the same order and co-indexed element-for-element with Selection — so the
// two pass as two arguments to one lambda and stay aligned, which is what
// lets a caller read a membership-axis lane and an attribute-axis lane in
// one expression.
//
// Repeats are deliberate. An attribute carrying the membership twice appears
// twice, because deduplicating would break that correspondence — and on a
// mixed channel, twice under different parameters is the normal case.
//
// An empty Lanes.Card carries its usual meaning: one membership per
// attribute, so the position IS the attribute index and the map is dropped.
// A caller whose column listing merely lacks the lane must refuse rather
// than reach this, exactly as for Value.
func SelectionAttrs(req Request) (expr string, err error) {
	sel, err := Selection(req)
	if err != nil {
		return
	}
	if req.Lanes.Card == "" {
		expr = sel
		return
	}
	expr = "LW_CO_GATHER(" + parentIds(req.Lanes.Card) + ", " + sel + ")"
	return
}

// attrOf renders the attribute index a mixed channel's locator resolves to,
// or 0 when the pair is absent — which ClickHouse answers with the type
// default on the read that follows, keeping Value's absence rule.
func attrOf(req Request) (expr string) {
	l := req.Lanes
	at := "arrayFirstIndex((m, p) -> m = " + req.Membership + " AND p = " + req.Params + ", " + l.Ident + ", " + l.Param + ")"
	if l.Card == "" {
		return at
	}
	return parentIds(l.Card) + "[" + at + "]"
}

// ValueCount renders how many elements the located list carries — the
// value-count half of the read surface, for a validator that pins how many
// a `,unit` slot may hold (ADR-0183 D5). An absent membership renders 0,
// following Value's type-default rule: locating nothing yields the empty
// list, whose length is zero.
//
// Unlike Value this uses ClickHouse BUILT-INS ONLY, because a validator term
// ends up inside the Filter a generated Scan embeds, which must run where the
// leeway helper pack is not installed (ADR-0100 S2). The count needs only the
// length lane at the owning attribute, never the values, so it can stay
// inside that budget where locating the list itself cannot.
//
// ShapeScalar carries one value per attribute by construction, so there is
// no such count to render and the request is rejected rather than answered
// with a constant. Request.Unit is ignored — the count is of the list, not
// of the element the unit shape indexes out of it.
func ValueCount(req Request) (expr string, err error) {
	if req.Shape != ShapeList {
		err = eb.Build().Stringer("shape", req.Shape).Errorf("a value count is only defined for a list extraction")
		return
	}
	req.Unit = false
	err = req.validate()
	if err != nil {
		return
	}
	l := req.Lanes
	at := "indexOf(" + l.Ident + ", " + req.Membership + ")"
	if l.Param != "" {
		at = "arrayFirstIndex((m, p) -> m = " + req.Membership + " AND p = " + req.Params + ", " + l.Ident + ", " + l.Param + ")"
	}
	if l.Card == "" {
		// One membership per attribute makes the membership index the
		// attribute index. An absent membership indexes at 0, which
		// ClickHouse answers with the type default — the zero this
		// function documents.
		expr = l.Length + "[" + at + "]"
		return
	}
	// The owning attribute of a flattened membership position is the first
	// whose cumulative membership count reaches it — LW_RAGGED_PARENT_IDS
	// read at that position, without materialising the expansion (an
	// attribute carrying no membership shares its predecessor's cumulative
	// count, so the FIRST index reaching the position always has one).
	// Absent, the position is 0 and the search answers 1, which is a real
	// attribute — so this form needs the presence guard the fast one gets
	// from ClickHouse's out-of-range default.
	owner := "arrayFirstIndex(cum -> cum >= " + at + ", arrayCumSum(" + l.Card + "))"
	expr = "if(" + PresentFor(req) + ", " + l.Length + "[" + owner + "], 0)"
	return
}

// Present renders the necessary condition that the attribute exists: the
// membership occurs in the identity lane.
//
// This is the cheap, index-prunable half of the read surface — has() over an
// Array column prunes granules through a bloom_filter skip index, which
// indexOf and countEqual cannot (ADR-0066's 2026-06-09 Update).
//
// This is the single-lane form. On a mixed channel it is a true but weaker
// condition — the identity occurs, at some parameter — so PresentFor is what
// callers that hold a parameter want.
func Present(lanes Lanes, membership string) (expr string) {
	expr = "has(" + lanes.Ident + ", " + membership + ")"
	return
}

// PresentFor is Present for a request that may carry a mixed channel's
// parameter: the pair occurs, at one position, in the two lanes together.
//
// The two has() conjuncts are not redundant with the arrayExists. They are
// the prunable part — a multi-lane lambda is opaque to index analysis, while
// each has() prunes granules on its own lane's skip index — and dropping
// them turns an indexed lookup into a full scan. This is the shape the pack
// already ships as LW_CO_EXISTS_EQ2, spelled inline so it stays available on
// a server provisioned before this existed.
func PresentFor(req Request) (expr string) {
	l := req.Lanes
	if l.Param == "" || !req.ParamsGiven {
		return Present(l, req.Membership)
	}
	expr = "has(" + l.Ident + ", " + req.Membership + ") AND has(" + l.Param + ", " + req.Params + ")" +
		" AND arrayExists((m, p) -> m = " + req.Membership + " AND p = " + req.Params + ", " + l.Ident + ", " + l.Param + ")"
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

// NullWhenAbsentFor is NullWhenAbsent for a request that may carry a mixed
// channel's parameter, so absence means the PAIR is absent.
//
// The distinction is not cosmetic: guarding a pair-located read with the
// identity alone reports "present" on a row that carries the identity under
// a different parameter, and then returns the type default — which is
// exactly the absent-versus-present-with-the-default confusion the NULL form
// exists to remove.
func NullWhenAbsentFor(req Request, expr string) (out string) {
	out = "if(" + PresentFor(req) + ", " + expr + ", NULL)"
	return
}

// parentIds is the flattened-membership-position → attribute-index map, the
// pack function the general form threads through every helper call.
func parentIds(card string) (expr string) {
	expr = "LW_RAGGED_PARENT_IDS(" + card + ")"
	return
}

// validateLocator checks what every builder here needs: an identity lane, a
// membership literal, and — on a mixed channel — a parameter literal that is
// either supplied or deliberately not.
func (inst Request) validateLocator() (err error) {
	if inst.Lanes.Ident == "" {
		err = eb.Build().Errorf("no membership identity lane")
		return
	}
	if inst.Membership == "" {
		err = eb.Build().Errorf("no membership literal")
		return
	}
	if inst.ParamsGiven && inst.Lanes.Param == "" {
		err = eb.Build().Errorf("a parameter was supplied but the channel has no parameter lane; only a mixed channel takes one")
		return
	}
	return
}

func (inst Request) validate() (err error) {
	err = inst.validateLocator()
	if err != nil {
		return
	}
	if inst.Lanes.Value == "" {
		err = eb.Build().Errorf("no value lane")
		return
	}
	// A mixed channel's identity is shared by design — the parameter lane
	// exists because several attributes carry the same identity — so a
	// singular read without one would locate an arbitrary member of that
	// set and report it as though it were the answer. Selection is the
	// plural question and takes no such requirement.
	if inst.Lanes.Param != "" && !inst.ParamsGiven {
		err = eb.Build().Errorf("a mixed channel identifies an attribute by identity AND parameter; supply a parameter, or select every attribute carrying the identity instead")
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
