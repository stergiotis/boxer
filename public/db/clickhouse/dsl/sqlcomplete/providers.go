package sqlcomplete

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
)

// ItemsFn answers a closed domain. ready=false means "not known yet" — an
// ADR-0147 §SD6 probe that has not come back — and is deliberately distinct
// from an empty answer, which is a claim that the domain has no members. The
// engine says nothing either way, but says a different why (§SD1, and
// ADR-0174's `?`-never-`MISSING` rule).
type ItemsFn func() (items []Item, ready bool)

// ItemsOfFn answers a domain that depends on a sibling argument's value.
type ItemsOfFn func(of string) (items []Item, ready bool)

// Providers is the host's wiring, per buffer (ADR-0147 §SD7). A nil field is a
// domain this host cannot resolve; the engine reports that rather than
// offering nothing without a reason.
type Providers struct {
	ComponentKinds ItemsFn
	// ComponentType answers the whole named Tuple a kind projects.
	//
	// A type rather than a list of rows, because that is what composes: the
	// typer needs the element's own type for
	// `tupleElement(tupleElement(LW_COMPONENT(k),'a'),'b')`, and the field
	// list falls out of the type. A provider keyed on a kind and answering
	// rows would serve the LW_COMPONENT spelling and nothing else.
	ComponentType func(kind string) (t chtype.Type, ok bool)

	IntrospectionTables ItemsFn
	Sections            ItemsFn
	SectionColumns      ItemsOfFn
	ExtractionTokens    ItemsOfFn
	Memberships         ItemsFn
	Channels            ItemsFn
	SupportRoles        ItemsFn
	Aspects             ItemsFn
	CanonicalTypes      ItemsFn
	Glosses             ItemsFn
	GlossKeys           ItemsOfFn
	IdentityTags        ItemsFn
	StatementParams     ItemsFn
	// Expressions answers a free expression position — the columns of the
	// statement's single source, the functions the endpoint has, the names
	// this build's vocabulary declares. Its argument is that source, qualified,
	// or empty when the statement has none or more than one.
	//
	// It exists because "any expression" is not one catalogue: a column and a
	// function are both valid at a SELECT position, and a provider returning
	// only one of them would be exactly as wrong as returning neither.
	Expressions ItemsOfFn

	// Catalog is the server's own vocabulary for this buffer's endpoint —
	// every entry an ADR-0147 §SD6 probe. Empty until M2.
	Catalog Catalog
}

// Catalog is the endpoint-dependent half: what `system.*` answers for the
// buffer this engine serves. Routed per buffer because two buffers may point
// at different endpoints, and because ad-hoc datasets contribute tables no
// `system.tables` enumerates (ADR-0190 §SD12).
type Catalog struct {
	Databases ItemsFn
	// Tables answers a named database's tables, views and table functions, or
	// the buffer's default database when the argument is empty.
	Tables ItemsOfFn
	// Columns answers a table's columns, or the statement's own sources' when
	// the argument is empty.
	Columns ItemsOfFn
	// ColumnType answers one column's type, for the typer. table may be
	// empty, meaning the buffer's single source.
	ColumnType func(table string, column string) (t chtype.Type, ok bool)

	Functions            ItemsFn
	Settings             ItemsFn
	TypeNames            ItemsFn
	TimeZones            ItemsFn
	Dictionaries         ItemsFn
	DictionaryAttributes ItemsOfFn
	Formats              ItemsFn
	// EnumValues answers a column's Enum members, derived from its type
	// string (§SD12 B11).
	EnumValues ItemsOfFn
}

// resolve turns a domain into candidates.
//
// It returns three states, and the difference between the last two is the
// point: items with ready=true is an answer; ready=false is a probe that has
// not come back; wired=false is a host that cannot answer this domain at all.
func (inst *Providers) resolve(d sqlvocab.Domain, of string) (items []Item, ready bool, wired bool) {
	call := func(f ItemsFn) ([]Item, bool, bool) {
		if f == nil {
			return nil, false, false
		}
		it, ok := f()
		return it, ok, true
	}
	callOf := func(f ItemsOfFn) ([]Item, bool, bool) {
		if f == nil {
			return nil, false, false
		}
		it, ok := f(of)
		return it, ok, true
	}

	switch d.Kind {
	case sqlvocab.DomainExpr:
		if inst.Expressions != nil {
			return callOf(inst.Expressions)
		}
		// Without a host answer, the columns are the part a catalogue can
		// enumerate exactly.
		return callOf(inst.Catalog.Columns)
	case sqlvocab.DomainComponentKind:
		return call(inst.ComponentKinds)
	case sqlvocab.DomainDatabase:
		return call(inst.Catalog.Databases)
	case sqlvocab.DomainTable:
		return callOf(inst.Catalog.Tables)
	case sqlvocab.DomainColumn:
		return callOf(inst.Catalog.Columns)
	case sqlvocab.DomainIntrospectionTable:
		return call(inst.IntrospectionTables)
	case sqlvocab.DomainSection:
		return call(inst.Sections)
	case sqlvocab.DomainSectionColumn:
		return callOf(inst.SectionColumns)
	case sqlvocab.DomainExtractionToken:
		return callOf(inst.ExtractionTokens)
	case sqlvocab.DomainMembership:
		return call(inst.Memberships)
	case sqlvocab.DomainChannel:
		return call(inst.Channels)
	case sqlvocab.DomainSupportRole:
		return call(inst.SupportRoles)
	case sqlvocab.DomainAspect:
		return call(inst.Aspects)
	case sqlvocab.DomainCanonicalType:
		return call(inst.CanonicalTypes)
	case sqlvocab.DomainGloss:
		return call(inst.Glosses)
	case sqlvocab.DomainGlossKey:
		return callOf(inst.GlossKeys)
	case sqlvocab.DomainIdentityTag:
		return call(inst.IdentityTags)
	case sqlvocab.DomainStatementParam:
		return call(inst.StatementParams)
	case sqlvocab.DomainTypeName:
		return call(inst.Catalog.TypeNames)
	case sqlvocab.DomainTimeZone:
		return call(inst.Catalog.TimeZones)
	case sqlvocab.DomainSetting:
		return call(inst.Catalog.Settings)
	case sqlvocab.DomainDictionary:
		return call(inst.Catalog.Dictionaries)
	case sqlvocab.DomainDictionaryAttribute:
		return callOf(inst.Catalog.DictionaryAttributes)
	case sqlvocab.DomainFormat:
		return call(inst.Catalog.Formats)
	}
	return
}
