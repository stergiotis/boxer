package lwsql

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
)

// lwsql_lanes.go answers the question an extraction asks of a schema
// (ADR-0181 §SD3): given a table and a section, which physical column
// carries the values, which carries the membership identities, and which
// carries the counts.
//
// Everything here is decided from the physical column NAMES alone — the
// naming convention parses them, and role, section and shape all fall out.
// No IR, no TableDesc round-trip, no server round-trip beyond the column
// list the Resolver already fetches.

// ExtractLanes are one section's extraction lanes on one table, with the
// physical names as the schema spells them (unquoted — the caller quotes
// when splicing).
type ExtractLanes struct {
	// Section is the section name as authored.
	Section string
	// Values are the section's value columns, in schema order.
	Values []ValueColumn
	// Channels are the membership channels this section actually carries,
	// in a fixed order. A section usually has exactly one; more than one
	// means the caller must say which it means.
	Channels []Channel
}

// ValueColumn is one value lane and everything an extraction needs to read
// it.
//
// Shape and Length are per COLUMN, not per section, and that is not a
// refinement — a section may legitimately hold a scalar, an array and a set
// side by side, in which case it carries BOTH a `len` and a `card` support
// column and no single section-level answer is right. Pairing a set's
// flattened lane with an array's per-attribute counts reads the wrong rows
// and raises no error, so the pairing is made here, once, from the same
// scalar modifier the write path bucketed the column by.
type ValueColumn struct {
	// Name is the sub-column name as authored, e.g. "pointLat".
	Name string
	// Physical is the physical column name.
	Physical string
	// Shape says how this column stores one attribute's value.
	Shape lwextract.ShapeE
	// Length is this column's per-attribute element-count lane — the
	// section's `len` for an array, its `card` for a set. Empty for a
	// scalar, where there is nothing to partition.
	Length string
}

// Channel is one membership channel a section carries.
type Channel struct {
	// Name is the channel token a caller spells to disambiguate, matching
	// the constructor family's channel vocabulary (ADR-0181 §SD2).
	Name string
	// Ident is the physical membership identity column.
	Ident string
	// Card is the physical per-attribute membership cardinality column, or
	// empty when this table's column listing does not carry one.
	//
	// Empty here means "not in the listing I was given" and NOTHING more.
	// It is deliberately NOT forwarded to lwextract.Lanes.Card, whose empty
	// value means the stronger "the schema proves one membership per
	// attribute" and licenses a form that reads a membership position as an
	// attribute index. A listing can be short for reasons that are not
	// proof — a view projecting a subset, a role token this build cannot
	// parse — so a caller that needs the lane must refuse when it is
	// missing, the way the read-back generator does.
	Card string
	// Verbatim says how a membership is spelled: verbatim channels carry
	// the name itself, ref channels carry a registry id.
	Verbatim bool
	// SingleMembership says the schema DECLARES this channel
	// single-instance — every attribute carries exactly one membership
	// (ADR-0213), recovered from the use-aspects the physical column names
	// encode. It is what turns an empty Card from "not in the listing" into
	// the proof lwextract's fast form requires: a caller may forward
	// Card == "" to lwextract exactly when this is set.
	SingleMembership bool
	// Param is the physical high-cardinality parameter lane of a MIXED
	// channel, co-indexed with Ident and counted by the same Card. Empty on
	// the four simple channels, which have no such lane — so its presence is
	// also how a caller tells the two apart.
	Param string
}

// extractChannels is the channel vocabulary, in the order Channels reports
// them.
//
// Names follow common.MembershipSpecE.String(), which is the spelling
// lwsql.ParseMembershipSpec accepts for the constructor family — one channel
// vocabulary across authoring and extraction, at the price of a long token
// for the mixed pair.
//
// The PARAMETRIZED channels are still absent, and for a different reason
// than the mixed pair used to be: a parametrized membership is one opaque
// blob carrying identity and parameters together, with no separate identity
// lane to match and no shared codec saying how the blob is laid out. There
// is no literal a caller could supply, so it needs a serialization contract
// first, not a lane lookup (ADR-0008 Cut 2).
var extractChannels = []struct {
	name     string
	ident    common.ColumnRoleE
	card     common.ColumnRoleE
	param    common.ColumnRoleE
	spec     common.MembershipSpecE
	verbatim bool
}{
	{"low-card-ref", common.ColumnRoleLowCardRef, common.ColumnRoleLowCardRefCardinality, "", common.MembershipSpecLowCardRef, false},
	{"low-card-verbatim", common.ColumnRoleLowCardVerbatim, common.ColumnRoleLowCardVerbatimCardinality, "", common.MembershipSpecLowCardVerbatim, true},
	{"high-card-ref", common.ColumnRoleHighCardRef, common.ColumnRoleHighCardRefCardinality, "", common.MembershipSpecHighCardRef, false},
	{"high-card-verbatim", common.ColumnRoleHighCardVerbatim, common.ColumnRoleHighCardVerbatimCardinality, "", common.MembershipSpecHighCardVerbatim, true},
	{"low-card-ref-high-card-params", common.ColumnRoleMixedLowCardRef, common.ColumnRoleMixedLowCardRefCardinality, common.ColumnRoleMixedRefHighCardParameters, common.MembershipSpecMixedLowCardRefHighCardParameters, false},
	{"low-card-verbatim-high-card-params", common.ColumnRoleMixedLowCardVerbatim, common.ColumnRoleMixedLowCardVerbatimCardinality, common.ColumnRoleMixedVerbatimHighCardParameters, common.MembershipSpecMixedLowCardVerbatimHighCardParameters, true},
}

// ExtractLanesFor reports the section's lanes on the given table. ok is
// false when the table is not leeway-shaped or carries no such section —
// the two cases a caller reports differently, so Sections is there to say
// what the table does have.
func (inst *Resolver) ExtractLanesFor(dbName string, tableName string, section string) (lanes ExtractLanes, ok bool) {
	idx := inst.indexFor(dbName, tableName)
	if idx == nil {
		return
	}
	si, found := idx.sections[fold(section)]
	if !found {
		return
	}
	lanes.Section = si.display
	lanes.Values = make([]ValueColumn, 0, len(si.valueCols))
	for _, c := range si.valueCols {
		vc := ValueColumn{Name: c.display, Physical: c.physical, Shape: lwextract.ShapeScalar}
		// A homogenous array is partitioned by `len`, a set by `card`.
		// Both are the same question to an extraction — how many elements
		// does this attribute have — so one field carries whichever this
		// column's own modifier calls for.
		switch c.scalarModifier {
		case canonicaltypes.ScalarModifierHomogenousArray:
			vc.Shape = lwextract.ShapeList
			vc.Length = si.roles[common.ColumnRoleLength]
		case canonicaltypes.ScalarModifierSet:
			vc.Shape = lwextract.ShapeList
			vc.Length = si.roles[common.ColumnRoleCardinality]
		}
		lanes.Values = append(lanes.Values, vc)
	}
	for _, ch := range extractChannels {
		ident, hasIdent := si.roles[ch.ident]
		if !hasIdent {
			continue
		}
		c := Channel{
			Name:             ch.name,
			Ident:            ident,
			Card:             si.roles[ch.card], // absent alone is NOT proof — SingleMembership is (ADR-0213)
			Verbatim:         ch.verbatim,
			SingleMembership: si.single&ch.spec != 0,
		}
		if ch.param != "" {
			// Guarded rather than looked up unconditionally: the simple
			// channels carry ColumnRoleUnspecific here, which is the empty
			// role, and asking si.roles for it would match a column that
			// merely failed to declare one.
			c.Param = si.roles[ch.param]
		}
		lanes.Channels = append(lanes.Channels, c)
	}
	ok = true
	return
}

// Sections lists the sections a table carries, as authored — for an error
// that has to say what was searched.
func (inst *Resolver) Sections(dbName string, tableName string) (names []string) {
	idx := inst.indexFor(dbName, tableName)
	if idx == nil {
		return
	}
	names = make([]string, 0, len(idx.sections))
	for _, si := range idx.sections {
		names = append(names, si.display)
	}
	// Sorted: this feeds an error message, and map order would make the
	// same failure read differently on every run.
	slices.Sort(names)
	return
}

// ValueNames lists the value sub-columns as authored, in order — the
// candidate list an unknown sub-column error offers.
func (inst ExtractLanes) ValueNames() (names []string) {
	names = make([]string, 0, len(inst.Values))
	for _, v := range inst.Values {
		names = append(names, v.Name)
	}
	return
}

// ValueColumnFor returns the value column to read, or the section's only one
// when subColumn is empty.
//
// Defaulting is deliberately narrow: a section with several value columns
// has no obvious "the" value, so the caller is asked rather than guessed at.
// The conventional single-column spelling — `value` — resolves by name like
// any other, so the common case needs no argument either way.
func (inst ExtractLanes) ValueColumnFor(subColumn string) (col ValueColumn, err error) {
	if subColumn == "" {
		switch len(inst.Values) {
		case 0:
			err = eb.Build().Str("section", inst.Section).Errorf("section carries no value column")
		case 1:
			col = inst.Values[0]
		default:
			err = eb.Build().Str("section", inst.Section).Str("candidates", strings.Join(inst.ValueNames(), ", ")).
				Errorf("section has more than one value column; name the one to read with col:<%s>", strings.Join(inst.ValueNames(), "|"))
		}
		return
	}
	folded := fold(subColumn)
	for _, v := range inst.Values {
		if fold(v.Name) == folded {
			col = v
			return
		}
	}
	err = eb.Build().Str("section", inst.Section).Str("subColumn", subColumn).
		Str("candidates", strings.Join(inst.ValueNames(), ", ")).
		Errorf("no such value column in section; it has %s", strings.Join(inst.ValueNames(), ", "))
	return
}

// ChannelFor selects the membership channel to read. An empty name takes
// the section's only channel; a section carrying several is ambiguous and
// says so, listing what it has.
func (inst ExtractLanes) ChannelFor(name string) (ch Channel, err error) {
	if name == "" {
		switch len(inst.Channels) {
		case 0:
			err = eb.Build().Str("section", inst.Section).Errorf("section carries no membership channel this build can read (parametrized channels are out of scope, ADR-0181 §SD3)")
		case 1:
			ch = inst.Channels[0]
		default:
			err = eb.Build().Str("section", inst.Section).Str("candidates", strings.Join(inst.ChannelNames(), ", ")).
				Errorf("section carries more than one membership channel; name the one to read with chan:<%s>", strings.Join(inst.ChannelNames(), "|"))
		}
		return
	}
	folded := fold(name)
	for _, c := range inst.Channels {
		if fold(c.Name) == folded {
			ch = c
			return
		}
	}
	err = eb.Build().Str("section", inst.Section).Str("channel", name).Str("candidates", strings.Join(inst.ChannelNames(), ", ")).
		Errorf("section does not carry that membership channel; it carries %s", strings.Join(inst.ChannelNames(), ", "))
	return
}

// ChannelNames lists the channels the section carries, for diagnostics.
func (inst ExtractLanes) ChannelNames() (names []string) {
	names = make([]string, 0, len(inst.Channels))
	for _, c := range inst.Channels {
		names = append(names, c.Name)
	}
	return
}
