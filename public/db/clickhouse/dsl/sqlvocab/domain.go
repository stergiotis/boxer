package sqlvocab

// DomainKindE names what may stand at an argument position (ADR-0190 §SD4).
//
// A kind is a question, not an answer: the resolver that turns
// [DomainComponentKind] into candidate spellings is wired by the host, per
// buffer. Adding a kind here is adding a member; what it means is fixed by the
// registry it reads, which the member's own comment names.
//
// The zero value is [DomainUnspecified] and [Registry.Register] refuses it — a
// roster author who leaves it unset gets an error rather than a parameter that
// silently offers nothing.
//
//codelint:enum-prefix=Domain
type DomainKindE uint8

const (
	// DomainUnspecified is the zero value: a parameter whose author has not
	// said what it takes. Refused at registration.
	DomainUnspecified DomainKindE = iota
	// DomainExpr is any expression. It is a real answer, not a fallback: a
	// column, a literal or a call may stand there, and the engine completes it
	// from the statement's own scope rather than from a closed list.
	DomainExpr
	// DomainComponentKind is a registered component kind —
	// `componentsql.Registry.Kinds()` (ADR-0189).
	DomainComponentKind
	// DomainElementOf is a named element of the tuple-typed sibling argument
	// at Ref. This is the one domain that needs the typer (ADR-0190 §SD5).
	DomainElementOf
	// DomainDatabase is a database on the buffer's endpoint.
	DomainDatabase
	// DomainTable is a table, view or table function of the database named at
	// the position this domain was resolved for, or of the endpoint's default.
	DomainTable
	// DomainColumn is a column of the table this position names, or of the
	// statement's own sources.
	DomainColumn
	// DomainIntrospectionTable is a table the introspection plane serves —
	// the `introspect` catalog, plus the ad-hoc dataset aliases bound in the
	// buffer's host (ADR-0094, ADR-0134 §SD4).
	DomainIntrospectionTable
	// DomainSection is a leeway section of the table in scope. Its resolver is
	// ADR-0147 §SD9's physical-schema reader.
	DomainSection
	// DomainSectionColumn is a value column of the section named at Ref,
	// spelled with its `col:` prefix.
	DomainSectionColumn
	// DomainExtractionToken is one of the leeway extraction family's trailing
	// prefixed tokens over the section at Ref: `col:<value column>`,
	// `chan:<membership channel>` or `param:<mixed-channel parameter>`.
	//
	// It is one domain rather than three because the surface accepts the
	// tokens in any order, so the ordinal does not say which one a position
	// holds — only the prefix the user has typed does. Declaring the three
	// per-ordinal would offer channels where a column belongs, which is the
	// one thing ADR-0190 §SD1 refuses.
	DomainExtractionToken
	// DomainMembership is a membership name, or the id a ref lane carries
	// (ADR-0171 §SD4).
	DomainMembership
	// DomainChannel is a membership channel — low-card-ref, low-card-verbatim,
	// high-card-ref — spelled with its `chan:` prefix where the surface takes
	// one.
	DomainChannel
	// DomainSupportRole is a section's support-column role: len, card, lrcard.
	DomainSupportRole
	// DomainAspect is an encoding, semantic, use or item aspect, spelled with
	// its `enc:` / `sem:` / `use:` / `item:` prefix.
	DomainAspect
	// DomainCanonicalType is a canonical type signature (`u64`, `s`, `f32h`).
	DomainCanonicalType
	// DomainGloss is a gloss name from the catalog (ADR-0186).
	DomainGloss
	// DomainGlossKey is a parameter key of the gloss named at Ref.
	DomainGlossKey
	// DomainIdentityTag is a registered identity tag value (ADR-0106).
	DomainIdentityTag
	// DomainTypeName is a ClickHouse type — `system.data_type_families`, or
	// the client's own knowledge of the spelling.
	DomainTypeName
	// DomainTimeZone is an IANA zone name — `system.time_zones`.
	DomainTimeZone
	// DomainSetting is a ClickHouse setting name — `system.settings` and
	// `system.merge_tree_settings`.
	DomainSetting
	// DomainDictionary is a loaded dictionary — `system.dictionaries`.
	DomainDictionary
	// DomainDictionaryAttribute is an attribute of the dictionary named at Ref.
	DomainDictionaryAttribute
	// DomainFormat is an input/output format name — `system.formats`.
	DomainFormat
	// DomainStatementParam is a query parameter slot name (ADR-0187).
	DomainStatementParam
)

// AllDomainKinds is every kind, for a test that walks them and for a panel
// listing what a build can express.
var AllDomainKinds = []DomainKindE{
	DomainUnspecified,
	DomainExpr,
	DomainComponentKind,
	DomainElementOf,
	DomainDatabase,
	DomainTable,
	DomainColumn,
	DomainIntrospectionTable,
	DomainSection,
	DomainSectionColumn,
	DomainExtractionToken,
	DomainMembership,
	DomainChannel,
	DomainSupportRole,
	DomainAspect,
	DomainCanonicalType,
	DomainGloss,
	DomainGlossKey,
	DomainIdentityTag,
	DomainTypeName,
	DomainTimeZone,
	DomainSetting,
	DomainDictionary,
	DomainDictionaryAttribute,
	DomainFormat,
	DomainStatementParam,
}

func (inst DomainKindE) String() string {
	switch inst {
	case DomainUnspecified:
		return "unspecified"
	case DomainExpr:
		return "expression"
	case DomainComponentKind:
		return "component kind"
	case DomainElementOf:
		return "tuple element"
	case DomainDatabase:
		return "database"
	case DomainTable:
		return "table"
	case DomainColumn:
		return "column"
	case DomainIntrospectionTable:
		return "introspection table"
	case DomainSection:
		return "leeway section"
	case DomainSectionColumn:
		return "section column"
	case DomainExtractionToken:
		return "extraction token"
	case DomainMembership:
		return "membership"
	case DomainChannel:
		return "membership channel"
	case DomainSupportRole:
		return "support role"
	case DomainAspect:
		return "aspect"
	case DomainCanonicalType:
		return "canonical type"
	case DomainGloss:
		return "gloss"
	case DomainGlossKey:
		return "gloss key"
	case DomainIdentityTag:
		return "identity tag"
	case DomainTypeName:
		return "type name"
	case DomainTimeZone:
		return "time zone"
	case DomainSetting:
		return "setting"
	case DomainDictionary:
		return "dictionary"
	case DomainDictionaryAttribute:
		return "dictionary attribute"
	case DomainFormat:
		return "format"
	case DomainStatementParam:
		return "query parameter"
	}
	return "unknown"
}

// IsRefDependent reports whether the kind reads a sibling argument. For these
// [Domain.Ref] is the sibling's ordinal and must be set; for every other kind
// it is [NoRef].
func (inst DomainKindE) IsRefDependent() bool {
	switch inst {
	case DomainElementOf, DomainSectionColumn, DomainExtractionToken, DomainGlossKey, DomainDictionaryAttribute:
		return true
	}
	return false
}

// NoRef is [Domain.Ref] for a kind that depends on no sibling. It is not zero
// because zero is a valid ordinal, and a domain that silently read argument 0
// would be a wrong answer rather than an absent one.
const NoRef = -1

// Domain is what may stand at one argument position.
type Domain struct {
	Kind DomainKindE
	// Ref is the ordinal of the sibling argument this domain reads, for the
	// kinds [DomainKindE.IsRefDependent] names; [NoRef] otherwise.
	Ref int
}

func (inst Domain) String() (s string) {
	s = inst.Kind.String()
	if inst.Kind.IsRefDependent() {
		s += " of argument " + itoa(inst.Ref)
	}
	return
}

// Param is one declared parameter: the spelling a call template shows, and
// what may stand there.
type Param struct {
	// Name is the display spelling the vocabulary panel renders — quotes and
	// ellipses included, as the rosters have always written them.
	Name   string
	Domain Domain
}

// Expr declares a parameter taking any expression.
func Expr(name string) (p Param) {
	p = Param{Name: name, Domain: Domain{Kind: DomainExpr, Ref: NoRef}}
	return
}

// Lit declares a parameter drawn from a closed vocabulary that depends on no
// sibling argument.
func Lit(name string, kind DomainKindE) (p Param) {
	p = Param{Name: name, Domain: Domain{Kind: kind, Ref: NoRef}}
	return
}

// Of declares a parameter whose vocabulary depends on the sibling argument at
// ref.
func Of(name string, kind DomainKindE, ref int) (p Param) {
	p = Param{Name: name, Domain: Domain{Kind: kind, Ref: ref}}
	return
}

// ElementOf declares a parameter naming an element of the tuple-typed sibling
// argument at ref.
func ElementOf(name string, ref int) (p Param) {
	p = Of(name, DomainElementOf, ref)
	return
}

// Exprs is the common case of a family whose whole signature is expressions.
func Exprs(names ...string) (ps []Param) {
	ps = make([]Param, len(names))
	for i, n := range names {
		ps[i] = Expr(n)
	}
	return
}

// ParamNames renders the display spellings, which is what a caller that wants
// the old `Params []string` reads.
func ParamNames(ps []Param) (names []string) {
	names = make([]string, len(ps))
	for i := range ps {
		names[i] = ps[i].Name
	}
	return
}

// itoa avoids a strconv import for the one small number this package formats.
func itoa(v int) (s string) {
	if v < 0 {
		return "-" + itoa(-v)
	}
	if v < 10 {
		return string(rune('0' + v))
	}
	return itoa(v/10) + string(rune('0'+v%10))
}
